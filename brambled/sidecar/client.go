package sidecar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type DeviceRecord struct {
	Fullname string  `json:"fullname"`
	Pubkey   *string `json:"pubkey"`
	Status   int     `json:"status"`
	Expiry   string  `json:"expiry"`
	TokenID  string  `json:"tokenId"`
	// Revoked mirrors the `revoked` text record — an EAC-gated revocation
	// lever distinct from clearing `pubkey` (see admission's package doc
	// comment and docs/adr/0004). Deliberately a separate record/role from
	// `pubkey` so revoke and rotate can be granted to different accounts.
	Revoked bool `json:"revoked"`
	// ACL mirrors the `acl` text record — a list of ECDH digests (see
	// docs/adr/0005-acl-record-schema.md's aclDigestECDH), never plaintext
	// service names, hosts, IPs, or ports. Gated by its own
	// grantSetterRoles-scoped role. Read-only plumbing for Phase 2 Section
	// C's gateway enforcement; admission/loop.go does not consume this field
	// — mesh admission and ACL enforcement are separate checks.
	ACL []string `json:"acl"`
	// ACLGranters mirrors the `acl-granters` text record — this device's own
	// list of already-enrolled identity labels it accepts ACL vouches from
	// when it acts as a gateway (docs/adr/0005). Section C's gateway
	// verification loop iterates this list, not ACL above, to decide whose
	// digests to even attempt matching.
	ACLGranters []string `json:"aclGranters"`
	// MeshIP mirrors the `mesh-ip` text record — this device's WireGuard
	// tunnel address as a CIDR string (e.g. "10.77.0.5/24"), in exactly the
	// format peerFlag.Set already parses for -peer's allowed-ip argument
	// (docs/adr/0009). Written once by the enrolling operator, never by the
	// device itself — see enroll.ts's mesh-ip allocation step. nil for a
	// device enrolled before this record existed.
	MeshIP *string `json:"meshIP"`
}

// RendezvousTokenResponse mirrors sidecar/src/routes/payments.ts's
// POST /rendezvous-token response — the sidecar has already paid a real
// x402/Hedera fee to the relay's payment sidecar by the time this returns
// (docs/adr/0007); Token is opaque to brambled, which just forwards it into
// rendezvous.Exchange's hello.
type RendezvousTokenResponse struct {
	Token          string `json:"token"`
	ExpiresAt      int64  `json:"expiresAt"`
	SettlementTxID string `json:"settlementTxId,omitempty"`
}

// Every call here is a loopback call to the local sidecar, but the sidecar
// then sends its ENS reads and x402 payments through Tor (ADR 0013), where
// the handoff measured 21 s for a first request and 4-5 s for later ones.
// The sidecar allows 60 s per ENS RPC and a device resolve makes seven reads
// in a row, so a flat 8 s client timeout would abandon valid answers. Each
// call therefore gets a budget for the whole operation, still bounded so one
// hung request cannot stall the retry loops that call these
// (watchDataRelayFailover, watchDataRelayAdopt) or admission's sequential
// per-peer SyncOnce pass; http.DefaultClient has no timeout at all.
var (
	// ensReadBudget covers ResolveDevice (7 sequential reads x 60 s = 7 min)
	// and the chunked log scans behind Relays and ListDevices.
	ensReadBudget = 8 * time.Minute
	// paymentBudget covers an x402 purchase: the unpaid request, the signed
	// retry and the Hedera settlement, all through Tor.
	paymentBudget = 5 * time.Minute
)

var sidecarHTTPClient = &http.Client{}

// cancelOnClose releases the request's deadline once the caller has read and
// closed the body, so the budget spans the body read as well as the headers.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

func doWithin(req *http.Request, cancel context.CancelFunc) (*http.Response, error) {
	resp, err := sidecarHTTPClient.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

func getWithin(url string, budget time.Duration) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	return doWithin(req, cancel)
}

func postJSONWithin(url string, body []byte, budget time.Duration) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return doWithin(req, cancel)
}

// RendezvousToken asks the local sidecar to pay sidecarURL — a specific
// relay's own payment sidecar — for and return one rendezvous token, for a
// single upcoming rendezvous.Exchange call against that same relay. Every
// relay in a set mints tokens under its own process-local secret and
// verifies hellos against that same secret (relay/sidecar.go,
// relay/token.go), so a token bought here is only ever valid at sidecarURL
// specifically — callers must call this again, with the new relay's own
// sidecarURL, before trying a different relay (docs/adr/0008's failover
// fix; this used to read a single fixed BRAMBLE_RENDEZVOUS_PAYMENT_URL,
// which is what made failover to a backup relay unable to get a token that
// relay would accept). Calling this against a sidecar that has no Hedera
// payer credentials configured (docs/adr/0007's client-side setup) returns
// an error — callers talking to an unmetered relay should not call this at
// all (rendezvous.Exchange accepts "" for token).
func (m *Manager) RendezvousToken(sidecarURL string) (*RendezvousTokenResponse, error) {
	reqBody, err := json.Marshal(struct {
		SidecarURL string `json:"sidecarUrl"`
	}{SidecarURL: sidecarURL})
	if err != nil {
		return nil, fmt.Errorf("encoding request: %w", err)
	}

	url := fmt.Sprintf("http://localhost:%d/rendezvous-token", m.Port)
	resp, err := postJSONWithin(url, reqBody, paymentBudget)
	if err != nil {
		return nil, fmt.Errorf("calling sidecar: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading sidecar response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sidecar returned %d: %s", resp.StatusCode, body)
	}

	var out RendezvousTokenResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decoding sidecar response: %w", err)
	}
	return &out, nil
}

