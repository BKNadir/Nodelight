package relay

import (
	"context"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testIP = "127.77.0.1"

func freeTCPPort(t *testing.T) uint16 {
	t.Helper()
	ln, err := net.Listen("tcp", testIP+":0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return uint16(ln.Addr().(*net.TCPAddr).Port)
}

func freeUDPPort(t *testing.T) uint16 {
	t.Helper()
	pc, err := net.ListenPacket("udp", testIP+":0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	return uint16(pc.LocalAddr().(*net.UDPAddr).Port)
}

// fakeRemote plays the role of the Sunshine host: the dialer used by the
// relay redirects "host:port" to a backend listening on 127.0.0.2:port.
type fakeRemote struct {
	dials atomic.Int64
}

func (f *fakeRemote) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	f.dials.Add(1)
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	return d.DialContext(ctx, network, net.JoinHostPort("127.0.0.2", port))
}

func startTCPEcho(t *testing.T, port uint16) {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.2", strconv.Itoa(int(port))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
}

func startUDPEcho(t *testing.T, port uint16, prefix string) {
	t.Helper()
	pc, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.2", strconv.Itoa(int(port))))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = pc.WriteTo(append([]byte(prefix), buf[:n]...), addr)
		}
	}()
}

func TestDefaultPorts(t *testing.T) {
	p := DefaultPorts(0, true)
	wantTCP := []uint16{47984, 47989, 48010, 47990}
	wantUDP := []uint16{47998, 47999, 48000, 48002}
	if !equal(p.TCP, wantTCP) || !equal(p.UDP, wantUDP) {
		t.Fatalf("got %+v", p)
	}
	p = DefaultPorts(48989, false)
	if !equal(p.TCP, []uint16{48984, 48989, 49010}) || !equal(p.UDP, []uint16{48998, 48999, 49000, 49002}) {
		t.Fatalf("custom base: got %+v", p)
	}
}

func equal(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTCPRoundTripAndHalfClose(t *testing.T) {
	port := freeTCPPort(t)
	startTCPEcho(t, port)

	r, err := Start(context.Background(), Config{
		Name: "t", ListenIP: testIP, Target: "host", Ports: Ports{TCP: []uint16{port}},
		Dialer: &fakeRemote{},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	c, err := net.Dial("tcp", net.JoinHostPort(testIP, strconv.Itoa(int(port))))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	payload := strings.Repeat("moonlight", 20000) // > one segment
	go func() {
		_, _ = c.Write([]byte(payload))
		_ = c.(*net.TCPConn).CloseWrite() // half-close: echo must still come back
	}()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	got, err := io.ReadAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != payload {
		t.Fatalf("echo mismatch: got %d bytes, want %d", len(got), len(payload))
	}
}

func TestUDPFlowsAreIndependent(t *testing.T) {
	port := freeUDPPort(t)
	startUDPEcho(t, port, "R:")
	fr := &fakeRemote{}

	r, err := Start(context.Background(), Config{
		Name: "t", ListenIP: testIP, Target: "host", Ports: Ports{UDP: []uint16{port}}, Dialer: fr,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	relayAddr := &net.UDPAddr{IP: net.ParseIP(testIP), Port: int(port)}
	newClient := func() *net.UDPConn {
		c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(testIP)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		return c
	}
	a, b := newClient(), newClient()

	for i := 0; i < 3; i++ { // several datagrams reuse the same flow
		if _, err := a.WriteToUDP([]byte("a"+strconv.Itoa(i)), relayAddr); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 64)
		n, from, err := a.ReadFromUDP(buf)
		if err != nil {
			t.Fatal(err)
		}
		if string(buf[:n]) != "R:a"+strconv.Itoa(i) {
			t.Fatalf("unexpected reply %q", buf[:n])
		}
		// The reply must appear to come from the relay's own socket.
		if !from.IP.Equal(relayAddr.IP) || from.Port != relayAddr.Port {
			t.Fatalf("reply came from %v, want %v", from, relayAddr)
		}
	}
	if _, err := b.WriteToUDP([]byte("b"), relayAddr); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, _, err := b.ReadFromUDP(buf)
	if err != nil || string(buf[:n]) != "R:b" {
		t.Fatalf("client b: %q, %v", buf[:n], err)
	}
	if got := fr.dials.Load(); got != 2 {
		t.Fatalf("expected one outbound flow per client (2), got %d dials", got)
	}
}

func TestUDPIdleFlowIsDropped(t *testing.T) {
	port := freeUDPPort(t)
	startUDPEcho(t, port, "")
	fr := &fakeRemote{}
	r, err := Start(context.Background(), Config{
		Name: "t", ListenIP: testIP, Target: "host", Ports: Ports{UDP: []uint16{port}},
		Dialer: fr, UDPIdle: 150 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	c, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP(testIP), Port: int(port)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ping := func() {
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err := c.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Read(make([]byte, 8)); err != nil {
			t.Fatal(err)
		}
	}
	ping()
	time.Sleep(600 * time.Millisecond) // well past UDPIdle
	ping()
	if got := fr.dials.Load(); got != 2 {
		t.Fatalf("flow should have been recreated after idle expiry: dials=%d", got)
	}
}

func TestStartIsAllOrNothing(t *testing.T) {
	busy := freeTCPPort(t)
	ln, err := net.Listen("tcp", net.JoinHostPort(testIP, strconv.Itoa(int(busy))))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	free := freeTCPPort(t)

	_, err = Start(context.Background(), Config{
		Name: "t", ListenIP: testIP, Target: "host",
		Ports:  Ports{TCP: []uint16{free, busy}},
		Dialer: &fakeRemote{},
	})
	if err == nil || !strings.Contains(err.Error(), strconv.Itoa(int(busy))) {
		t.Fatalf("want an error naming port %d, got %v", busy, err)
	}
	// The port bound before the failure must have been released again.
	again, err := net.Listen("tcp", net.JoinHostPort(testIP, strconv.Itoa(int(free))))
	if err != nil {
		t.Fatalf("port %d leaked after failed Start: %v", free, err)
	}
	again.Close()
}

func TestRejectsWildcardListenIP(t *testing.T) {
	for _, ip := range []string{"", "0.0.0.0", "::", "not-an-ip"} {
		if _, err := Start(context.Background(), Config{ListenIP: ip, Target: "h", Dialer: &fakeRemote{}}); err == nil {
			t.Fatalf("ListenIP %q should be rejected", ip)
		}
	}
}

func TestCloseStopsEverything(t *testing.T) {
	port := freeTCPPort(t)
	startTCPEcho(t, port)
	r, err := Start(context.Background(), Config{
		Name: "t", ListenIP: testIP, Target: "host", Ports: Ports{TCP: []uint16{port}}, Dialer: &fakeRemote{},
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp", net.JoinHostPort(testIP, strconv.Itoa(int(port))))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	done := make(chan struct{})
	go func() { r.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not return with an open connection")
	}
	if _, err := net.DialTimeout("tcp", net.JoinHostPort(testIP, strconv.Itoa(int(port))), time.Second); err == nil {
		t.Fatal("listener still accepting after Close")
	}
}
