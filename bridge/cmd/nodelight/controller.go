package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"nodelight/relay"
)

type state string

const (
	stateStopped    state = "stopped"
	stateStarting   state = "starting"
	stateNeedsLogin state = "needs-login"
	stateRunning    state = "running"
	stateError      state = "error"
)

// closer is what the controller needs from a running relay.
type closer interface{ Close() }

type relayStarter func(ctx context.Context, cfg relay.Config) (closer, error)

func startRealRelay(ctx context.Context, cfg relay.Config) (closer, error) {
	r, err := relay.Start(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// overrides are command-line values that take precedence over the config file
// for the profile active at startup. They are never written to the file.
type overrides struct {
	Hostname   string
	ControlURL string
	ExitNode   string
	Hosts      []hostSpec
}

type controllerConfig struct {
	ConfigPath string
	StateBase  string // "data" directory
	Prefix     string // loopback prefix, e.g. "127.77.0."
	WebUI      bool
	Factory    tunnelFactory
	StartRelay relayStarter
	Overrides  overrides
	Emit       func(event) // console events (optional)
	Logf       func(format string, args ...any)
}

type hostView struct {
	Name      string `json:"name"`
	Address   string `json:"address"`
	BasePort  uint16 `json:"basePort,omitempty"`
	Loopback  string `json:"loopback"`
	Reachable *bool  `json:"reachable,omitempty"`
}

type statusView struct {
	State       state          `json:"state"`
	AuthURL     string         `json:"authUrl,omitempty"`
	TailscaleIP string         `json:"tailscaleIp,omitempty"`
	ExitNode    string         `json:"exitNode"`
	ExitNodeIP  string         `json:"exitNodeIp,omitempty"`
	Warning     string         `json:"warning,omitempty"`
	Error       string         `json:"error,omitempty"`
	Profile     string         `json:"profile"`
	Profiles    []string       `json:"profiles"`
	ControlURL  string         `json:"controlUrl,omitempty"`
	Hostname    string         `json:"hostname"`
	Hosts       []hostView     `json:"hosts"`
	ExitNodes   []exitNodeInfo `json:"exitNodes"`
}

// controller owns the Tailscale connection and the relays and applies the
// changes requested from the settings page.
type controller struct {
	cc   controllerConfig
	root context.Context

	cfgMu sync.Mutex // serializes read-modify-write of the config file

	mu          sync.Mutex
	active      string
	over        overrides
	st          state
	authURL     string
	selfIP      string
	exitApplied string
	warn        string
	errMsg      string
	hosts       []hostView
	hostsGen    int
	gen         int
	cancel      context.CancelFunc
	done        chan struct{}
	sessCtx     context.Context
	tun         tunnel
	relays      []closer
}

func newController(root context.Context, cc controllerConfig) (*controller, error) {
	if cc.Factory == nil {
		cc.Factory = newTSTunnel
	}
	if cc.StartRelay == nil {
		cc.StartRelay = startRealRelay
	}
	if cc.Emit == nil {
		cc.Emit = func(event) {}
	}
	if cc.Logf == nil {
		cc.Logf = func(string, ...any) {}
	}
	cfg, err := loadConfig(cc.ConfigPath)
	if err != nil {
		return nil, err
	}
	return &controller{cc: cc, root: root, active: cfg.activeName(), over: cc.Overrides, st: stateStopped}, nil
}

// updateConfig applies fn to the freshly loaded config file and saves it.
func (c *controller) updateConfig(fn func(*fileConfig) error) error {
	c.cfgMu.Lock()
	defer c.cfgMu.Unlock()
	cfg, err := loadConfig(c.cc.ConfigPath)
	if err != nil {
		return err
	}
	if err := fn(&cfg); err != nil {
		return err
	}
	return saveConfig(c.cc.ConfigPath, cfg)
}

// effective returns the active profile as stored in the file, with the
// command-line overrides applied.
func (c *controller) effective() (profile, error) {
	cfg, err := loadConfig(c.cc.ConfigPath)
	if err != nil {
		return profile{}, err
	}
	c.mu.Lock()
	name, over := c.active, c.over
	c.mu.Unlock()
	p, ok := cfg.profile(name)
	if !ok {
		p, _ = cfg.profile(defaultProfileName)
	}
	if over.Hostname != "" {
		p.Hostname = over.Hostname
	}
	if over.ControlURL != "" {
		p.ControlURL = over.ControlURL
	}
	if over.ExitNode != "" {
		p.ExitNode = over.ExitNode
	}
	if len(over.Hosts) > 0 {
		p.Hosts = mergeHosts(p.Hosts, over.Hosts)
	}
	if p.Hostname == "" {
		p.Hostname = "nodelight"
	}
	return p, nil
}

func (c *controller) viewsFor(hosts []hostSpec) []hostView {
	v := make([]hostView, len(hosts))
	for i, h := range hosts {
		v[i] = hostView{Name: h.Name, Address: h.Address, BasePort: h.BasePort, Loopback: c.cc.Prefix + strconv.Itoa(i+1)}
	}
	return v
}

func validateHosts(hosts []hostSpec) error {
	if len(hosts) > 250 {
		return errors.New("too many PCs (max 250)")
	}
	seen := map[string]bool{}
	for _, h := range hosts {
		if err := h.validate(); err != nil {
			return err
		}
		k := strings.ToLower(strings.TrimSpace(h.Name))
		if seen[k] {
			return fmt.Errorf("duplicate PC name %q", h.Name)
		}
		seen[k] = true
	}
	return nil
}

func (c *controller) live(s state) bool {
	return s == stateStarting || s == stateNeedsLogin || s == stateRunning
}

// Connect starts the Tailscale connection of the active profile (no-op if it
// is already starting or running).
func (c *controller) Connect() error {
	prof, err := c.effective()
	if err != nil {
		return err
	}
	// No PC is needed to connect: signing in and choosing an exit node come
	// first, the PCs can be added once the tunnel is up.
	if err := validateHosts(prof.Hosts); err != nil {
		return err
	}

	// A previous session may still be shutting down.
	c.mu.Lock()
	prev := c.done
	c.mu.Unlock()
	if prev != nil {
		select {
		case <-prev:
		case <-time.After(10 * time.Second):
			return errors.New("the previous connection is still shutting down, try again")
		}
	}

	c.mu.Lock()
	if c.live(c.st) {
		c.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(c.root)
	c.gen++
	gen := c.gen
	done := make(chan struct{})
	c.cancel, c.done, c.sessCtx = cancel, done, ctx
	c.st = stateStarting
	c.authURL, c.selfIP, c.exitApplied, c.warn, c.errMsg = "", "", "", "", ""
	c.hosts = c.viewsFor(prof.Hosts)
	c.mu.Unlock()

	stateDir := profileStateDir(c.cc.StateBase, prof.Name)
	c.cc.Emit(event{Event: "starting", Message: stateDir})
	go c.session(ctx, gen, prof, stateDir, done)
	return nil
}

func (c *controller) session(ctx context.Context, gen int, prof profile, stateDir string, done chan struct{}) {
	defer close(done)

	tun, err := c.cc.Factory(prof, stateDir, func(u string) { c.onAuth(gen, u) })
	if err != nil {
		c.fail(gen, fmt.Errorf("open Tailscale identity: %w", err))
		return
	}
	c.mu.Lock()
	c.tun = tun
	c.mu.Unlock()
	defer c.teardown(tun)

	ip, err := tun.Up(ctx)
	if err != nil {
		if ctx.Err() == nil {
			c.fail(gen, fmt.Errorf("connect to Tailscale: %w", err))
		}
		return
	}
	c.mu.Lock()
	if gen != c.gen || ctx.Err() != nil {
		c.mu.Unlock()
		return
	}
	c.st, c.selfIP, c.authURL = stateRunning, ip, ""
	c.mu.Unlock()
	c.cc.Emit(event{Event: "connected", TailscaleIP: ip})

	if prof.ExitNode != "" {
		c.applyExitNode(ctx, gen, tun, prof.ExitNode)
	}
	if err := c.startRelays(ctx, gen, tun, prof.Hosts); err != nil {
		c.fail(gen, err)
		return
	}
	<-ctx.Done()
}

func (c *controller) teardown(tun tunnel) {
	c.mu.Lock()
	relays := c.relays
	c.relays = nil
	if c.tun == tun {
		c.tun = nil
	}
	c.mu.Unlock()
	for _, r := range relays {
		r.Close()
	}
	_ = tun.Close()
}

func (c *controller) onAuth(gen int, u string) {
	c.mu.Lock()
	if gen != c.gen || c.st != stateStarting {
		c.mu.Unlock()
		return
	}
	first := c.authURL != u
	c.st, c.authURL = stateNeedsLogin, u
	c.mu.Unlock()
	if first {
		c.cc.Emit(event{Event: "auth", AuthURL: u})
	}
}

// fail records an error, ends the session and lets the page show the reason.
func (c *controller) fail(gen int, err error) {
	c.mu.Lock()
	if gen != c.gen {
		c.mu.Unlock()
		return
	}
	c.st, c.errMsg = stateError, err.Error()
	cancel := c.cancel
	c.mu.Unlock()
	c.cc.Emit(event{Event: "error", Message: err.Error()})
	if cancel != nil {
		cancel()
	}
}

func (c *controller) applyExitNode(ctx context.Context, gen int, tun tunnel, want string) {
	ip, err := tun.SetExitNode(ctx, want)
	c.mu.Lock()
	if gen != c.gen {
		c.mu.Unlock()
		return
	}
	if err != nil {
		c.warn = fmt.Sprintf("exit node: %v", err)
		c.exitApplied = ""
	} else {
		c.warn = ""
		c.exitApplied = ip
	}
	c.mu.Unlock()
	switch {
	case err != nil:
		c.cc.Emit(event{Event: "warning", Message: fmt.Sprintf("exit node: %v", err)})
	case ip != "":
		c.cc.Emit(event{Event: "exitnode", ExitNode: ip})
	}
}

// startRelays (re)creates one relay per host, each on its own loopback IP, and
// probes every host in the background.
func (c *controller) startRelays(ctx context.Context, gen int, tun tunnel, hosts []hostSpec) error {
	var started []closer
	for i, h := range hosts {
		ip := c.cc.Prefix + strconv.Itoa(i+1)
		r, err := c.cc.StartRelay(ctx, relay.Config{
			Name:     h.Name,
			ListenIP: ip,
			Target:   h.Address,
			Ports:    relay.DefaultPorts(h.BasePort, c.cc.WebUI),
			Dialer:   relay.DialFunc(tun.DialContext),
			Logf:     c.cc.Logf,
		})
		if err != nil {
			for _, s := range started {
				s.Close()
			}
			return fmt.Errorf("PC %q on %s: %w (is a local Sunshine/Moonlight using these ports on that address?)", h.Name, ip, err)
		}
		started = append(started, r)
	}

	c.mu.Lock()
	if gen != c.gen {
		c.mu.Unlock()
		for _, s := range started {
			s.Close()
		}
		return nil
	}
	c.relays = started
	c.hosts = c.viewsFor(hosts)
	c.hostsGen++
	hg := c.hostsGen
	views := append([]hostView(nil), c.hosts...)
	c.mu.Unlock()

	for i, h := range hosts {
		go c.probe(ctx, tun, gen, hg, i, h, views[i].Loopback)
	}
	return nil
}

// probe checks that the host answers on its HTTP port (reported, never fatal).
func (c *controller) probe(ctx context.Context, tun tunnel, gen, hostsGen, idx int, h hostSpec, loopback string) {
	base := h.BasePort
	if base == 0 {
		base = relay.DefaultBasePort
	}
	dctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	ok := false
	if conn, err := tun.DialContext(dctx, "tcp", fmt.Sprintf("%s:%d", hostPort(h.Address), base)); err == nil {
		ok = true
		_ = conn.Close()
	}
	if ctx.Err() != nil {
		return
	}
	c.mu.Lock()
	if gen == c.gen && hostsGen == c.hostsGen && idx < len(c.hosts) {
		c.hosts[idx].Reachable = &ok
	}
	c.mu.Unlock()
	c.cc.Emit(event{Event: "host", Host: h.Name, Address: h.Address, Loopback: loopback, Reachable: &ok})
}

// hostPort brackets IPv6 literals so "host:port" can be built by formatting.
func hostPort(addr string) string {
	if strings.Contains(addr, ":") && !strings.HasPrefix(addr, "[") {
		return "[" + addr + "]"
	}
	return addr
}

// Disconnect stops the connection and the relays. The device identity is kept.
func (c *controller) Disconnect() {
	c.mu.Lock()
	cancel, done := c.cancel, c.done
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	}
	c.mu.Lock()
	wasLive := c.live(c.st) || c.st == stateError
	c.st = stateStopped
	c.authURL, c.selfIP, c.exitApplied, c.warn, c.errMsg = "", "", "", "", ""
	for i := range c.hosts {
		c.hosts[i].Reachable = nil
	}
	c.mu.Unlock()
	if wasLive {
		c.cc.Emit(event{Event: "stopping"})
	}
}

// SetExitNode stores the exit node ("" for none) and applies it right away
// when connected.
func (c *controller) SetExitNode(want string) error {
	want = strings.TrimSpace(want)
	err := c.updateConfig(func(cfg *fileConfig) error {
		c.mu.Lock()
		name := c.active
		c.mu.Unlock()
		p, ok := cfg.profile(name)
		if !ok {
			return fmt.Errorf("unknown profile %q", name)
		}
		p.ExitNode = want
		cfg.setProfile(p)
		return nil
	})
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.over.ExitNode = "" // the page's choice wins over --exit-node
	running, tun, ctx, gen := c.st == stateRunning, c.tun, c.sessCtx, c.gen
	c.mu.Unlock()
	if running && tun != nil {
		c.applyExitNode(ctx, gen, tun, want)
	}
	return nil
}

// SetHosts replaces the list of PCs of the active profile.
func (c *controller) SetHosts(hosts []hostSpec) error {
	for i := range hosts {
		hosts[i].Name = strings.TrimSpace(hosts[i].Name)
		hosts[i].Address = strings.TrimSpace(hosts[i].Address)
	}
	if err := validateHosts(hosts); err != nil {
		return err
	}
	err := c.updateConfig(func(cfg *fileConfig) error {
		c.mu.Lock()
		name := c.active
		c.mu.Unlock()
		p, ok := cfg.profile(name)
		if !ok {
			return fmt.Errorf("unknown profile %q", name)
		}
		p.Hosts = hosts
		cfg.setProfile(p)
		return nil
	})
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.over.Hosts = nil // the page's list is the complete list
	running, tun, ctx, gen := c.st == stateRunning, c.tun, c.sessCtx, c.gen
	old := c.relays
	if running {
		c.relays = nil
	} else {
		c.hosts = c.viewsFor(hosts)
	}
	c.mu.Unlock()
	if !running || tun == nil {
		return nil
	}
	for _, r := range old {
		r.Close()
	}
	if len(hosts) == 0 {
		c.mu.Lock()
		c.hosts = nil
		c.mu.Unlock()
		return nil
	}
	if err := c.startRelays(ctx, gen, tun, hosts); err != nil {
		c.fail(gen, err)
		return err
	}
	return nil
}

// SelectProfile switches the active profile, reconnecting if it was connected.
func (c *controller) SelectProfile(name string) error {
	cfg, err := loadConfig(c.cc.ConfigPath)
	if err != nil {
		return err
	}
	p, ok := cfg.profile(name)
	if !ok {
		return fmt.Errorf("unknown profile %q", name)
	}
	c.mu.Lock()
	wasLive := c.live(c.st)
	c.mu.Unlock()

	c.Disconnect()
	if err := c.updateConfig(func(cfg *fileConfig) error { cfg.Active = p.Name; return nil }); err != nil {
		return err
	}
	c.mu.Lock()
	c.active, c.over = p.Name, overrides{}
	c.hosts = c.viewsFor(p.Hosts)
	c.mu.Unlock()
	if wasLive {
		return c.Connect()
	}
	return nil
}

// CreateProfile adds an empty profile (it does not switch to it).
func (c *controller) CreateProfile(name, controlURL, hostname string) error {
	name = strings.TrimSpace(name)
	if err := validProfileName(name); err != nil {
		return err
	}
	controlURL = strings.TrimSpace(controlURL)
	if controlURL != "" && !strings.HasPrefix(controlURL, "https://") && !strings.HasPrefix(controlURL, "http://") {
		return errors.New("the control server URL must start with https://")
	}
	return c.updateConfig(func(cfg *fileConfig) error {
		for _, n := range cfg.profileNames() {
			if strings.EqualFold(n, name) {
				return fmt.Errorf("profile %q already exists", name)
			}
		}
		cfg.setProfile(profile{Name: name, ControlURL: controlURL, Hostname: strings.TrimSpace(hostname)})
		return nil
	})
}

// DeleteProfile removes a profile and its stored Tailscale identity.
func (c *controller) DeleteProfile(name string) error {
	if strings.EqualFold(name, defaultProfileName) {
		return errors.New("the default profile cannot be deleted")
	}
	cfg, err := loadConfig(c.cc.ConfigPath)
	if err != nil {
		return err
	}
	p, ok := cfg.profile(name)
	if !ok {
		return fmt.Errorf("unknown profile %q", name)
	}
	c.mu.Lock()
	isActive := strings.EqualFold(c.active, p.Name)
	c.mu.Unlock()
	if isActive {
		if err := c.SelectProfile(defaultProfileName); err != nil {
			return err
		}
	}
	if err := c.updateConfig(func(cfg *fileConfig) error {
		cfg.deleteProfile(p.Name)
		return nil
	}); err != nil {
		return err
	}
	dir := profileStateDir(c.cc.StateBase, p.Name)
	if filepath.Base(filepath.Dir(dir)) == "profiles" { // never touch anything else
		_ = os.RemoveAll(dir)
	}
	return nil
}

// Status returns the current state for the settings page.
func (c *controller) Status(ctx context.Context) statusView {
	eff, _ := c.effective()
	cfg, _ := loadConfig(c.cc.ConfigPath)

	c.mu.Lock()
	v := statusView{
		State:       c.st,
		AuthURL:     c.authURL,
		TailscaleIP: c.selfIP,
		ExitNode:    eff.ExitNode,
		ExitNodeIP:  c.exitApplied,
		Warning:     c.warn,
		Error:       c.errMsg,
		Profile:     c.active,
		ControlURL:  eff.ControlURL,
		Hostname:    eff.Hostname,
		Hosts:       append([]hostView{}, c.hosts...),
		ExitNodes:   []exitNodeInfo{},
	}
	tun, running := c.tun, c.st == stateRunning
	c.mu.Unlock()

	v.Profiles = cfg.profileNames()
	if v.State != stateRunning {
		v.Hosts = c.viewsFor(eff.Hosts) // nothing is relayed or probed yet
	}
	if running && tun != nil {
		qctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if exits, err := tun.ExitNodes(qctx); err == nil && exits != nil {
			v.ExitNodes = exits
		}
	}
	return v
}

// Shutdown disconnects; used when the application exits.
func (c *controller) Shutdown() { c.Disconnect() }
