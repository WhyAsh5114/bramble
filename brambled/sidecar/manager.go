// Package sidecar spawns and talks to the per-node Hono/TypeScript sidecar
// process that brambled uses for all ENS interaction (docs/adr/0002). The
// sidecar is started as a child process of this daemon, never shared across
// nodes — see the ADR for why that's load-bearing, not incidental.
package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Manager struct {
	Port int
	cmd  *exec.Cmd

	// wantTailnetName/wantTailnetRegistry are the identity waitHealthy
	// checks a /health response against — see its doc comment.
	wantTailnetName     string
	wantTailnetRegistry string
}

// healthResponse mirrors sidecar/src/index.ts's /health payload.
type healthResponse struct {
	OK              bool   `json:"ok"`
	TailnetName     string `json:"tailnetName"`
	TailnetRegistry string `json:"tailnetRegistry"`
}

type Config struct {
	// Dir is the sidecar's project directory (contains package.json).
	Dir string
	// Port is the local TCP port the sidecar listens on.
	Port int
	// TailnetName and TailnetRegistry are passed through to the sidecar's
	// environment — a node must know which tailnet/subregistry it belongs to
	// before it can resolve anything (see sidecar/src/ens/config.ts).
	TailnetName     string
	TailnetRegistry string
	// RPCURL is optional; the sidecar has its own default.
	RPCURL string

	// HederaClientAccountID and HederaClientPrivateKey configure this node's
	// payment-client side of docs/adr/0007 — the sidecar's /rendezvous-token
	// and /data-relay-session routes pay whichever relay-sidecar URL the
	// caller passes per call, using this Hedera account. Both optional:
	// leave them unset when talking only to unmetered relays, in which case
	// RendezvousToken (see client.go) is simply never called.
	HederaClientAccountID  string
	HederaClientPrivateKey string

	// TorReadyTimeout bounds how long Start waits for the sidecar's Tor
	// client after /health answers. Zero means DefaultTorReadyTimeout.
	TorReadyTimeout time.Duration
}

// DefaultTorReadyTimeout covers a first bootstrap attempt that runs to its
// full deadline and a second, complete attempt: the sidecar gives each
// attempt 180 s and starts the next 30 s after a failure, so 390 s in all.
// Tor was ready after 51 s under Bun with no snapshot (5 Nov 2026 spike).
const DefaultTorReadyTimeout = 7 * time.Minute

// torStatusResponse mirrors sidecar/src/routes/tor.ts's /tor/status payload.
type torStatusResponse struct {
	State     string `json:"state"`
	LastError string `json:"lastError"`
	Message   string `json:"message"`
}

// Start spawns the sidecar as a child process and blocks until it responds
// to /health and its Tor client is ready, or a timeout elapses.
func Start(cfg Config) (*Manager, error) {
	if cfg.Port == 0 {
		cfg.Port = 7890
	}

	cmd := exec.Command("bun", "run", "src/index.ts")
	cmd.Dir = cfg.Dir
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("BRAMBLE_SIDECAR_PORT=%d", cfg.Port),
		fmt.Sprintf("BRAMBLE_TAILNET_NAME=%s", cfg.TailnetName),
		fmt.Sprintf("BRAMBLE_TAILNET_REGISTRY=%s", cfg.TailnetRegistry),
	)
	if cfg.RPCURL != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("SEPOLIA_RPC_URL=%s", cfg.RPCURL))
	}
	if cfg.HederaClientAccountID != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("HEDERA_CLIENT_ACCOUNT_ID=%s", cfg.HederaClientAccountID))
	}
	if cfg.HederaClientPrivateKey != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("HEDERA_CLIENT_PRIVATE_KEY=%s", cfg.HederaClientPrivateKey))
	}
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting sidecar: %w", err)
	}

	m := &Manager{Port: cfg.Port, cmd: cmd, wantTailnetName: cfg.TailnetName, wantTailnetRegistry: cfg.TailnetRegistry}
	if err := m.waitHealthy(5 * time.Second); err != nil {
		_ = m.Stop()
		return nil, err
	}
	torTimeout := cfg.TorReadyTimeout
	if torTimeout == 0 {
		torTimeout = DefaultTorReadyTimeout
	}
	if err := m.waitTorReady(torTimeout, 500*time.Millisecond); err != nil {
		_ = m.Stop()
		return nil, err
	}
	return m, nil
}

