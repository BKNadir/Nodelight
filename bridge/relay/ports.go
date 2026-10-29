// Package relay forwards the TCP and UDP ports used by a Sunshine/Apollo host
// from a dedicated loopback address to the same ports on a remote machine,
// through an arbitrary Dialer (in production, a Tailscale tsnet server).
//
// Moonlight's streaming code uses raw sockets and takes the video, audio and
// control ports from the host's RTSP replies, so the relay has to listen on
// exactly the same port numbers as the real host. To support several hosts at
// once, each host gets its own loopback IP (127.77.0.1, 127.77.0.2, ...).
package relay

// Ports lists the ports to forward. The same number is used locally and on
// the remote host.
type Ports struct {
	TCP []uint16
	UDP []uint16
}

// DefaultBasePort is the HTTP port of GameStream-compatible hosts. All other
// ports are fixed offsets from it.
const DefaultBasePort uint16 = 47989

// DefaultPorts returns the Sunshine/Apollo port set for the given HTTP base
// port (0 means DefaultBasePort).
//
// Offsets follow the Sunshine documentation:
//
//	TCP  base-5   HTTPS (47984)
//	TCP  base     HTTP  (47989)
//	TCP  base+1   Web UI (47990), optional
//	TCP  base+21  RTSP  (48010)
//	UDP  base+9   video (47998)
//	UDP  base+10  control (47999)
//	UDP  base+11  audio (48000)
//	UDP  base+13  microphone (48002)
func DefaultPorts(base uint16, withWebUI bool) Ports {
	if base == 0 {
		base = DefaultBasePort
	}
	p := Ports{
		TCP: []uint16{base - 5, base, base + 21},
		UDP: []uint16{base + 9, base + 10, base + 11, base + 13},
	}
	if withWebUI {
		p.TCP = append(p.TCP, base+1)
	}
	return p
}
