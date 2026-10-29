package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nodelight/relay"
)

// ---- fakes ----

type fakeTunnel struct {
	mu         sync.Mutex
	ip         string
	upErr      error
	authURL    string        // when set, Up reports it and waits for release
	release    chan struct{} // closed by the test to finish the "sign in"
	exitNodes  []exitNodeInfo
	setExitErr error
	exitCalls  []string
	closed     bool
	onAuth     func(string)
	stateDir   string
	profile    profile
}

func (f *fakeTunnel) Up(ctx context.Context) (string, error) {
	if f.authURL != "" {
		f.onAuth(f.authURL)
		select {
		case <-f.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if f.upErr != nil {
		return "", f.upErr
	}
	return f.ip, nil
}

func (f *fakeTunnel) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return nil, errors.New("no route")
}

func (f *fakeTunnel) ExitNodes(ctx context.Context) ([]exitNodeInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exitNodes, nil
}

func (f *fakeTunnel) SetExitNode(ctx context.Context, want string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exitCalls = append(f.exitCalls, want)
	if f.setExitErr != nil {
		return "", f.setExitErr
	}
	if want == "" {
		return "", nil
	}
	return "100.64.0.5", nil
}

func (f *fakeTunnel) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

func (f *fakeTunnel) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.exitCalls...)
}

func (f *fakeTunnel) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

type fakeRelay struct {
	cfg    relay.Config
	mu     sync.Mutex
	closed bool
}

func (r *fakeRelay) Close() { r.mu.Lock(); r.closed = true; r.mu.Unlock() }
func (r *fakeRelay) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

type harness struct {
	t       *testing.T
	ctl     *controller
	cfgPath string
	base    string

	mu      sync.Mutex
	tunnels []*fakeTunnel
	relays  []*fakeRelay
	proto   fakeTunnel // template copied into every new tunnel
	failIP  string     // relay start fails for this listen IP
}