// DataRelaySessionResponse mirrors sidecar/src/routes/payments.ts's
// POST /data-relay-session response — the sidecar has already paid a real,
// bytes-priced x402/Hedera fee to the chosen data relay's payment sidecar
// (docs/adr/0008). Port is where both peers must point their WireGuard
// peer-endpoint; SessionID is opaque to brambled beyond logging it.
type DataRelaySessionResponse struct {
	SessionID      string `json:"sessionId"`
	Port           int    `json:"port"`
	ExpiresAt      int64  `json:"expiresAt"`
	SettlementTxID string `json:"settlementTxId,omitempty"`
}

// DataRelaySession asks the local sidecar to buy a bytes-metered data-relay
// session from sidecarURL (a relay-sidecar's public base URL, chosen by
// brambled's own relay-selection logic — see brambled/datarelay.Pick) for
// bytes worth of forwarding. Only the side of a connection flagged with
// -relay calls this; the other side just receives the resulting relayHost:
// port as an ordinary rendezvous candidate (docs/adr/0008).
func (m *Manager) DataRelaySession(sidecarURL string, sessionBytes int64) (*DataRelaySessionResponse, error) {
	reqBody, err := json.Marshal(struct {
		SidecarURL string `json:"sidecarUrl"`
		Bytes      int64  `json:"bytes"`
	}{SidecarURL: sidecarURL, Bytes: sessionBytes})
	if err != nil {
		return nil, fmt.Errorf("encoding request: %w", err)
	}

	url := fmt.Sprintf("http://localhost:%d/data-relay-session", m.Port)
	resp, err := postJSONWithin(url, reqBody, paymentBudget)
	if err != nil {
		return nil, fmt.Errorf("calling sidecar: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading sidecar response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sidecar returned %d: %s", resp.StatusCode, respBody)
	}

	var out DataRelaySessionResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decoding sidecar response: %w", err)
	}
	return &out, nil
}

// ResolveDevice calls the sidecar's read-only device-resolution endpoint.
// label is the device's own subname label (e.g. "device1"), not a full
// dotted name — the sidecar already knows which tailnet it belongs to.
func (m *Manager) ResolveDevice(label string) (*DeviceRecord, error) {
	url := fmt.Sprintf("http://localhost:%d/device/%s", m.Port, label)
	resp, err := getWithin(url, ensReadBudget)
	if err != nil {
		return nil, fmt.Errorf("calling sidecar: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading sidecar response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sidecar returned %d: %s", resp.StatusCode, body)
	}

	var record DeviceRecord
	if err := json.Unmarshal(body, &record); err != nil {
		return nil, fmt.Errorf("decoding sidecar response: %w", err)
	}
	return &record, nil
}

// RendezvousRelay/DataRelay/RelaysResponse mirror sidecar/src/ens/relays.ts's
// resolveRelays() output — real ENS-based relay discovery (docs/adr/0003's
// Consequence section, resolved), replacing the -rendezvous/-data-relay
// hardcoding that shipped before this.
type RendezvousRelay struct {
	Label   string `json:"label"`
	Address string `json:"address"`
	// SidecarURL is this relay's own payment sidecar, if it published one —
	// needed to buy a token valid at this specific relay (each relay mints
	// under its own process-local secret). Empty for an unmetered relay.
	SidecarURL string `json:"sidecarUrl,omitempty"`
}

type DataRelay struct {
	Label        string `json:"label"`
	SidecarURL   string `json:"sidecarUrl"`
	PricePerByte string `json:"pricePerByte"`
}

type RelaysResponse struct {
	Rendezvous []RendezvousRelay `json:"rendezvous"`
	DataRelays []DataRelay       `json:"dataRelays"`
}

// Relays asks the local sidecar to discover this tailnet's relays from the
// relay registry. Returns empty lists, not an error, if no relay registry
// is configured — callers should fall back to their own static overrides
// (-rendezvous/-data-relay) in that case, same as sidecar/src/ens/relays.ts
// itself does.
func (m *Manager) Relays() (*RelaysResponse, error) {
	url := fmt.Sprintf("http://localhost:%d/relays", m.Port)
	resp, err := getWithin(url, ensReadBudget)
	if err != nil {
		return nil, fmt.Errorf("calling sidecar: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading sidecar response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sidecar returned %d: %s", resp.StatusCode, body)
	}

	var out RelaysResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decoding sidecar response: %w", err)
	}
	return &out, nil
}

// ListDevices asks the local sidecar to enumerate every label ever
// registered on the tailnet's own device subregistry (docs/adr/0010's
// full-mesh-by-default peer discovery) — the same LabelRegistered-log-scan
// technique as Relays() above, against the device registry instead of the
// relay one. Includes revoked/expired/self labels; callers filter those out
// (admission.Loop's own authorization check already handles revoked/expired
// gracefully for a statically-listed peer, so this only needs to exclude
// self).
func (m *Manager) ListDevices() ([]string, error) {
	url := fmt.Sprintf("http://localhost:%d/devices", m.Port)
	resp, err := getWithin(url, ensReadBudget)
	if err != nil {
		return nil, fmt.Errorf("calling sidecar: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading sidecar response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sidecar returned %d: %s", resp.StatusCode, body)
	}

	var out struct {
		Labels []string `json:"labels"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decoding sidecar response: %w", err)
	}
	return out.Labels, nil
}
