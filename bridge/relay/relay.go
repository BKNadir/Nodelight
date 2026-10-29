package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// Dialer opens connections towards the remote host. *tsnet.Server satisfies
// it through its Dial method (see the adapter in cmd/nodelight).
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// DialFunc adapts a plain function to Dialer.
type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// DialContext implements Dialer.
func (f DialFunc) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return f(ctx, network, address)
}

// Config describes one forwarded host.
type Config struct {
	// Name is only used in log messages.
	Name string
	// ListenIP is the local address to bind, e.g. "127.77.0.1". It must not
	// be a wildcard: Moonlight expects replies to come from this exact IP.
	ListenIP string
	// Target is the remote host name or IP (tailnet name or Tailscale IP).
	Target string
	Ports  Ports
	Dialer Dialer
	// UDPIdle is how long a UDP flow may stay silent before it is dropped.
	// Defaults to 90 seconds.
	UDPIdle time.Duration
	// DialTimeout bounds every outbound connection attempt. Defaults to 10s.
	DialTimeout time.Duration
	// Logf receives diagnostics. Optional.
	Logf func(format string, args ...any)
}

// Relay is a running set of forwarders for one host.
type Relay struct {
	cfg    Config
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu        sync.Mutex
	tcpLn     []net.Listener
	udpConns  []net.PacketConn
	closeOnce sync.Once
}

// Start binds every listener and starts forwarding. It is all-or-nothing: if
// any port cannot be bound (for example because a local Sunshine already uses
// it on that address), nothing is left running and the error names the port.
func Start(parent context.Context, cfg Config) (*Relay, error) {
	if cfg.Dialer == nil {
		return nil, errors.New("relay: Dialer is required")
	}
	if cfg.Target == "" {
		return nil, errors.New("relay: Target is required")
	}
	ip := net.ParseIP(cfg.ListenIP)
	if ip == nil || ip.IsUnspecified() {
		return nil, fmt.Errorf("relay: ListenIP %q must be a specific IP address", cfg.ListenIP)
	}
	if cfg.UDPIdle <= 0 {
		cfg.UDPIdle = 90 * time.Second
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 10 * time.Second
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}

	ctx, cancel := context.WithCancel(parent)
	r := &Relay{cfg: cfg, ctx: ctx, cancel: cancel}

	for _, port := range cfg.Ports.TCP {
		ln, err := net.Listen("tcp", net.JoinHostPort(cfg.ListenIP, strconv.Itoa(int(port))))
		if err != nil {
			r.Close()
			return nil, fmt.Errorf("listen tcp %s:%d: %w", cfg.ListenIP, port, err)
		}
		r.mu.Lock()
		r.tcpLn = append(r.tcpLn, ln)
		r.mu.Unlock()
		r.wg.Add(1)
		go r.serveTCP(ln, port)
	}
	for _, port := range cfg.Ports.UDP {
		pc, err := net.ListenPacket("udp", net.JoinHostPort(cfg.ListenIP, strconv.Itoa(int(port))))
		if err != nil {
			r.Close()
			return nil, fmt.Errorf("listen udp %s:%d: %w", cfg.ListenIP, port, err)
		}
		r.mu.Lock()
		r.udpConns = append(r.udpConns, pc)
		r.mu.Unlock()
		r.wg.Add(1)
		go r.serveUDP(pc, port)
	}
	return r, nil
}

// Close stops all listeners and flows and waits for the goroutines to exit.
func (r *Relay) Close() {
	r.closeOnce.Do(func() {
		r.cancel()
		r.mu.Lock()
		for _, ln := range r.tcpLn {
			_ = ln.Close()
		}
		for _, pc := range r.udpConns {
			_ = pc.Close()
		}
		r.mu.Unlock()
	})
	r.wg.Wait()
}

func (r *Relay) remote(port uint16) string {
	return net.JoinHostPort(r.cfg.Target, strconv.Itoa(int(port)))
}

// ---- TCP ----

func (r *Relay) serveTCP(ln net.Listener, port uint16) {
	defer r.wg.Done()
	for {
		c, err := ln.Accept()
		if err != nil {
			return // listener closed
		}
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			r.handleTCP(c, port)
		}()
	}
}