// waitTorReady polls /tor/status until the sidecar's Tor client is ready.
// The sidecar answers every data route with 503 "starting Tor" until then
// (docs/adr/0013), and serve's startup reads (auto-discovery, mesh-ip
// resolution) have no last-known state to fall back on, so they must wait
// rather than fail. A 404 means a sidecar that predates the Tor client and
// is treated as ready. A "failed" state keeps polling, because the sidecar
// retries its bootstrap on its own.
func (m *Manager) waitTorReady(timeout, interval time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	url := fmt.Sprintf("http://127.0.0.1:%d/tor/status", m.Port)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	start := time.Now()
	announced := false
	last := "no answer from /tor/status"
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("building /tor/status request: %w", err)
		}
		if resp, err := http.DefaultClient.Do(req); err == nil {
			var body torStatusResponse
			decodeErr := json.NewDecoder(resp.Body).Decode(&body)
			resp.Body.Close()
			switch {
			case resp.StatusCode == http.StatusNotFound:
				return nil
			case resp.StatusCode == http.StatusOK && decodeErr == nil && body.State == "ready":
				if announced {
					fmt.Fprintf(os.Stderr, "sidecar Tor ready after %s\n", time.Since(start).Round(time.Second))
				}
				return nil
			case decodeErr == nil && body.Message != "":
				last = body.Message
			}
		}
		if !announced {
			fmt.Fprintf(os.Stderr, "waiting for sidecar Tor client (%s)...\n", last)
			announced = true
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("sidecar Tor client not ready within %s: %s", timeout, last)
		case <-ticker.C:
		}
	}
}

// waitHealthy polls /health until it reports ok for *this* node's tailnet,
// not just ok:true from whatever happens to be listening on the port. The
// sidecar's port is fixed (Config.Port, default 7890) and a bare ok:true
// check can't tell "my sidecar came up" from "a stray sidecar left running
// from another node on this host is answering instead" — a silent
// wrong-tailnet resolution, not a startup failure, and much harder to
// notice. A response for a different tailnet is treated as not-yet-healthy
// and polling continues; if the timeout is reached with only mismatched
// responses seen, the error says so explicitly instead of just timing out.
func (m *Manager) waitHealthy(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	url := fmt.Sprintf("http://127.0.0.1:%d/health", m.Port)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var lastMismatch *healthResponse

	for {
		select {
		case <-ctx.Done():
			if lastMismatch != nil {
				return fmt.Errorf("sidecar did not become healthy within %s: port %d is answering /health for tailnet %q (registry %s), not %q (registry %s) — a stray sidecar from another node may be using this port",
					timeout, m.Port, lastMismatch.TailnetName, lastMismatch.TailnetRegistry, m.wantTailnetName, m.wantTailnetRegistry)
			}
			return fmt.Errorf("sidecar did not become healthy within %s", timeout)
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				continue
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				continue
			}
			var body healthResponse
			decodeErr := json.NewDecoder(resp.Body).Decode(&body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK || decodeErr != nil || !body.OK {
				continue
			}
			if !strings.EqualFold(body.TailnetRegistry, m.wantTailnetRegistry) || body.TailnetName != m.wantTailnetName {
				lastMismatch = &body
				continue
			}
			return nil
		}
	}
}

// Stop terminates the sidecar child process. Safe to call multiple times.
func (m *Manager) Stop() error {
	if m.cmd == nil || m.cmd.Process == nil {
		return nil
	}
	return m.cmd.Process.Kill()
}