func newHarness(t *testing.T, cfg fileConfig, over overrides) *harness {
	t.Helper()
	dir := t.TempDir()
	h := &harness{t: t, cfgPath: filepath.Join(dir, "hosts.json"), base: filepath.Join(dir, "data")}
	h.proto.ip = "100.64.0.1"
	if err := saveConfig(h.cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ctl, err := newController(ctx, controllerConfig{
		ConfigPath: h.cfgPath,
		StateBase:  h.base,
		Prefix:     "127.77.0.",
		Overrides:  over,
		Factory: func(p profile, stateDir string, onAuth func(string)) (tunnel, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			f := &fakeTunnel{ip: h.proto.ip, upErr: h.proto.upErr, authURL: h.proto.authURL, release: h.proto.release,
				exitNodes: h.proto.exitNodes, setExitErr: h.proto.setExitErr, onAuth: onAuth, stateDir: stateDir, profile: p}
			h.tunnels = append(h.tunnels, f)
			return f, nil
		},
		StartRelay: func(ctx context.Context, c relay.Config) (closer, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.failIP != "" && c.ListenIP == h.failIP {
				return nil, errors.New("address in use")
			}
			r := &fakeRelay{cfg: c}
			h.relays = append(h.relays, r)
			return r, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.ctl = ctl
	t.Cleanup(ctl.Shutdown)
	return h
}

func (h *harness) lastTunnel() *fakeTunnel {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.tunnels) == 0 {
		return nil
	}
	return h.tunnels[len(h.tunnels)-1]
}

func (h *harness) relayList() []*fakeRelay {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*fakeRelay(nil), h.relays...)
}

func (h *harness) waitState(want state) statusView {
	h.t.Helper()
	var st statusView
	waitFor(h.t, func() bool {
		st = h.ctl.Status(context.Background())
		return st.State == want
	}, "state "+string(want))
	return st
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func twoHosts() fileConfig {
	return fileConfig{Hosts: []hostSpec{
		{Name: "bureau", Address: "192.168.1.20"},
		{Name: "salon", Address: "salon-pc", BasePort: 48989},
	}}
}

// ---- tests ----

func TestConnectWithoutHostsFails(t *testing.T) {
	h := newHarness(t, fileConfig{}, overrides{})
	if err := h.ctl.Connect(); !errors.Is(err, errNoHosts) {
		t.Fatalf("got %v, want errNoHosts", err)
	}
	if st := h.ctl.Status(context.Background()); st.State != stateStopped {
		t.Fatalf("state %s", st.State)
	}
}

func TestConnectStartsRelaysAndDisconnectStopsThem(t *testing.T) {
	h := newHarness(t, twoHosts(), overrides{})
	if err := h.ctl.Connect(); err != nil {
		t.Fatal(err)
	}
	st := h.waitState(stateRunning)
	waitFor(t, func() bool { return len(h.relayList()) == 2 }, "two relays")

	if st.TailscaleIP != "100.64.0.1" || len(st.Hosts) != 2 {
		t.Fatalf("status: %+v", st)
	}
	rl := h.relayList()
	if rl[0].cfg.ListenIP != "127.77.0.1" || rl[0].cfg.Target != "192.168.1.20" ||
		rl[1].cfg.ListenIP != "127.77.0.2" || rl[1].cfg.Target != "salon-pc" {
		t.Fatalf("relay configs: %+v / %+v", rl[0].cfg, rl[1].cfg)
	}
	if got := rl[1].cfg.Ports.TCP[1]; got != 48989 {
		t.Fatalf("second host must use base port 48989, got %d", got)
	}
	if st.Hosts[0].Loopback != "127.77.0.1" || st.Hosts[1].Loopback != "127.77.0.2" {
		t.Fatalf("loopbacks: %+v", st.Hosts)
	}
	// An unreachable probe is reported (the fake has no route).
	waitFor(t, func() bool {
		s := h.ctl.Status(context.Background())
		return s.Hosts[0].Reachable != nil && !*s.Hosts[0].Reachable
	}, "probe result")

	h.ctl.Disconnect()
	if st := h.ctl.Status(context.Background()); st.State != stateStopped || st.TailscaleIP != "" {
		t.Fatalf("after disconnect: %+v", st)
	}
	for i, r := range h.relayList() {
		if !r.isClosed() {
			t.Fatalf("relay %d still open", i)
		}
	}
	if !h.lastTunnel().isClosed() {
		t.Fatal("tunnel not closed")
	}
	// Connecting again works and reuses the same identity directory.
	if err := h.ctl.Connect(); err != nil {
		t.Fatal(err)
	}
	h.waitState(stateRunning)
	if h.tunnels[0].stateDir != h.base || h.tunnels[1].stateDir != h.base {
		t.Fatalf("state dirs: %q %q", h.tunnels[0].stateDir, h.tunnels[1].stateDir)
	}
}

func TestSignInFlow(t *testing.T) {
	h := newHarness(t, twoHosts(), overrides{})
	h.proto.authURL = "https://login.tailscale.com/a/abc123"
	h.proto.release = make(chan struct{})
	if err := h.ctl.Connect(); err != nil {
		t.Fatal(err)
	}
	st := h.waitState(stateNeedsLogin)
	if st.AuthURL != "https://login.tailscale.com/a/abc123" {
		t.Fatalf("auth url %q", st.AuthURL)
	}
	close(h.proto.release)
	st = h.waitState(stateRunning)
	if st.AuthURL != "" {
		t.Fatalf("auth url must be cleared once signed in, got %q", st.AuthURL)
	}
}

func TestDisconnectWhileWaitingForSignIn(t *testing.T) {
	h := newHarness(t, twoHosts(), overrides{})
	h.proto.authURL = "https://login.tailscale.com/a/abc123"
	h.proto.release = make(chan struct{}) // never released
	if err := h.ctl.Connect(); err != nil {
		t.Fatal(err)
	}
	h.waitState(stateNeedsLogin)
	done := make(chan struct{})
	go func() { h.ctl.Disconnect(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Disconnect hung while waiting for sign-in")
	}
	if st := h.ctl.Status(context.Background()); st.State != stateStopped {
		t.Fatalf("state %s", st.State)
	}
	if !h.lastTunnel().isClosed() {
		t.Fatal("tunnel not closed")
	}
}

func TestUpErrorThenRetry(t *testing.T) {
	h := newHarness(t, twoHosts(), overrides{})
	h.proto.upErr = errors.New("control server unreachable")
	if err := h.ctl.Connect(); err != nil {
		t.Fatal(err)
	}
	st := h.waitState(stateError)
	if !strings.Contains(st.Error, "control server unreachable") {
		t.Fatalf("error text: %q", st.Error)
	}
	h.proto.upErr = nil
	if err := h.ctl.Connect(); err != nil {
		t.Fatal(err)
	}
	h.waitState(stateRunning)
}

func TestExitNodeAppliedAtConnectAndLive(t *testing.T) {
	cfg := twoHosts()
	cfg.ExitNode = "homeassistant"
	h := newHarness(t, cfg, overrides{})
	h.proto.exitNodes = []exitNodeInfo{{Name: "homeassistant", IP: "100.64.0.5", Online: true}}
	if err := h.ctl.Connect(); err != nil {
		t.Fatal(err)
	}
	st := h.waitState(stateRunning)
	waitFor(t, func() bool { return h.ctl.Status(context.Background()).ExitNodeIP == "100.64.0.5" }, "exit node applied")
	if got := h.lastTunnel().calls(); len(got) != 1 || got[0] != "homeassistant" {
		t.Fatalf("calls: %v", got)
	}
	if len(st.ExitNodes) != 1 || st.ExitNodes[0].Name != "homeassistant" {
		t.Fatalf("exit nodes in status: %+v", st.ExitNodes)
	}

	// Clear it live: stored in the file and applied immediately.
	if err := h.ctl.SetExitNode(""); err != nil {
		t.Fatal(err)
	}
	if got := h.lastTunnel().calls(); len(got) != 2 || got[1] != "" {
		t.Fatalf("calls after clear: %v", got)
	}
	if st := h.ctl.Status(context.Background()); st.ExitNode != "" || st.ExitNodeIP != "" {
		t.Fatalf("status after clear: %+v", st)
	}
	file, _ := loadConfig(h.cfgPath)
	if file.ExitNode != "" {
		t.Fatalf("file still has %q", file.ExitNode)
	}

	// Select again.
	if err := h.ctl.SetExitNode(" homeassistant "); err != nil {
		t.Fatal(err)
	}
	file, _ = loadConfig(h.cfgPath)
	if file.ExitNode != "homeassistant" || h.ctl.Status(context.Background()).ExitNodeIP != "100.64.0.5" {
		t.Fatalf("file %q, status %+v", file.ExitNode, h.ctl.Status(context.Background()))
	}
}

func TestExitNodeSetWhileStoppedIsStoredOnly(t *testing.T) {
	h := newHarness(t, twoHosts(), overrides{})
	if err := h.ctl.SetExitNode("homeassistant"); err != nil {
		t.Fatal(err)
	}
	if h.lastTunnel() != nil {
		t.Fatal("no tunnel should exist while stopped")
	}
	if st := h.ctl.Status(context.Background()); st.ExitNode != "homeassistant" {
		t.Fatalf("status: %+v", st)
	}
	if err := h.ctl.Connect(); err != nil {
		t.Fatal(err)
	}
	h.waitState(stateRunning)
	waitFor(t, func() bool { return len(h.lastTunnel().calls()) == 1 }, "exit node applied at connect")
}

func TestExitNodeFailureIsAWarningNotFatal(t *testing.T) {
	cfg := twoHosts()
	cfg.ExitNode = "nope"
	h := newHarness(t, cfg, overrides{})
	h.proto.setExitErr = errors.New("exit node not found")
	if err := h.ctl.Connect(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		st := h.ctl.Status(context.Background())
		return st.State == stateRunning && strings.Contains(st.Warning, "exit node not found")
	}, "warning")
	waitFor(t, func() bool { return len(h.relayList()) == 2 }, "relays still started")
}

func TestCommandLineOverridesAreNotSavedAndPageWins(t *testing.T) {
	h := newHarness(t, twoHosts(), overrides{ExitNode: "from-flag", Hosts: []hostSpec{{Name: "extra", Address: "9.9.9.9"}}})
	st := h.ctl.Status(context.Background())
	if st.ExitNode != "from-flag" || len(st.Hosts) != 3 {
		t.Fatalf("override not applied: %+v", st)
	}
	file, _ := loadConfig(h.cfgPath)
	if file.ExitNode != "" || len(file.Hosts) != 2 {
		t.Fatalf("overrides leaked to the file: %+v", file)
	}
	if err := h.ctl.SetExitNode("homeassistant"); err != nil {
		t.Fatal(err)
	}
	if st := h.ctl.Status(context.Background()); st.ExitNode != "homeassistant" {
		t.Fatalf("page choice must win over --exit-node: %+v", st)
	}
}

func TestSetHostsRestartsRelaysWhenRunning(t *testing.T) {
	h := newHarness(t, twoHosts(), overrides{})
	if err := h.ctl.Connect(); err != nil {
		t.Fatal(err)
	}
	h.waitState(stateRunning)
	waitFor(t, func() bool { return len(h.relayList()) == 2 }, "initial relays")
	old := h.relayList()

	if err := h.ctl.SetHosts([]hostSpec{{Name: " nas ", Address: " 192.168.1.30 "}}); err != nil {
		t.Fatal(err)
	}
	for i, r := range old {
		if !r.isClosed() {
			t.Fatalf("old relay %d not closed", i)
		}
	}
	rl := h.relayList()
	if len(rl) != 3 || rl[2].cfg.Name != "nas" || rl[2].cfg.Target != "192.168.1.30" || rl[2].cfg.ListenIP != "127.77.0.1" {
		t.Fatalf("relays after SetHosts: %d, last %+v", len(rl), rl[len(rl)-1].cfg)
	}
	file, _ := loadConfig(h.cfgPath)
	if len(file.Hosts) != 1 || file.Hosts[0].Name != "nas" {
		t.Fatalf("file: %+v", file.Hosts)
	}
	if st := h.ctl.Status(context.Background()); len(st.Hosts) != 1 || st.State != stateRunning {
		t.Fatalf("status: %+v", st)
	}
}

func TestSetHostsValidation(t *testing.T) {
	h := newHarness(t, twoHosts(), overrides{})
	bad := [][]hostSpec{
		{{Name: "a", Address: "x"}, {Name: "A", Address: "y"}}, // duplicate (case-insensitive)
		{{Name: "", Address: "x"}},
		{{Name: "a", Address: ""}},
	}
	for i, hosts := range bad {
		if err := h.ctl.SetHosts(hosts); err == nil {
			t.Errorf("case %d: expected an error", i)
		}
	}
	file, _ := loadConfig(h.cfgPath)
	if len(file.Hosts) != 2 {
		t.Fatalf("invalid input must not modify the file: %+v", file.Hosts)
	}
}

func TestRelayFailureEndsSessionAndReleasesEverything(t *testing.T) {
	h := newHarness(t, twoHosts(), overrides{})
	h.failIP = "127.77.0.2"
	if err := h.ctl.Connect(); err != nil {
		t.Fatal(err)
	}
	st := h.waitState(stateError)
	if !strings.Contains(st.Error, "127.77.0.2") || !strings.Contains(st.Error, "address in use") {
		t.Fatalf("error text: %q", st.Error)
	}
	waitFor(t, func() bool {
		rl := h.relayList()
		return len(rl) == 1 && rl[0].isClosed() && h.lastTunnel().isClosed()
	}, "cleanup after relay failure")
}

func TestProfiles(t *testing.T) {
	h := newHarness(t, twoHosts(), overrides{})

	if err := h.ctl.CreateProfile("travail", "https://hs.example.org", "pc-travail"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "default", "Default", "travail", "TRAVAIL", "../x", "a/b"} {
		if err := h.ctl.CreateProfile(bad, "", ""); err == nil {
			t.Errorf("profile name %q must be rejected", bad)
		}
	}
	if err := h.ctl.CreateProfile("x", "ftp://nope", ""); err == nil {
		t.Error("a control URL must be http(s)")
	}
	if st := h.ctl.Status(context.Background()); len(st.Profiles) != 2 || st.Profiles[0] != "default" || st.Profiles[1] != "travail" {
		t.Fatalf("profiles: %v", st.Profiles)
	}

	// The new profile has no PC yet.
	if err := h.ctl.SelectProfile("travail"); err != nil {
		t.Fatal(err)
	}
	if err := h.ctl.Connect(); !errors.Is(err, errNoHosts) {
		t.Fatalf("got %v", err)
	}
	if err := h.ctl.SetHosts([]hostSpec{{Name: "bureau", Address: "10.1.1.5"}}); err != nil {
		t.Fatal(err)
	}
	if err := h.ctl.Connect(); err != nil {
		t.Fatal(err)
	}
	h.waitState(stateRunning)
	tun := h.lastTunnel()
	want := filepath.Join(h.base, "profiles", "travail")
	if tun.stateDir != want || tun.profile.ControlURL != "https://hs.example.org" || tun.profile.Hostname != "pc-travail" {
		t.Fatalf("tunnel for profile: dir %q, %+v", tun.stateDir, tun.profile)
	}

	// Switching while connected reconnects with the other identity.
	if err := h.ctl.SelectProfile("default"); err != nil {
		t.Fatal(err)
	}
	h.waitState(stateRunning)
	if !tun.isClosed() {
		t.Fatal("previous profile's tunnel must be closed")
	}
	if got := h.lastTunnel(); got == tun || got.stateDir != h.base {
		t.Fatalf("default profile must use %q, got %q", h.base, got.stateDir)
	}
	file, _ := loadConfig(h.cfgPath)
	if file.Active != "" && !strings.EqualFold(file.Active, "default") {
		t.Fatalf("active in file: %q", file.Active)
	}
	if len(file.Hosts) != 2 {
		t.Fatalf("default profile's PCs must be untouched: %+v", file.Hosts)
	}

	// Delete removes the profile and its identity directory.
	if err := os.MkdirAll(want, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := h.ctl.DeleteProfile("travail"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatalf("identity directory must be removed: %v", err)
	}
	if st := h.ctl.Status(context.Background()); len(st.Profiles) != 1 {
		t.Fatalf("profiles after delete: %v", st.Profiles)
	}
	if err := h.ctl.DeleteProfile("default"); err == nil {
		t.Fatal("the default profile cannot be deleted")
	}
	if err := h.ctl.DeleteProfile("nope"); err == nil {
		t.Fatal("unknown profile must be an error")
	}
}

func TestDeletingTheActiveProfileFallsBackToDefault(t *testing.T) {
	h := newHarness(t, twoHosts(), overrides{})
	if err := h.ctl.CreateProfile("autre", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := h.ctl.SelectProfile("autre"); err != nil {
		t.Fatal(err)
	}
	if err := h.ctl.DeleteProfile("autre"); err != nil {
		t.Fatal(err)
	}
	st := h.ctl.Status(context.Background())
	if st.Profile != defaultProfileName || len(st.Hosts) != 2 {
		t.Fatalf("status: %+v", st)
	}
}