func (r *Relay) handleTCP(local net.Conn, port uint16) {
	defer local.Close()
	dctx, cancel := context.WithTimeout(r.ctx, r.cfg.DialTimeout)
	remote, err := r.cfg.Dialer.DialContext(dctx, "tcp", r.remote(port))
	cancel()
	if err != nil {
		r.cfg.Logf("[%s] tcp %d: dial %s: %v", r.cfg.Name, port, r.remote(port), err)
		return
	}
	defer remote.Close()

	// Closing both ends when the relay stops unblocks the copy loops.
	stop := context.AfterFunc(r.ctx, func() {
		_ = local.Close()
		_ = remote.Close()
	})
	defer stop()

	done := make(chan struct{}, 2)
	go func() { pipe(remote, local); done <- struct{}{} }()
	go func() { pipe(local, remote); done <- struct{}{} }()
	<-done
	<-done
}

// pipe copies src to dst, then half-closes dst so the peer sees EOF while the
// opposite direction can still finish (HTTP/1.0-style responses rely on it).
func pipe(dst, src net.Conn) {
	_, _ = io.Copy(dst, src)
	if cw, ok := dst.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	} else {
		_ = dst.Close()
	}
}

// ---- UDP ----

type udpFlow struct {
	remote   net.Conn
	lastSeen time.Time // guarded by udpTable.mu
}

type udpTable struct {
	mu    sync.Mutex
	flows map[string]*udpFlow
}

func (r *Relay) serveUDP(pc net.PacketConn, port uint16) {
	defer r.wg.Done()
	tbl := &udpTable{flows: make(map[string]*udpFlow)}

	// Sweeper: drop flows that have been silent for UDPIdle.
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		t := time.NewTicker(r.cfg.UDPIdle / 3)
		defer t.Stop()
		for {
			select {
			case <-r.ctx.Done():
				return
			case now := <-t.C:
				tbl.mu.Lock()
				for k, f := range tbl.flows {
					if now.Sub(f.lastSeen) > r.cfg.UDPIdle {
						_ = f.remote.Close()
						delete(tbl.flows, k)
					}
				}
				tbl.mu.Unlock()
			}
		}
	}()

	defer func() {
		tbl.mu.Lock()
		for k, f := range tbl.flows {
			_ = f.remote.Close()
			delete(tbl.flows, k)
		}
		tbl.mu.Unlock()
	}()

	buf := make([]byte, 65535)
	for {
		n, client, err := pc.ReadFrom(buf)
		if err != nil {
			return // socket closed
		}
		key := client.String()

		tbl.mu.Lock()
		f := tbl.flows[key]
		tbl.mu.Unlock()

		if f == nil {
			dctx, cancel := context.WithTimeout(r.ctx, r.cfg.DialTimeout)
			rc, derr := r.cfg.Dialer.DialContext(dctx, "udp", r.remote(port))
			cancel()
			if derr != nil {
				r.cfg.Logf("[%s] udp %d: dial %s: %v", r.cfg.Name, port, r.remote(port), derr)
				continue
			}
			f = &udpFlow{remote: rc}
			tbl.mu.Lock()
			if existing := tbl.flows[key]; existing != nil {
				// Lost a race with the sweeper or a duplicate: keep one.
				_ = rc.Close()
				f = existing
			} else {
				tbl.flows[key] = f
				r.wg.Add(1)
				go r.udpReturn(pc, client, key, f, tbl)
			}
			tbl.mu.Unlock()
		}

		tbl.mu.Lock()
		f.lastSeen = time.Now()
		tbl.mu.Unlock()

		if _, werr := f.remote.Write(buf[:n]); werr != nil {
			r.cfg.Logf("[%s] udp %d: write: %v", r.cfg.Name, port, werr)
		}
	}
}

// udpReturn carries datagrams from the remote host back to the local client.
// The reply goes out of the listening socket so its source is ListenIP:port,
// which is what Moonlight's sockets expect.
func (r *Relay) udpReturn(pc net.PacketConn, client net.Addr, key string, f *udpFlow, tbl *udpTable) {
	defer r.wg.Done()
	buf := make([]byte, 65535)
	for {
		n, err := f.remote.Read(buf)
		if err != nil {
			tbl.mu.Lock()
			if tbl.flows[key] == f {
				delete(tbl.flows, key)
			}
			tbl.mu.Unlock()
			_ = f.remote.Close()
			return
		}
		tbl.mu.Lock()
		f.lastSeen = time.Now()
		tbl.mu.Unlock()
		if _, werr := pc.WriteTo(buf[:n], client); werr != nil {
			return
		}
	}
}
