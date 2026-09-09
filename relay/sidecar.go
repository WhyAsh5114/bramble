package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"
)

// paySidecar spawns relay-sidecar (relay-sidecar/, Hono on Bun) as a child
// process when -meter is set, adapted from brambled/sidecar.Manager's
// spawn+health-check pattern — not shared code with that package: this one
// is smaller and has no tailnet identity to match, just liveness (see
// docs/adr/0007).
type paySidecar struct {
	port   int
	secret []byte
	cmd    *exec.Cmd
}

type paySidecarConfig struct {
	Dir   string // relay-sidecar's project directory (contains package.json)
	Port  int
	Payee string // Hedera account id, HEDERA_RELAY_OPERATOR_ACCOUNT_ID
}

// startPaySidecar generates a fresh HMAC secret, spawns relay-sidecar with
// it, and blocks until /health responds. The secret never leaves this
// process pair — it's not chain state and not operator-configured, see
// docs/adr/0007's "Decision" section.
func startPaySidecar(cfg paySidecarConfig) (*paySidecar, error) {
	if cfg.Port == 0 {
		cfg.Port = 7891
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generating token secret: %w", err)
	}
	secretHex := hex.EncodeToString(secret)

	cmd := exec.Command("bun", "run", "src/index.ts")
	cmd.Dir = cfg.Dir
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("PORT=%d", cfg.Port),
		fmt.Sprintf("RENDEZVOUS_TOKEN_SECRET=%s", secretHex),
		fmt.Sprintf("HEDERA_RELAY_OPERATOR_ACCOUNT_ID=%s", cfg.Payee),
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting relay-sidecar: %w", err)
	}

	p := &paySidecar{port: cfg.Port, secret: secret, cmd: cmd}
	if err := p.waitHealthy(5 * time.Second); err != nil {
		_ = p.Stop()
		return nil, err
	}
	return p, nil
}

func (p *paySidecar) waitHealthy(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	url := fmt.Sprintf("http://127.0.0.1:%d/health", p.port)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("relay-sidecar did not become healthy within %s", timeout)
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				continue
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				continue
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
	}
}

func (p *paySidecar) Stop() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}
