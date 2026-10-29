package main

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

// peerInfo is the part of a tailnet peer's status needed to choose an exit
// node. It is kept free of Tailscale types so the selection logic can be unit
// tested.
type peerInfo struct {
	HostName string
	DNSName  string // MagicDNS name, usually with a trailing dot
	IPs      []netip.Addr
	ExitNode bool // the peer advertises itself as an exit node and it is approved
}

var errExitNodeNotFound = errors.New("exit node not found")

// resolveExitNode turns what the user typed (a Tailscale IP, a machine name or
// a MagicDNS name) into the exit node's Tailscale IP.
func resolveExitNode(peers []peerInfo, want string) (netip.Addr, error) {
	want = strings.TrimSpace(want)
	if want == "" {
		return netip.Addr{}, errors.New("empty exit node")
	}
	if a, err := netip.ParseAddr(want); err == nil {
		return a, nil
	}

	lw := strings.ToLower(strings.TrimSuffix(want, "."))
	var matches []peerInfo
	for _, p := range peers {
		dns := strings.ToLower(strings.TrimSuffix(p.DNSName, "."))
		label, _, _ := strings.Cut(dns, ".")
		if strings.EqualFold(p.HostName, lw) || dns == lw || label == lw {
			matches = append(matches, p)
		}
	}

	switch len(matches) {
	case 0:
		var names []string
		for _, p := range peers {
			if p.ExitNode {
				names = append(names, p.HostName)
			}
		}
		sort.Strings(names)
		if len(names) == 0 {
			return netip.Addr{}, fmt.Errorf("%w: %q (no machine of your tailnet offers to be an exit node: "+
				"enable \"Use as exit node\" on it and approve it in the Tailscale admin console)", errExitNodeNotFound, want)
		}
		return netip.Addr{}, fmt.Errorf("%w: %q (available exit nodes: %s)", errExitNodeNotFound, want, strings.Join(names, ", "))
	case 1:
		// ok
	default:
		return netip.Addr{}, fmt.Errorf("%q matches %d machines, use its Tailscale IP instead", want, len(matches))
	}

	p := matches[0]
	if !p.ExitNode {
		return netip.Addr{}, fmt.Errorf("%q is not an approved exit node: enable \"Use as exit node\" on it "+
			"and approve it in the Tailscale admin console (Machines > Edit route settings)", want)
	}
	// Prefer an IPv4 address.
	for _, a := range p.IPs {
		if a.Is4() {
			return a, nil
		}
	}
	if len(p.IPs) > 0 {
		return p.IPs[0], nil
	}
	return netip.Addr{}, fmt.Errorf("%q has no Tailscale IP yet", want)
}
