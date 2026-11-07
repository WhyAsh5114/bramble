package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"time"
)

// Server is the local control API and UI (ETH-177 contract v1, spec D3).
type Server struct {
	Daemon *Daemon
	// UI is the static UI to serve at /.
	UI fs.FS
	// Port is the port the listener is bound to; it fixes the allowed Host
	// and Origin values.
	Port int
}

// Listen binds 127.0.0.1:preferred, or the next free port up to 20 above.
func Listen(preferred int) (net.Listener, int, error) {
	var lastErr error
	for p := preferred; p < preferred+20; p++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			return ln, p, nil
		}
		lastErr = err
	}
	return nil, 0, fmt.Errorf("no free port from %d to %d: %w", preferred, preferred+19, lastErr)
}

// Handler returns the guarded mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("POST /api/setup", s.handleSetup)
	mux.HandleFunc("POST /api/join", s.handleJoin)
	mux.HandleFunc("POST /api/leave", s.handleLeave)
	mux.HandleFunc("POST /api/test", s.handleTest)
	mux.HandleFunc("POST /api/request-access", s.handleRequestAccess)
	mux.HandleFunc("/api/admin/", s.handleAdmin)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, &ErrorInfo{Message: "No such API route.", NextStep: "Update Bramble."})
	})
	mux.Handle("/", s.uiHandler())
	return s.guard(mux)
}

// guard rejects requests a web page on another site could forge: a Host
// that is not this loopback listener (DNS rebinding), and a write whose
// Origin is not this UI (cross-site requests).
func (s *Server) guard(next http.Handler) http.Handler {
	hosts := map[string]bool{}
	origins := map[string]bool{}
	for _, h := range []string{"127.0.0.1", "localhost", "[::1]"} {
		hp := fmt.Sprintf("%s:%d", h, s.Port)
		hosts[hp] = true
		origins["http://"+hp] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hosts[strings.ToLower(r.Host)] {
			writeError(w, http.StatusForbidden, &ErrorInfo{Message: "This address is not allowed.", NextStep: fmt.Sprintf("Open http://127.0.0.1:%d instead.", s.Port)})
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			origin := r.Header.Get("Origin")
			if origin != "" && !origins[strings.ToLower(origin)] {
				writeError(w, http.StatusForbidden, &ErrorInfo{Message: "Requests from other websites are not allowed.", NextStep: fmt.Sprintf("Use the Bramble page at http://127.0.0.1:%d.", s.Port)})
				return
			}
			if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
				writeError(w, http.StatusForbidden, &ErrorInfo{Message: "Requests from other websites are not allowed.", NextStep: fmt.Sprintf("Use the Bramble page at http://127.0.0.1:%d.", s.Port)})
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, info *ErrorInfo) {
	writeJSON(w, status, map[string]any{"error": info})
}

// writeErr maps a daemon error to a response.
func writeErr(w http.ResponseWriter, err error) {
	var info *ErrorInfo
	if !errors.As(err, &info) {
		info = &ErrorInfo{Message: err.Error(), NextStep: "Try again. If it keeps failing, restart Bramble."}
	}
	status := http.StatusBadRequest
	if errors.Is(err, ErrBusy) {
		status = http.StatusConflict
	}
	writeError(w, status, info)
}

func decodeBody(r *http.Request, v any) error {
	if r.ContentLength == 0 {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(v); err != nil && err != io.EOF {
		return &ErrorInfo{Message: "The request body is not valid JSON.", NextStep: "Reload the page and try again."}
	}
	return nil
}

func (s *Server) handleState(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Daemon.State())
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Daemon.Setup(body.Name); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.Daemon.State())
}

func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tailnet string `json:"tailnet"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Daemon.Join(r.Context(), body.Tailnet); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.Daemon.State())
}

func (s *Server) handleLeave(w http.ResponseWriter, _ *http.Request) {
	s.Daemon.Leave()
	writeJSON(w, http.StatusOK, s.Daemon.State())
}

func (s *Server) handleRequestAccess(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Peer    string `json:"peer"`
		Service string `json:"service"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Daemon.RequestAccess(r.Context(), body.Peer, body.Service); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.Daemon.State())
}

func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Peer    string `json:"peer"`
		Service string `json:"service"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.Daemon.Test(r.Context(), body.Peer, body.Service, httpGetStatus))
}

// httpGetStatus fetches url once and returns its status code.
func httpGetStatus(ctx context.Context, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, nil
}

// handleAdmin proxies /api/admin/<rest> to <joinApi>/api/<rest>, adding the
// saved admin token. The browser never sees the token (spec D6).
func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, &ErrorInfo{Message: "Only GET and POST are allowed here.", NextStep: "Reload the page."})
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/admin/")
	if rest == "" || strings.Contains(rest, "..") {
		writeError(w, http.StatusNotFound, &ErrorInfo{Message: "No such admin route.", NextStep: "Reload the page."})
		return
	}
	cfg, err := s.Daemon.config(r.Context(), false)
	if err != nil {
		writeErr(w, err)
		return
	}
	target, err := url.Parse(cfg.JoinAPI)
	if err != nil {
		writeErr(w, err)
		return
	}
	token := s.Daemon.Store.AdminToken()
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = target.Scheme
			pr.Out.URL.Host = target.Host
			pr.Out.URL.Path = path.Join(target.Path, "/api", rest)
			pr.Out.URL.RawPath = ""
			pr.Out.Host = target.Host
			for _, h := range []string{"Authorization", "Cookie", "Origin", "Referer"} {
				pr.Out.Header.Del(h)
			}
			if token != "" {
				pr.Out.Header.Set("Authorization", "Bearer "+token)
			}
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			writeError(w, http.StatusBadGateway, &ErrorInfo{Message: "Bramble could not reach the approval service.", NextStep: "Check the internet connection, then reload."})
		},
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del("Set-Cookie")
			return nil
		},
		FlushInterval: 100 * time.Millisecond,
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	proxy.ServeHTTP(w, r)
}

// uiHandler serves the static UI. A Next.js static export names pages
// "<route>.html", so "/join" falls back to "join.html".
func (s *Server) uiHandler() http.Handler {
	files := http.FileServer(http.FS(s.UI))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, &ErrorInfo{Message: "Only GET is allowed here.", NextStep: "Reload the page."})
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" && !strings.Contains(path.Base(p), ".") {
			if _, err := fs.Stat(s.UI, p+".html"); err == nil {
				r = r.Clone(r.Context())
				r.URL.Path = "/" + p + ".html"
			}
		}
		files.ServeHTTP(w, r)
	})
}
