package daemon

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/WhyAsh5114/bramble/brambled/hederapay"
)

// AuditSettings is saved in state.json.
type AuditSettings struct {
	Enabled bool `json:"enabled"`
}

// AuditInfo is what the UI shows about the audit log of gateway decisions.
type AuditInfo struct {
	Enabled bool `json:"enabled"`
	// Topic is the tailnet's audit-topic record, empty when the admin has not set one.
	Topic string `json:"topic,omitempty"`
	// Active is true when the running node posts commitments.
	Active  bool `json:"active"`
	Funded  bool `json:"funded"`
	Posted  int  `json:"posted"`
	Dropped int  `json:"dropped"`
	Failed  int  `json:"failed"`
	// Openings counts the openings this device holds in memory.
	Openings int    `json:"openings"`
	Error    string `json:"error,omitempty"`
}

// Audit reports the audit log state.
func (d *Daemon) Audit() AuditInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	info := AuditInfo{Funded: d.payBal.Account != "" && d.payBal.HBAR > 0}
	if a := d.saved.Audit; a != nil {
		info.Enabled = a.Enabled
	}
	if d.tn != nil {
		info.Topic = d.tn.AuditTopic()
	}
	if d.node != nil && d.node.handle != nil {
		if st, ops, ok := d.node.handle.Audit(); ok {
			info.Active, info.Posted, info.Dropped, info.Failed, info.Error, info.Openings = true, st.Posted, st.Dropped, st.Failed, st.Error, len(ops)
		}
	}
	return info
}

// SetAudit turns the audit log on or off. The first opt-in makes the device's
// Hedera key, which the user then funds with a little HBAR.
func (d *Daemon) SetAudit(enabled bool) error {
	var key *HederaKey
	if enabled {
		k, err := d.Store.LoadOrCreateHederaKey()
		if err != nil {
			return err
		}
		key = &k
	}
	d.mu.Lock()
	d.saved.Audit = &AuditSettings{Enabled: enabled}
	if key != nil {
		d.hedera = key
	}
	err := d.Store.SaveSaved(d.saved)
	d.mu.Unlock()
	d.poke()
	if enabled {
		go d.refreshPay(context.Background())
	}
	return err
}

// CreateAuditTopic makes a topic for the tailnet's gateways with this
// device's key and returns its ID. The tailnet's admin then publishes it in
// the tailnet's audit-topic record from the browser; this device cannot write
// tailnet records.
func (d *Daemon) CreateAuditTopic() (string, error) {
	d.mu.Lock()
	key, account, hbar := d.hedera, d.payBal.Account, d.payBal.HBAR
	d.mu.Unlock()
	if key == nil || account == "" || hbar <= 0 {
		return "", errors.New("turn on the audit log and fund this device's Hedera address with test HBAR first")
	}
	signer, err := hederapay.NewSigner(account, key.PrivateKeyHex)
	if err != nil {
		return "", err
	}
	return hederapay.CreateTopic(signer, "bramble audit trail v1")
}

// AuditOpenings returns the openings this gateway keeps, newest last.
func (d *Daemon) AuditOpenings() []hederapay.Opening {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.node == nil || d.node.handle == nil {
		return []hederapay.Opening{}
	}
	if _, ops, ok := d.node.handle.Audit(); ok {
		return ops
	}
	return []hederapay.Opening{}
}

// VerifyResult says whether an opening matches an entry on the audit topic.
type VerifyResult struct {
	Match     bool   `json:"match"`
	Sequence  int64  `json:"sequence,omitempty"`
	Timestamp string `json:"consensusTimestamp,omitempty"`
	Topic     string `json:"topic"`
}

// VerifyOpening recomputes the commitment of an opening that a gateway's owner
// handed over and looks for it on the tailnet's topic through the mirror node.
func (d *Daemon) VerifyOpening(ctx context.Context, o hederapay.Opening) (*VerifyResult, error) {
	c, err := o.Recompute()
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	topic := ""
	if d.tn != nil {
		topic = d.tn.AuditTopic()
	}
	d.mu.Unlock()
	if topic == "" {
		return nil, errors.New("this tailnet has no audit topic")
	}
	vctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	e, err := hederapay.FindCommitment(vctx, &http.Client{Timeout: 10 * time.Second}, d.mirrorURL(), topic, c)
	if err != nil {
		return nil, err
	}
	res := &VerifyResult{Topic: topic}
	if e != nil {
		res.Match, res.Sequence, res.Timestamp = true, e.Sequence, e.Timestamp
	}
	return res, nil
}
