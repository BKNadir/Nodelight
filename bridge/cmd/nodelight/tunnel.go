package main

import (
	"context"
	"net"
)

// exitNodeInfo describes a tailnet machine that offers to be an exit node.
type exitNodeInfo struct {
	Name   string `json:"name"`
	IP     string `json:"ip"`
	Online bool   `json:"online"`
}

// tunnel is the Tailscale connection used by the controller. The production
// implementation wraps tsnet (tunnel_ts.go); tests use a fake.
type tunnel interface {
	// Up blocks until the node is connected and returns its Tailscale IP.
	// While waiting for the user to sign in, the factory's auth callback is called.
	Up(ctx context.Context) (selfIP string, err error)
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
	// ExitNodes lists the exit nodes of the tailnet.
	ExitNodes(ctx context.Context) ([]exitNodeInfo, error)
	// SetExitNode selects an exit node by Tailscale IP or name and returns its
	// IP. An empty name clears the selection.
	SetExitNode(ctx context.Context, want string) (ip string, err error)
	Close() error
}

// tunnelFactory builds an (unstarted) tunnel for a profile. onAuth is called
// with the sign-in URL when the device needs to be authorized.
type tunnelFactory func(p profile, stateDir string, onAuth func(url string)) (tunnel, error)
