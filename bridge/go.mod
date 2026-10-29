module nodelight/bridge

go 1.26

require (
	nodelight/relay v0.0.0
	tailscale.com v1.102.5
)

replace nodelight/relay => ./relay

// go.sum and the indirect requirements are produced by `go mod tidy`
// (scripts/build-bridge.ps1 runs it). The tailscale.com version is the one
// PortableTailscale currently builds against.
