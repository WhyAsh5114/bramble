package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultRPCURLs are free public Sepolia endpoints that served eth_getLogs over
// 9,000-block windows on 9 Nov 2026. The client rotates across them on errors.
var DefaultRPCURLs = []string{
	"https://ethereum-sepolia-rpc.publicnode.com",
	"https://0xrpc.io/sep",
	"https://sepolia.gateway.tenderly.co",
	"https://rpc.sepolia.ethpandaops.io",
}

// RPC is a JSON-RPC client that fails over across several endpoints. Free
// endpoints rate-limit and lag behind each other, so one bad answer moves the
// client to the next endpoint instead of failing the caller.
type RPC struct {
	urls   []string
	client *http.Client

	mu     sync.Mutex
	cursor int
}

// NewRPC returns a client for urls, or DefaultRPCURLs when urls is empty.
func NewRPC(urls []string) *RPC {
	var clean []string
	for _, u := range urls {
		if u = strings.TrimSpace(u); u != "" {
			clean = append(clean, u)
		}
	}
	if len(clean) == 0 {
		clean = append(clean, DefaultRPCURLs...)
	}
	return &RPC{urls: clean, client: &http.Client{Timeout: 30 * time.Second}}
}

// Endpoints returns the configured URLs in rotation order.
func (r *RPC) Endpoints() []string { return append([]string(nil), r.urls...) }

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
}

// RPCError is a JSON-RPC error object returned by a node.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// rangeTooLarge reports whether a node refused a getLogs window as too wide.
func rangeTooLarge(err error) bool {
	var re *RPCError
	if !errors.As(err, &re) {
		return false
	}
	m := strings.ToLower(re.Message)
	if behindHead(err) {
		return false
	}
	return strings.Contains(m, "range") || strings.Contains(m, "limit") || strings.Contains(m, "too many") ||
		strings.Contains(m, "10000") || strings.Contains(m, "block range")
}

// behindHead reports whether a node refused a window because it has not
// reached the window's end yet. Another endpoint, or a later retry, answers it.
func behindHead(err error) bool {
	var re *RPCError
	if !errors.As(err, &re) {
		return false
	}
	m := strings.ToLower(re.Message)
	return strings.Contains(m, "beyond current head") || strings.Contains(m, "after last accepted") ||
		strings.Contains(m, "block not found") || strings.Contains(m, "header not found") ||
		strings.Contains(m, "unknown block")
}

// call tries each endpoint once, starting at the current cursor, and returns
// the first successful result. A failing endpoint advances the cursor.
func (r *RPC) call(ctx context.Context, method string, params []any, out any) error {
	r.mu.Lock()
	start := r.cursor
	r.mu.Unlock()
	var lastErr error
	for i := range r.urls {
		idx := (start + i) % len(r.urls)
		err := r.callOne(ctx, r.urls[idx], method, params, out)
		if err == nil {
			if i > 0 {
				r.mu.Lock()
				r.cursor = idx
				r.mu.Unlock()
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lastErr = fmt.Errorf("%s: %w", hostOf(r.urls[idx]), err)
		// A window that is too wide fails the same way everywhere; let the caller shrink it.
		if rangeTooLarge(err) {
			return lastErr
		}
	}
	return lastErr
}

func (r *RPC) callOne(ctx context.Context, url, method string, params []any, out any) error {
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	var decoded rpcResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	if decoded.Error != nil {
		return decoded.Error
	}
	if len(decoded.Result) == 0 || string(decoded.Result) == "null" {
		return errors.New("empty result")
	}
	return json.Unmarshal(decoded.Result, out)
}

// hostOf trims a URL to its host for error messages, so a key embedded in a
// path never reaches a log line.
func hostOf(u string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	if i := strings.IndexAny(s, "/?"); i >= 0 {
		s = s[:i]
	}
	return s
}

// BlockNumber returns the endpoint's head block.
func (r *RPC) BlockNumber(ctx context.Context) (uint64, error) {
	var hex string
	if err := r.call(ctx, "eth_blockNumber", []any{}, &hex); err != nil {
		return 0, err
	}
	return parseQuantity(hex)
}

// Log is one decoded-enough eth_getLogs entry.
type Log struct {
	Address     string   `json:"address"`
	Topics      []string `json:"topics"`
	Data        string   `json:"data"`
	BlockNumber string   `json:"blockNumber"`
	TxIndex     string   `json:"transactionIndex"`
	LogIndex    string   `json:"logIndex"`
	Removed     bool     `json:"removed"`
}

// Block returns the log's block number.
func (l Log) Block() uint64 { n, _ := parseQuantity(l.BlockNumber); return n }

// Position orders logs within the chain: block, then log index.
func (l Log) Position() (uint64, uint64) {
	b, _ := parseQuantity(l.BlockNumber)
	i, _ := parseQuantity(l.LogIndex)
	return b, i
}

// GetLogs returns every log emitted by addresses in [from, to].
func (r *RPC) GetLogs(ctx context.Context, addresses []string, from, to uint64) ([]Log, error) {
	if len(addresses) == 0 || from > to {
		return nil, nil
	}
	filter := map[string]any{
		"address":   addresses,
		"fromBlock": quantity(from),
		"toBlock":   quantity(to),
	}
	var logs []Log
	if err := r.call(ctx, "eth_getLogs", []any{filter}, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

// GetLogsTopics is GetLogs with a topic filter (nil positions match anything).
func (r *RPC) GetLogsTopics(ctx context.Context, addresses []string, topics [][]string, from, to uint64) ([]Log, error) {
	filter := map[string]any{
		"address":   addresses,
		"fromBlock": quantity(from),
		"toBlock":   quantity(to),
	}
	var ts []any
	for _, t := range topics {
		if t == nil {
			ts = append(ts, nil)
		} else {
			ts = append(ts, t)
		}
	}
	filter["topics"] = ts
	var logs []Log
	if err := r.call(ctx, "eth_getLogs", []any{filter}, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

// Call runs eth_call against the latest block and returns the raw result.
func (r *RPC) Call(ctx context.Context, to string, data []byte) ([]byte, error) {
	var hex string
	err := r.call(ctx, "eth_call", []any{map[string]string{"to": to, "data": "0x" + hexEncode(data)}, "latest"}, &hex)
	if err != nil {
		return nil, err
	}
	return hexDecode(hex)
}

func quantity(n uint64) string { return fmt.Sprintf("0x%x", n) }

func parseQuantity(s string) (uint64, error) {
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return 0, errors.New("empty quantity")
	}
	n, ok := new(big.Int).SetString(s, 16)
	if !ok || !n.IsUint64() {
		return 0, fmt.Errorf("bad quantity %q", s)
	}
	return n.Uint64(), nil
}
