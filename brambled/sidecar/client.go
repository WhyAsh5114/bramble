package sidecar

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
}

// ResolveDevice calls the sidecar's read-only device-resolution endpoint.
// label is the device's own subname label (e.g. "device1"), not a full
// dotted name — the sidecar already knows which tailnet it belongs to.
func (m *Manager) ResolveDevice(label string) (*DeviceRecord, error) {
	url := fmt.Sprintf("http://localhost:%d/device/%s", m.Port, label)
	resp, err := http.Get(url)
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
