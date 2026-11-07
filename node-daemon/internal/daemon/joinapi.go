package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// JoinStatus is one of the join API's status values.
type JoinStatus string

const (
	JoinPending  JoinStatus = "pending"
	JoinApproved JoinStatus = "approved"
	JoinDenied   JoinStatus = "denied"
	JoinRevoked  JoinStatus = "revoked"
)

// Join is GET /api/joins/:id's response (ETH-177 contract v1).
type Join struct {
	ID        string     `json:"id"`
	Label     string     `json:"label"`
	Tailnet   string     `json:"tailnet"`
	Status    JoinStatus `json:"status"`
	MeshIP    string     `json:"meshIp,omitempty"`
	DecidedAt string     `json:"decidedAt,omitempty"`
}

// ErrJoinNotFound means the join API no longer knows the request id. Join
// requests live in memory, so an approval-service restart forgets them.
var ErrJoinNotFound = errors.New("join request not found")

// JoinAPIError is a non-2xx answer from the join API.
type JoinAPIError struct {
	Status  int
	Message string
}

func (e *JoinAPIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("the approval service answered HTTP %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("the approval service answered HTTP %d", e.Status)
}

// JoinClient talks to the approval service's open join endpoints.
type JoinClient struct {
	BaseURL string
	HTTP    *http.Client
}

func (c *JoinClient) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Request sends POST /api/joins.
func (c *JoinClient) Request(ctx context.Context, label, pubkeyHex, tailnet string) (*Join, error) {
	body, _ := json.Marshal(map[string]string{"label": label, "pubkey": pubkeyHex, "tailnet": tailnet})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/joins", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	var j Join
	if err := c.do(req, &j); err != nil {
		return nil, err
	}
	if j.ID == "" {
		return nil, fmt.Errorf("the approval service returned a join request without an id")
	}
	if j.Status == "" {
		j.Status = JoinPending
	}
	return &j, nil
}

// RequestAccess sends POST /api/joins/access: this device asks the operator
// for a service. It creates a pending request; it grants nothing.
func (c *JoinClient) RequestAccess(ctx context.Context, label, gateway, service string) error {
	body, _ := json.Marshal(map[string]string{
		"agentLabel":   label,
		"gatewayLabel": gateway,
		"service":      service,
		"note":         "Requested from " + label,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/joins/access", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	var out map[string]any
	return c.do(req, &out)
}

// Get sends GET /api/joins/:id.
func (c *JoinClient) Get(ctx context.Context, id string) (*Join, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/joins/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	var j Join
	if err := c.do(req, &j); err != nil {
		var apiErr *JoinAPIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			return nil, ErrJoinNotFound
		}
		return nil, err
	}
	return &j, nil
}

func (c *JoinClient) do(req *http.Request, out any) error {
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		return &JoinAPIError{Status: resp.StatusCode, Message: e.Error}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("the approval service sent an unreadable answer: %w", err)
	}
	return nil
}
