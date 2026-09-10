// demo-agent is the intentionally thin automated worker for the hackathon
// demo. It has no backend credential: its only route to the private task API
// is brambled's localhost forward and the worker device's live ENS grant.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type taskResponse struct {
	Task       string `json:"task"`
	Host       string `json:"host"`
	Status     string `json:"status"`
	ObservedAt string `json:"observedAt"`
}

func runTask(ctx context.Context, client *http.Client, taskURL string, out io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, taskURL, nil)
	if err != nil {
		return fmt.Errorf("building task request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("private task request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("private task returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var task taskResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&task); err != nil {
		return fmt.Errorf("decoding private task: %w", err)
	}
	if task.Task != "report-private-service-health" || task.Status != "healthy" || task.Host == "" {
		return fmt.Errorf("private task returned an invalid result")
	}

	fmt.Fprintf(out, "task complete: %s is %s on %s (observed %s)\n", task.Task, task.Status, task.Host, task.ObservedAt)
	return nil
}

func main() {
	taskURL := flag.String("url", "http://127.0.0.1:8080/task", "private task URL exposed by brambled's local forward")
	timeout := flag.Duration("timeout", 10*time.Second, "task request timeout")
	flag.Parse()
	if *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "error: timeout must be positive")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := runTask(ctx, &http.Client{}, *taskURL, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "task denied or failed:", err)
		os.Exit(1)
	}
}
