package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type apiHarness struct {
	t   *testing.T
	h   *harness
	srv *controlServer
}

func newAPIHarness(t *testing.T, cfg fileConfig) *apiHarness {
	t.Helper()
	h := newHarness(t, cfg, overrides{})
	srv, err := startControlServer(h.ctl)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return &apiHarness{t: t, h: h, srv: srv}
}

func (a *apiHarness) do(method, path string, body any, token string, host string) (int, map[string]any) {
	a.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, a.srv.URL()+path, rd)
	if err != nil {
		a.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set(tokenHeader, token)
	}
	if host != "" {
		req.Host = host
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (a *apiHarness) ok(method, path string, body any) map[string]any {
	a.t.Helper()
	code, out := a.do(method, path, body, a.srv.token, "")
	if code >= 300 {
		a.t.Fatalf("%s %s: %d %v", method, path, code, out)
	}
	return out
}

func TestAPIRequiresToken(t *testing.T) {
	a := newAPIHarness(t, twoHosts())
	for _, tok := range []string{"", "wrong", a.srv.token + "x"} {
		if code, _ := a.do("GET", "/api/status", nil, tok, ""); code != http.StatusUnauthorized {
			t.Errorf("token %q: got %d, want 401", tok, code)
		}
	}
	if code, _ := a.do("POST", "/api/connect", nil, "", ""); code != http.StatusUnauthorized {
		t.Errorf("connect without token: %d", code)
	}
	if st := a.h.ctl.Status(context.Background()); st.State != stateStopped {
		t.Fatalf("an unauthenticated call must not change anything: %s", st.State)
	}
}

func TestAPIRejectsForeignHostHeader(t *testing.T) { // DNS rebinding
	a := newAPIHarness(t, twoHosts())
	if code, _ := a.do("GET", "/api/status", nil, a.srv.token, "evil.example.com"); code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", code)
	}
}

func TestAPIIndexIsServedWithoutToken(t *testing.T) {
	a := newAPIHarness(t, twoHosts())
	resp, err := http.Get(a.srv.URL() + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "Nodelight") || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("status %d, type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") {
		t.Fatalf("CSP: %q", csp)
	}
	if got := a.srv.PanelURL(); !strings.Contains(got, "?token="+a.srv.token) {
		t.Fatalf("panel URL %q", got)
	}
	// Unknown paths are 404, not the page.
	if r2, err := http.Get(a.srv.URL() + "/nope"); err != nil || r2.StatusCode != 404 {
		t.Fatalf("unknown path: %v %v", r2, err)
	}
}

func TestAPIConnectDisconnectAndStatus(t *testing.T) {
	a := newAPIHarness(t, twoHosts())
	st := a.ok("GET", "/api/status", nil)
	if st["state"] != "stopped" || st["profile"] != "default" {
		t.Fatalf("status: %v", st)
	}
	a.ok("POST", "/api/connect", nil)
	a.h.waitState(stateRunning)
	st = a.ok("GET", "/api/status", nil)
	if st["state"] != "running" || st["tailscaleIp"] != "100.64.0.1" {
		t.Fatalf("status: %v", st)
	}
	st = a.ok("POST", "/api/disconnect", nil)
	if st["state"] != "stopped" {
		t.Fatalf("status: %v", st)
	}
}

func TestAPIConnectWithoutHosts(t *testing.T) {
	a := newAPIHarness(t, fileConfig{})
	code, out := a.do("POST", "/api/connect", nil, a.srv.token, "")
	if code != http.StatusOK {
		t.Fatalf("%d %v", code, out)
	}
}

func TestAPIExitNodeHostsAndProfiles(t *testing.T) {
	a := newAPIHarness(t, twoHosts())

	st := a.ok("POST", "/api/exit-node", map[string]string{"exitNode": "homeassistant"})
	if st["exitNode"] != "homeassistant" {
		t.Fatalf("status: %v", st)
	}

	st = a.ok("PUT", "/api/hosts", map[string]any{"hosts": []map[string]any{
		{"name": "nas", "address": "192.168.1.30", "basePort": 48989},
	}})
	hosts := st["hosts"].([]any)
	if len(hosts) != 1 || hosts[0].(map[string]any)["loopback"] != "127.77.0.1" {
		t.Fatalf("hosts: %v", hosts)
	}

	code, _ := a.do("PUT", "/api/hosts", map[string]any{"hosts": []map[string]any{
		{"name": "a", "address": "x"}, {"name": "a", "address": "y"}}}, a.srv.token, "")
	if code != http.StatusBadRequest {
		t.Fatalf("duplicate names: %d", code)
	}

	if code, _ := a.do("POST", "/api/profile/create", map[string]string{"name": "travail"}, a.srv.token, ""); code != http.StatusCreated {
		t.Fatalf("create: %d", code)
	}
	st = a.ok("POST", "/api/profile/select", map[string]string{"name": "travail"})
	if st["profile"] != "travail" {
		t.Fatalf("status: %v", st)
	}
	st = a.ok("POST", "/api/profile/delete", map[string]string{"name": "travail"})
	if st["profile"] != "default" {
		t.Fatalf("status: %v", st)
	}
}

func TestAPIRejectsUnknownFieldsAndBadJSON(t *testing.T) {
	a := newAPIHarness(t, twoHosts())
	if code, _ := a.do("POST", "/api/exit-node", `{"exitNode":"x","extra":1}`, a.srv.token, ""); code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", code)
	}
	if code, _ := a.do("POST", "/api/exit-node", `{not json`, a.srv.token, ""); code != http.StatusBadRequest {
		t.Fatalf("bad json: %d", code)
	}
	if code, _ := a.do("GET", "/api/connect", nil, a.srv.token, ""); code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method: %d", code)
	}
}
