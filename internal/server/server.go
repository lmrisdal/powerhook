// Package server implements the powerhook HTTP API.
package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"time"

	"powerhook/internal/config"
	"powerhook/internal/power"
	"powerhook/internal/sysinfo"
)

// Server is the HTTP API.
type Server struct {
	cfg       *config.Config
	power     power.Controller
	version   string
	tokenHash [32]byte
	busy      atomic.Bool
	// after schedules fn to run after d. Replaced in tests.
	after func(d time.Duration, fn func())
}

// New builds a Server.
func New(cfg *config.Config, ctl power.Controller, version string) *Server {
	return &Server{
		cfg:       cfg,
		power:     ctl,
		version:   version,
		tokenHash: sha256.Sum256([]byte(cfg.Token)),
		after:     func(d time.Duration, fn func()) { time.AfterFunc(d, fn) },
	}
}

// Handler returns the API routes wrapped in IP and token checks.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/suspend", s.action("suspend", s.power.Suspend))
	mux.HandleFunc("POST /api/poweroff", s.action("poweroff", s.power.Poweroff))
	mux.HandleFunc("POST /api/restart", s.action("restart", s.power.Restart))
	mux.HandleFunc("GET /api/status", s.status)
	return s.guard(mux)
}

// Run serves until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	ln, err := net.Listen("tcp", s.cfg.ListenAddr())
	if err != nil {
		return err
	}
	notes := ""
	if !s.cfg.TokenRequired() {
		notes += " [no token: IP allowlist only]"
	}
	if s.cfg.DryRun {
		notes += " [dry run]"
	}
	log.Printf("powerhook %s listening on %s (allowed: %s)%s", s.version, ln.Addr(), strings.Join(s.cfg.AllowedIPs, ", "), notes)

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

// guard checks the source IP first, then the bearer token.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, ok := remoteIP(r)
		if !ok || !s.allowed(ip) {
			log.Printf("rejected %s %s from %s: address not allowed", r.Method, r.URL.Path, r.RemoteAddr)
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		if s.cfg.TokenRequired() && !s.authorized(r) {
			log.Printf("rejected %s %s from %s: bad or missing token", r.Method, r.URL.Path, ip)
			w.Header().Set("WWW-Authenticate", `Bearer realm="powerhook"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func remoteIP(r *http.Request) (netip.Addr, bool) {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}, false
	}
	return ap.Addr().Unmap().WithZone(""), true
}

func (s *Server) allowed(ip netip.Addr) bool {
	for _, p := range s.cfg.Allowed {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *Server) authorized(r *http.Request) bool {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	// Compare fixed-size hashes so neither content nor length leaks via timing.
	got := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return subtle.ConstantTimeCompare(got[:], s.tokenHash[:]) == 1
}

func (s *Server) action(name string, fn func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.busy.CompareAndSwap(false, true) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "another action is already pending"})
			return
		}
		delay := s.cfg.ActionDelay()
		log.Printf("%s requested by %s (in %s)", name, r.RemoteAddr, delay)
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":   "accepted",
			"action":   name,
			"delay_ms": delay.Milliseconds(),
		})
		s.after(delay, func() {
			defer s.busy.Store(false)
			if err := fn(); err != nil {
				log.Printf("%s failed: %v", name, err)
			}
		})
	}
}

type statusResponse struct {
	sysinfo.Info
	Version string `json:"version"`
	DryRun  bool   `json:"dry_run"`
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, statusResponse{Info: sysinfo.Get(), Version: s.version, DryRun: s.cfg.DryRun})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
