package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/WhyAsh5114/bramble/brambled/hederapay"
	"io"
	"io/fs"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

// ErrorInfo is the API's error body: what went wrong and what to do next.
type ErrorInfo struct {
	Message  string `json:"message"`
	NextStep string `json:"nextStep,omitempty"`
}

func (e *ErrorInfo) Error() string { return e.Message }

// Server serves the local UI and its API on loopback.
type Server struct {
	Daemon *Daemon
	UI     fs.FS
	Port   int
}

// Listen binds the first free loopback port from preferred.
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
	d := s.Daemon
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, d.State()) })
	mux.HandleFunc("POST /api/tailnet/lookup", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name  string `json:"name"`
			Start uint64 `json:"start"`
		}
		if !decode(w, r, &body) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		ref, err := d.Lookup(ctx, body.Name, body.Start)
		reply(w, ref, err)
	})
	mux.HandleFunc("POST /api/tailnet/select", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name  string `json:"name"`
			Label string `json:"label"`
			Start uint64 `json:"start"`
		}
		if !decode(w, r, &body) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		reply(w, d.State(), d.Select(ctx, body.Name, body.Label, body.Start))
	})
	mux.HandleFunc("POST /api/tailnet/leave", func(w http.ResponseWriter, _ *http.Request) { reply(w, d.State(), d.Leave()) })
	mux.HandleFunc("GET /api/devices", func(w http.ResponseWriter, _ *http.Request) {
		devs, err := d.Devices()
		reply(w, devs, err)
	})
	mux.HandleFunc("GET /api/mesh-ip", func(w http.ResponseWriter, _ *http.Request) {
		ip, err := d.NextMeshIP()
		reply(w, map[string]string{"meshIp": ip}, err)
	})
	mux.HandleFunc("POST /api/join-code", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Code string `json:"code"`
		}
		if !decode(w, r, &body) {
			return
		}
		req, err := d.ParseJoin(body.Code)
		reply(w, req, err)
	})
	mux.HandleFunc("POST /api/grant-digest", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Device  string `json:"device"`
			Gateway string `json:"gateway"`
			Service string `json:"service"`
		}
		if !decode(w, r, &body) {
			return
		}
		digest, err := d.GrantDigest(body.Device, body.Gateway, body.Service)
		reply(w, map[string]string{"digest": digest}, err)
	})
	mux.HandleFunc("GET /api/admin", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		ok, err := d.IsAdmin(ctx, r.URL.Query().Get("address"))
		reply(w, map[string]bool{"admin": ok}, err)
	})
	mux.HandleFunc("PUT /api/services", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Services []ServiceConfig `json:"services"`
			Ping     bool            `json:"ping"`
		}
		if !decode(w, r, &body) {
			return
		}
		reply(w, d.State(), d.SetServices(body.Services, body.Ping))
	})
	mux.HandleFunc("PUT /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RelayFirst bool     `json:"relayFirst"`
			RPCURLs    []string `json:"rpcUrls"`
		}
		if !decode(w, r, &body) {
			return
		}
		reply(w, d.State(), d.SetSettings(body.RelayFirst, body.RPCURLs))
	})
	mux.HandleFunc("GET /api/pay", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, d.Pay()) })
	mux.HandleFunc("PUT /api/pay", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled    bool  `json:"enabled"`
			MaxSession int64 `json:"maxSession"`
			MaxDay     int64 `json:"maxDay"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := d.SetPay(body.Enabled, body.MaxSession, body.MaxDay); err != nil {
			reply(w, nil, err)
			return
		}
		writeJSON(w, 200, d.Pay())
	})
	mux.HandleFunc("GET /api/audit", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, d.Audit()) })
	mux.HandleFunc("PUT /api/audit", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := d.SetAudit(body.Enabled); err != nil {
			reply(w, nil, err)
			return
		}
		writeJSON(w, 200, d.Audit())
	})
	mux.HandleFunc("POST /api/audit/topic", func(w http.ResponseWriter, _ *http.Request) {
		id, err := d.CreateAuditTopic()
		reply(w, map[string]string{"topic": id}, err)
	})
	mux.HandleFunc("GET /api/audit/openings", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, d.AuditOpenings()) })
	mux.HandleFunc("POST /api/audit/verify", func(w http.ResponseWriter, r *http.Request) {
		var o hederapay.Opening
		if !decode(w, r, &o) {
			return
		}
		res, err := d.VerifyOpening(r.Context(), o)
		reply(w, res, err)
	})
	mux.HandleFunc("GET /api/peers/{label}/services", func(w http.ResponseWriter, r *http.Request) {
		list, err := d.PeerServices(r.PathValue("label"))
		reply(w, list, err)
	})
	mux.HandleFunc("POST /api/peers/{label}/ping", func(w http.ResponseWriter, r *http.Request) {
		res, err := d.Ping(r.PathValue("label"))
		reply(w, res, err)
	})
	mux.HandleFunc("GET /api/forwards", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, d.Forwards()) })
	mux.HandleFunc("POST /api/forwards", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Peer    string `json:"peer"`
			Service string `json:"service"`
		}
		if !decode(w, r, &body) {
			return
		}
		f, err := d.Forward(body.Peer, body.Service)
		reply(w, f, err)
	})
	mux.HandleFunc("DELETE /api/forwards/{port}", func(w http.ResponseWriter, r *http.Request) {
		port, _ := strconv.Atoi(r.PathValue("port"))
		d.CloseForward(port)
		writeJSON(w, 200, d.Forwards())
	})
	mux.HandleFunc("GET /api/activity", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, d.Activity()) })
	mux.HandleFunc("GET /api/relays", func(w http.ResponseWriter, _ *http.Request) {
		relays, err := d.Relays()
		reply(w, relays, err)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, &ErrorInfo{Message: "No such API route.", NextStep: "Update Bramble."})
	})
	mux.Handle("/", s.uiHandler())
	return s.guard(mux)
}

// guard admits only same-origin loopback requests, so a website the user
// visits cannot drive this API (DNS rebinding and cross-site requests).
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
			if (origin != "" && !origins[strings.ToLower(origin)]) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				writeError(w, http.StatusForbidden, &ErrorInfo{Message: "Requests from other websites are not allowed.", NextStep: fmt.Sprintf("Use the Bramble page at http://127.0.0.1:%d.", s.Port)})
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
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

func reply(w http.ResponseWriter, v any, err error) {
	if err != nil {
		var info *ErrorInfo
		if !errors.As(err, &info) {
			info = &ErrorInfo{Message: err.Error()}
		}
		writeError(w, http.StatusBadRequest, info)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(v); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, &ErrorInfo{Message: "The request body is not valid JSON."})
		return false
	}
	return true
}

// uiHandler serves the static UI. A Next.js static export names pages
// "<route>.html", so "/devices" falls back to "devices.html".
func (s *Server) uiHandler() http.Handler {
	files := http.FileServer(http.FS(s.UI))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, &ErrorInfo{Message: "Only GET is allowed here."})
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
