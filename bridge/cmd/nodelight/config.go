package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// hostSpec is one remote Sunshine/Apollo machine reachable over the tailnet.
type hostSpec struct {
	Name     string `json:"name"`
	Address  string `json:"address"`            // MagicDNS name or Tailscale IP
	BasePort uint16 `json:"basePort,omitempty"` // HTTP port, default 47989
}

// profile is one Tailscale identity plus the PCs reached through it. Each
// profile has its own state directory, so it can belong to a different tailnet
// (or to a self-hosted Headscale server).
type profile struct {
	Name       string `json:"name"`
	ControlURL string `json:"controlUrl,omitempty"`
	Hostname   string `json:"hostname,omitempty"`
	// ExitNode is the Tailscale name or IP of an exit node (for example a Home
	// Assistant box) used to reach hosts that are not on the tailnet themselves,
	// such as a PC addressed by its LAN IP.
	ExitNode string     `json:"exitNode,omitempty"`
	Hosts    []hostSpec `json:"hosts"`
}

// fileConfig is the content of hosts.json. The top-level hostname, controlUrl,
// exitNode and hosts fields are the implicit "default" profile, which keeps
// files written by earlier versions working unchanged.
type fileConfig struct {
	Hostname   string     `json:"hostname,omitempty"`
	ControlURL string     `json:"controlUrl,omitempty"`
	ExitNode   string     `json:"exitNode,omitempty"`
	Hosts      []hostSpec `json:"hosts"`

	// Active is the profile used at startup ("" or "default" for the implicit one).
	Active string `json:"active,omitempty"`
	// Profiles are the additional profiles.
	Profiles []profile `json:"profiles,omitempty"`
}

const defaultProfileName = "default"

var profileNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,31}$`)

func validProfileName(n string) error {
	if !profileNameRE.MatchString(n) {
		return fmt.Errorf("invalid profile name %q (letters, digits, space, _ . -; 32 characters max)", n)
	}
	if strings.EqualFold(n, defaultProfileName) {
		return fmt.Errorf("the name %q is reserved", defaultProfileName)
	}
	return nil
}

// activeName returns the profile to use at startup, falling back to "default"
// when the configured one no longer exists.
func (c fileConfig) activeName() string {
	for _, p := range c.Profiles {
		if strings.EqualFold(p.Name, c.Active) {
			return p.Name
		}
	}
	return defaultProfileName
}

// profileNames lists "default" first, then the additional profiles in file order.
func (c fileConfig) profileNames() []string {
	names := []string{defaultProfileName}
	for _, p := range c.Profiles {
		names = append(names, p.Name)
	}
	return names
}

// profile returns the named profile (case-insensitive).
func (c fileConfig) profile(name string) (profile, bool) {
	if name == "" || strings.EqualFold(name, defaultProfileName) {
		return profile{Name: defaultProfileName, ControlURL: c.ControlURL, Hostname: c.Hostname,
			ExitNode: c.ExitNode, Hosts: c.Hosts}, true
	}
	for _, p := range c.Profiles {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return profile{}, false
}

// setProfile replaces the profile with the same name, or appends it.
func (c *fileConfig) setProfile(p profile) {
	if p.Name == "" || strings.EqualFold(p.Name, defaultProfileName) {
		c.ControlURL, c.Hostname, c.ExitNode, c.Hosts = p.ControlURL, p.Hostname, p.ExitNode, p.Hosts
		return
	}
	for i := range c.Profiles {
		if strings.EqualFold(c.Profiles[i].Name, p.Name) {
			p.Name = c.Profiles[i].Name
			c.Profiles[i] = p
			return
		}
	}
	c.Profiles = append(c.Profiles, p)
}

// deleteProfile removes an additional profile; the default one cannot be removed.
func (c *fileConfig) deleteProfile(name string) bool {
	for i := range c.Profiles {
		if strings.EqualFold(c.Profiles[i].Name, name) {
			c.Profiles = append(c.Profiles[:i], c.Profiles[i+1:]...)
			if strings.EqualFold(c.Active, name) {
				c.Active = ""
			}
			return true
		}
	}
	return false
}

// profileStateDir is where a profile keeps its Tailscale identity: the base
// "data" directory for the default profile (as in earlier versions), a
// subdirectory for the others.
func profileStateDir(base, name string) string {
	if name == "" || strings.EqualFold(name, defaultProfileName) {
		return base
	}
	return filepath.Join(base, "profiles", name)
}

// loadConfig reads path. A missing file is not an error (returns an empty
// config) so that --host flags alone are enough.
func loadConfig(path string) (fileConfig, error) {
	var cfg fileConfig
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// parseHostFlag parses "name=address" or "name=address:basePort".
// Address may be a name, an IPv4 address, or a bracketed IPv6 address.
func parseHostFlag(s string) (hostSpec, error) {
	name, rest, ok := strings.Cut(s, "=")
	if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(rest) == "" {
		return hostSpec{}, fmt.Errorf("invalid --host %q (want name=address[:basePort])", s)
	}
	h := hostSpec{Name: strings.TrimSpace(name), Address: strings.TrimSpace(rest)}
	// Split an optional trailing :port, but leave bare IPv6 literals alone.
	if i := strings.LastIndex(h.Address, ":"); i > 0 && !strings.Contains(h.Address[i+1:], "]") &&
		(strings.Count(h.Address, ":") == 1 || strings.HasPrefix(h.Address, "[")) {
		p, err := strconv.ParseUint(h.Address[i+1:], 10, 16)
		if err != nil || p == 0 {
			return hostSpec{}, fmt.Errorf("invalid port in --host %q", s)
		}
		h.BasePort = uint16(p)
		h.Address = strings.Trim(h.Address[:i], "[]")
	}
	return h, nil
}

func (h hostSpec) validate() error {
	if strings.TrimSpace(h.Name) == "" {
		return errors.New("a host has no name")
	}
	if strings.TrimSpace(h.Address) == "" {
		return fmt.Errorf("host %q has no address", h.Name)
	}
	return nil
}

// mergeHosts returns file hosts overridden/extended by flag hosts (same name
// wins from the flags). Order is stable: it decides the loopback IP of each
// host, so keeping it deterministic keeps Moonlight's saved addresses valid.
func mergeHosts(fromFile, fromFlags []hostSpec) []hostSpec {
	out := append([]hostSpec(nil), fromFile...)
	for _, f := range fromFlags {
		replaced := false
		for i := range out {
			if strings.EqualFold(out[i].Name, f.Name) {
				out[i] = f
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, f)
		}
	}
	return out
}

// saveConfig writes cfg to path (private permissions: it names your machines).
func saveConfig(path string, cfg fileConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// promptForHost asks for the PC to connect to on first run, so the portable
// app works by double-click without hand-editing hosts.json.
func promptForHost(in io.Reader, out io.Writer) (hostSpec, error) {
	fmt.Fprint(out, "First run. Enter the Tailscale name or IP of your Sunshine/Apollo PC\n"+
		"(for example desktop-pc or 100.101.102.103, optionally :basePort): ")
	line, err := bufio.NewReader(in).ReadString('\n')
	addr := strings.TrimSpace(line)
	if addr == "" {
		if err == nil {
			err = errors.New("no address entered")
		}
		return hostSpec{}, err
	}
	return parseHostFlag("pc=" + addr)
}

// promptForExitNode asks for an optional exit node. An empty answer means none.
func promptForExitNode(in io.Reader, out io.Writer) (string, error) {
	fmt.Fprint(out, "Is your PC reached through a Tailscale exit node or subnet router (for example a Home Assistant\n"+
		"server on the same LAN)? Enter its Tailscale name or IP, or press Enter if the PC itself is on Tailscale: ")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
