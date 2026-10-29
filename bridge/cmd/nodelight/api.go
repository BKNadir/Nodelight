package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

//go:embed web/index.html
var indexHTML []byte

// tokenHeader carries the secret shared with Moonlight and the settings page.
const tokenHeader = "X-Nodelight-Token"

// controlServer is the local control API used by Moonlight's settings page and
// by the browser page. It only listens on 127.0.0.1 and every API call needs
// the random token generated at startup.
type controlServer struct {
	c     *controller
	token string
	ln    net.Listener
	srv   *http.Server
	port  int
}

func newToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func startControlServer(c *controller) (*controlServer, error) {
	tok, err := newToken()
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &controlServer{c: c, token: tok, ln: ln, port: ln.Addr().(*net.TCPAddr).Port}
	s.srv = &http.Server{
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
	}
	go func() { _ = s.srv.Serve(ln) }()
	return s, nil
}

func (s *controlServer) URL() string { return fmt.Sprintf("http://127.0.0.1:%d", s.port) }

// PanelURL is the browser page address, with the token the page needs.
func (s *controlServer) PanelURL() string { return s.URL() + "/?token=" + s.token }

func (s *controlServer) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.srv.Shutdown(ctx)
}

func (s *controlServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /api/status", s.api(s.handleStatus))
	mux.HandleFunc("POST /api/connect", s.api(s.handleConnect))
	mux.HandleFunc("POST /api/disconnect", s.api(s.handleDisconnect))
	mux.HandleFunc("POST /api/exit-node", s.api(s.handleExitNode))
	mux.HandleFunc("PUT /api/hosts", s.api(s.handleHosts))
	mux.HandleFunc("POST /api/profile/select", s.api(s.handleProfileSelect))
	mux.HandleFunc("POST /api/profile/create", s.api(s.handleProfileCreate))
	mux.HandleFunc("POST /api/profile/delete", s.api(s.handleProfileDelete))
	return s.hostGuard(mux)
}

// hostGuard rejects requests whose Host header is not our loopback address,
// which blocks DNS-rebinding attacks from web pages.
func (s *controlServer) hostGuard(next http.Handler) http.Handler {
	ok := map[string]bool{
		fmt.Sprintf("127.0.0.1:%d", s.port): true,
		fmt.Sprintf("localhost:%d", s.port): true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ok[strings.ToLower(r.Host)] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *controlServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; img-src 'self' data:")
	_, _ = w.Write(indexHTML)
}

type apiHandler func(r *http.Request) (any, int, error)

// api wraps a handler with token checking and JSON encoding.
func (s *controlServer) api(h apiHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if subtle.ConstantTimeCompare([]byte(r.Header.Get(tokenHeader)), []byte(s.token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		out, code, err := h(r)
		if err != nil {
			if code == 0 {
				code = http.StatusBadRequest
			}
			writeJSON(w, code, map[string]string{"error": err.Error()})
			return
		}
		if code == 0 {
			code = http.StatusOK
		}
		writeJSON(w, code, out)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeBody(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	return nil
}

func (s *controlServer) status(r *http.Request) statusView { return s.c.Status(r.Context()) }

func (s *controlServer) handleStatus(r *http.Request) (any, int, error) {
	return s.status(r), 0, nil
}

func (s *controlServer) handleConnect(r *http.Request) (any, int, error) {
	if err := s.c.Connect(); err != nil {
		return nil, http.StatusBadRequest, err
	}
	return s.status(r), 0, nil
}

func (s *controlServer) handleDisconnect(r *http.Request) (any, int, error) {
	s.c.Disconnect()
	return s.status(r), 0, nil
}

func (s *controlServer) handleExitNode(r *http.Request) (any, int, error) {
	var body struct {
		ExitNode string `json:"exitNode"`
	}
	if err := decodeBody(r, &body); err != nil {
		return nil, 0, err
	}
	if err := s.c.SetExitNode(body.ExitNode); err != nil {
		return nil, http.StatusBadRequest, err
	}
	return s.status(r), 0, nil
}

func (s *controlServer) handleHosts(r *http.Request) (any, int, error) {
	var body struct {
		Hosts []hostSpec `json:"hosts"`
	}
	if err := decodeBody(r, &body); err != nil {
		return nil, 0, err
	}
	if err := s.c.SetHosts(body.Hosts); err != nil {
		return nil, http.StatusBadRequest, err
	}
	return s.status(r), 0, nil
}

func (s *controlServer) handleProfileSelect(r *http.Request) (any, int, error) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeBody(r, &body); err != nil {
		return nil, 0, err
	}
	if err := s.c.SelectProfile(body.Name); err != nil {
		return nil, http.StatusBadRequest, err
	}
	return s.status(r), 0, nil
}

func (s *controlServer) handleProfileCreate(r *http.Request) (any, int, error) {
	var body struct {
		Name       string `json:"name"`
		ControlURL string `json:"controlUrl"`
		Hostname   string `json:"hostname"`
	}
	if err := decodeBody(r, &body); err != nil {
		return nil, 0, err
	}
	if err := s.c.CreateProfile(body.Name, body.ControlURL, body.Hostname); err != nil {
		return nil, http.StatusBadRequest, err
	}
	return s.status(r), http.StatusCreated, nil
}

func (s *controlServer) handleProfileDelete(r *http.Request) (any, int, error) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeBody(r, &body); err != nil {
		return nil, 0, err
	}
	if err := s.c.DeleteProfile(body.Name); err != nil {
		return nil, http.StatusBadRequest, err
	}
	return s.status(r), 0, nil
}
