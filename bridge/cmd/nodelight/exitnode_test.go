package main

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func ip(s string) netip.Addr { return netip.MustParseAddr(s) }

var testPeers = []peerInfo{
	{HostName: "homeassistant", DNSName: "homeassistant.tail1234.ts.net.", IPs: []netip.Addr{ip("fd7a:115c:a1e0::1"), ip("100.64.0.5")}, ExitNode: true},
	{HostName: "laptop", DNSName: "laptop.tail1234.ts.net.", IPs: []netip.Addr{ip("100.64.0.9")}},
	{HostName: "nas", DNSName: "nas.tail1234.ts.net.", IPs: []netip.Addr{ip("100.64.0.7")}, ExitNode: true},
	{HostName: "dup", DNSName: "dup.tail1234.ts.net.", IPs: []netip.Addr{ip("100.64.0.20")}, ExitNode: true},
	{HostName: "DUP", DNSName: "dup-2.tail1234.ts.net.", IPs: []netip.Addr{ip("100.64.0.21")}, ExitNode: true},
}

func TestResolveExitNode(t *testing.T) {
	ok := map[string]string{
		"100.64.0.99":                    "100.64.0.99", // literal IP is used as is
		"homeassistant":                  "100.64.0.5",  // IPv4 preferred
		"HomeAssistant":                  "100.64.0.5",  // case-insensitive
		"homeassistant.tail1234.ts.net":  "100.64.0.5",
		"homeassistant.tail1234.ts.net.": "100.64.0.5",
		"  nas  ":                        "100.64.0.7",
	}
	for in, want := range ok {
		got, err := resolveExitNode(testPeers, in)
		if err != nil || got != ip(want) {
			t.Errorf("%q: got %v, %v; want %s", in, got, err, want)
		}
	}
}

func TestResolveExitNodeErrors(t *testing.T) {
	if _, err := resolveExitNode(testPeers, ""); err == nil {
		t.Error("empty name must fail")
	}

	_, err := resolveExitNode(testPeers, "unknown")
	if !errors.Is(err, errExitNodeNotFound) || !strings.Contains(err.Error(), "homeassistant") || strings.Contains(err.Error(), "laptop") {
		t.Errorf("unknown: want a not-found error listing only exit nodes, got %v", err)
	}

	_, err = resolveExitNode(testPeers, "laptop")
	if err == nil || errors.Is(err, errExitNodeNotFound) || !strings.Contains(err.Error(), "not an approved exit node") {
		t.Errorf("laptop: want a 'not an approved exit node' error, got %v", err)
	}

	if _, err := resolveExitNode(testPeers, "dup"); err == nil || !strings.Contains(err.Error(), "matches 2") {
		t.Errorf("dup: want an ambiguity error, got %v", err)
	}

	_, err = resolveExitNode([]peerInfo{{HostName: "laptop"}}, "x")
	if !errors.Is(err, errExitNodeNotFound) || !strings.Contains(err.Error(), "Use as exit node") {
		t.Errorf("no exit node at all: want a helpful hint, got %v", err)
	}

	_, err = resolveExitNode([]peerInfo{{HostName: "ha", ExitNode: true}}, "ha")
	if err == nil || !strings.Contains(err.Error(), "no Tailscale IP") {
		t.Errorf("peer without IP: got %v", err)
	}
}
