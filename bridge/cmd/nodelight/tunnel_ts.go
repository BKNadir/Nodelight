package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/ipn/store"
	"tailscale.com/tsnet"
	"tailscale.com/types/logger"
)

var authURLPattern = regexp.MustCompile(`https?://[^\s"']+/a/[A-Za-z0-9_-]+`)

// tsTunnel is the production tunnel: an embedded Tailscale node in userspace
// (no driver, no admin rights).
type tsTunnel struct {
	srv        *tsnet.Server
	runtimeDir string
}

func newTSTunnel(p profile, stateDir string, onAuth func(string)) (tunnel, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	st, err := store.NewFileStore(logger.Discard, filepath.Join(stateDir, "tailscaled.state"))
	if err != nil {
		return nil, err
	}
	rt, err := os.MkdirTemp("", "nodelight-")
	if err != nil {
		return nil, fmt.Errorf("create runtime directory: %w", err)
	}
	hostname := p.Hostname
	if hostname == "" {
		hostname = "nodelight"
	}
	return &tsTunnel{
		runtimeDir: rt,
		srv: &tsnet.Server{
			Dir:        rt,
			Store:      st,
			Hostname:   hostname,
			ControlURL: p.ControlURL,
			Logf:       logger.Discard,
			UserLogf: func(format string, args ...any) {
				if u := authURLPattern.FindString(fmt.Sprintf(format, args...)); u != "" {
					onAuth(u)
				}
			},
		},
	}, nil
}

func (t *tsTunnel) Up(ctx context.Context) (string, error) {
	st, err := t.srv.Up(ctx)
	if err != nil {
		return "", err
	}
	if st != nil && len(st.TailscaleIPs) > 0 {
		return st.TailscaleIPs[0].String(), nil
	}
	return "unknown", nil
}

func (t *tsTunnel) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return t.srv.Dial(ctx, network, address)
}

func (t *tsTunnel) Close() error {
	err := t.srv.Close()
	_ = os.RemoveAll(t.runtimeDir)
	return err
}

func (t *tsTunnel) peers(ctx context.Context) ([]peerInfo, []exitNodeInfo, error) {
	lc, err := t.srv.LocalClient()
	if err != nil {
		return nil, nil, fmt.Errorf("local client: %w", err)
	}
	st, err := lc.Status(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("read tailnet status: %w", err)
	}
	var peers []peerInfo
	var exits []exitNodeInfo
	for _, p := range st.Peer {
		peers = append(peers, peerInfo{HostName: p.HostName, DNSName: p.DNSName, IPs: p.TailscaleIPs, ExitNode: p.ExitNodeOption})
		if p.ExitNodeOption {
			info := exitNodeInfo{Name: p.HostName, Online: p.Online}
			for _, a := range p.TailscaleIPs {
				if a.Is4() {
					info.IP = a.String()
					break
				}
			}
			if info.IP == "" && len(p.TailscaleIPs) > 0 {
				info.IP = p.TailscaleIPs[0].String()
			}
			exits = append(exits, info)
		}
	}
	sort.Slice(exits, func(i, j int) bool { return exits[i].Name < exits[j].Name })
	return peers, exits, nil
}

func (t *tsTunnel) ExitNodes(ctx context.Context) ([]exitNodeInfo, error) {
	_, exits, err := t.peers(ctx)
	return exits, err
}

// SetExitNode routes this node's non-tailnet traffic (for example the LAN
// address of a PC that is not on Tailscale) through the given exit node.
func (t *tsTunnel) SetExitNode(ctx context.Context, want string) (string, error) {
	lc, err := t.srv.LocalClient()
	if err != nil {
		return "", fmt.Errorf("local client: %w", err)
	}

	if want == "" { // clear the selection (the node may have stored it as an ID)
		_, err := lc.EditPrefs(ctx, &ipn.MaskedPrefs{ExitNodeIPSet: true, ExitNodeIDSet: true})
		if err != nil {
			return "", fmt.Errorf("clear exit node: %w", err)
		}
		return "", nil
	}

	var addr netip.Addr
	// Peers can appear a moment after the node is up: retry a few times.
	for attempt := 0; ; attempt++ {
		peers, _, err := t.peers(ctx)
		if err != nil {
			return "", err
		}
		addr, err = resolveExitNode(peers, want)
		if err == nil {
			break
		}
		if !errors.Is(err, errExitNodeNotFound) || attempt >= 4 {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
		}
	}

	_, err = lc.EditPrefs(ctx, &ipn.MaskedPrefs{
		Prefs:         ipn.Prefs{ExitNodeIP: addr},
		ExitNodeIPSet: true,
	})
	if err != nil {
		return "", fmt.Errorf("select exit node %s: %w", addr, err)
	}
	return addr.String(), nil
}
