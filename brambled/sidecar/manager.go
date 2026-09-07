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
}

// Start spawns the sidecar as a child process and blocks until it responds
// to /health or the timeout elapses.
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
	return m, nil
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
