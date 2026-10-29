// Nodelight connects to a tailnet in userspace (no driver, no admin rights) and
// exposes remote Sunshine/Apollo hosts on local loopback addresses so that
// Moonlight can stream through Tailscale.
//
//	Nodelight --host bureau=desktop-pc            # bridge only
//	Nodelight --host bureau=desktop-pc --moonlight .\client\NodelightClient.exe
//
// Each host receives its own loopback IP (127.77.0.1, 127.77.0.2, ...) in the
// order they are declared; add that IP as a PC in Moonlight. Connection,
// exit node, PCs and profiles can be changed at any time from Moonlight's
// settings page or from the browser page served on 127.0.0.1 (see api.go).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
)

type options struct {
	configPath string
	stateDir   string
	prefix     string
	webUI      bool
	json       bool
	noBrowser  bool
	moonlight  string
	noLaunch   bool
	over       overrides
}

type hostsFlag []hostSpec

func (h *hostsFlag) String() string { return fmt.Sprint(*h) }
func (h *hostsFlag) Set(v string) error {
	s, err := parseHostFlag(v)
	if err != nil {
		return err
	}
	*h = append(*h, s)
	return nil
}

// event is one line of --json output (also rendered as text otherwise).
type event struct {
	Event       string `json:"event"`
	Message     string `json:"message,omitempty"`
	AuthURL     string `json:"authUrl,omitempty"`
	TailscaleIP string `json:"tailscaleIp,omitempty"`
	Host        string `json:"host,omitempty"`
	Address     string `json:"address,omitempty"`
	Loopback    string `json:"loopback,omitempty"`
	ExitNode    string `json:"exitNode,omitempty"`
	Panel       string `json:"panel,omitempty"`
	Reachable   *bool  `json:"reachable,omitempty"`
}

type reporter struct {
	json   bool
	mu     sync.Mutex
	out    io.Writer
	errw   io.Writer
	dialog bool // no console: fatal errors are also shown in a message box
}

// fatal reports an error the user must see, even without a console.
func (r *reporter) fatal(msg string) {
	r.emit(event{Event: "error", Message: msg})
	if r.dialog {
		showMessage("Nodelight", msg, true)
	}
}

func (r *reporter) emit(e event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.json {
		_ = json.NewEncoder(r.out).Encode(e)
		return
	}
	switch e.Event {
	case "starting":
		fmt.Fprintf(r.out, "Starting Tailscale (state: %s)\n", e.Message)
	case "auth":
		fmt.Fprintf(r.out, "Sign in to Tailscale to authorize this device:\n  %s\n", e.AuthURL)
	case "connected":
		fmt.Fprintf(r.out, "Tailscale connected (%s)\n", e.TailscaleIP)
	case "exitnode":
		fmt.Fprintf(r.out, "Using exit node %s to reach hosts outside the tailnet\n", e.ExitNode)
	case "host":
		status := ""
		if e.Reachable != nil {
			if *e.Reachable {
				status = "  [reachable]"
			} else {
				status = "  [NOT reachable yet: is the PC on, Sunshine running, ACLs allowing it? With an exit node: is the PC's LAN address right?]"
			}
		}
		fmt.Fprintf(r.out, "  %-16s %-24s -> add %s in Moonlight%s\n", e.Host, e.Address, e.Loopback, status)
	case "panel":
		fmt.Fprintf(r.out, "Settings page: %s\n", e.Panel)
	case "launched":
		fmt.Fprintf(r.out, "Moonlight started: %s\n", e.Message)
	case "ready":
		fmt.Fprintln(r.out, "Bridge running. Press Ctrl+C to stop.")
	case "stopping":
		fmt.Fprintln(r.out, "Stopping...")
	case "warning":
		fmt.Fprintf(r.errw, "Warning: %s\n", e.Message)
	case "error":
		fmt.Fprintf(r.errw, "Error: %s\n", e.Message)
	}
}

func main() { os.Exit(run()) }

func run() int {
	// Nodelight.exe is a windowless program: use the terminal it was started
	// from if there is one, otherwise write what would be printed to
	// Nodelight.log beside the executable and show errors in message boxes.
	console := attachConsole()
	report := &reporter{out: os.Stdout, errw: os.Stderr}
	if console {
		setConsoleTitle("Nodelight")
	} else {
		report.dialog = true
		if f := openLogFile(); f != nil {
			defer f.Close()
			report.out, report.errw = f, f
		} else {
			report.out, report.errw = io.Discard, io.Discard
		}
	}

	opts, err := parseOptions()
	if err != nil {
		report.fatal(err.Error())
		return 2
	}
	report.json = opts.json

	release, first := singleInstance()
	if !first {
		report.fatal("Nodelight est déjà en cours d'exécution (cherchez sa fenêtre Moonlight ou fermez-la d'abord).")
		return 1
	}
	defer release()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	ctl, err := newController(ctx, controllerConfig{
		ConfigPath: opts.configPath,
		StateBase:  opts.stateDir,
		Prefix:     opts.prefix,
		WebUI:      opts.webUI,
		Overrides:  opts.over,
		Emit: func(e event) {
			report.emit(e)
			if e.Event == "auth" && !opts.noBrowser {
				if err := openBrowser(e.AuthURL); err != nil {
					report.emit(event{Event: "error", Message: fmt.Sprintf("open browser: %v", err)})
				}
			}
		},
		Logf: func(f string, a ...any) {
			if !opts.json {
				report.mu.Lock()
				fmt.Fprintf(report.errw, f+"\n", a...)
				report.mu.Unlock()
			}
		},
	})
	if err != nil {
		report.fatal(err.Error())
		return 2
	}
	defer ctl.Shutdown()

	moonlight := ""
	if !opts.noLaunch {
		moonlight = opts.moonlightPath()
	}

	prof, err := ctl.effective()
	if err != nil {
		report.fatal(err.Error())
		return 2
	}

	// First run of the portable app: ask for the PC instead of failing.
	if len(prof.Hosts) == 0 && !opts.json && stdinIsTerminal() {
		in := bufio.NewReader(os.Stdin)
		h, err := promptForHost(in, os.Stdout)
		if err != nil {
			report.emit(event{Event: "error", Message: fmt.Sprintf("no host entered: %v", err)})
			return 2
		}
		exit := ""
		if opts.over.ExitNode == "" {
			if ex, err := promptForExitNode(in, os.Stdout); err == nil {
				exit = ex
			}
		}
		if err := ctl.updateConfig(func(cfg *fileConfig) error {
			p, _ := cfg.profile(cfg.activeName())
			p.Hosts = []hostSpec{h}
			if exit != "" {
				p.ExitNode = exit
			}
			cfg.setProfile(p)
			return nil
		}); err != nil {
			report.emit(event{Event: "error", Message: fmt.Sprintf("could not save %s: %v", opts.configPath, err)})
		}
		prof, _ = ctl.effective()
	}
	// Without Moonlight there is nothing to do with an empty list; with it, the
	// PCs can still be added from the settings page.
	if len(prof.Hosts) == 0 && moonlight == "" {
		report.fatal(fmt.Sprintf("no host configured. Create %s or pass --host name=address", opts.configPath))
		return 2
	}
	// Without a console nobody would see a bridge-only run: Moonlight is the UI.
	if moonlight == "" && !opts.noLaunch && !console {
		report.fatal(`NodelightClient.exe est introuvable : il doit se trouver dans le dossier "client" à côté de Nodelight.exe.`)
		return 2
	}

	api, err := startControlServer(ctl)
	if err != nil {
		report.fatal(fmt.Sprintf("start settings server: %v", err))
		return 1
	}
	defer api.Close()
	report.emit(event{Event: "panel", Panel: api.PanelURL()})

	if len(prof.Hosts) > 0 {
		if err := ctl.Connect(); err != nil {
			report.fatal(err.Error())
			return 1
		}
	} else {
		report.emit(event{Event: "warning", Message: "no PC configured yet: add one in Moonlight's settings or on the settings page"})
	}

	// Launcher mode: run Moonlight and stop when it exits.
	if moonlight != "" {
		// Moonlight keeps its settings and pairing data next to itself when a
		// portable.dat file exists in its working directory (instead of the
		// registry / user profile), which keeps the whole app portable.
		if err := ensurePortable(filepath.Dir(moonlight)); err != nil {
			report.emit(event{Event: "error", Message: fmt.Sprintf("enable Moonlight portable mode: %v", err)})
		}
		cmd := exec.CommandContext(ctx, moonlight)
		cmd.Dir = filepath.Dir(moonlight)
		// The patched Moonlight shows a Tailscale section in its settings when
		// these are set.
		cmd.Env = append(os.Environ(), "NODELIGHT_API="+api.URL(), "NODELIGHT_TOKEN="+api.token)
		if len(prof.Hosts) == 0 {
			// Moonlight opens the Tailscale page so the first PC can be added.
			cmd.Env = append(cmd.Env, "NODELIGHT_FIRST_RUN=1")
		}
		if err := cmd.Start(); err != nil {
			report.fatal(fmt.Sprintf("Impossible de lancer Moonlight (%s) : %v", moonlight, err))
			return 1
		}
		report.emit(event{Event: "launched", Message: moonlight})
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		select {
		case <-exited:
		case <-ctx.Done():
			<-exited
		}
		report.emit(event{Event: "stopping"})
		return 0
	}

	report.emit(event{Event: "ready"})
	<-ctx.Done()
	report.emit(event{Event: "stopping"})
	return 0
}

// moonlightPath returns the Moonlight executable to launch, or "".
func (o options) moonlightPath() string {
	if o.moonlight != "" {
		return o.moonlight
	}
	exeDir, err := exeDir()
	if err != nil {
		return ""
	}
	for _, rel := range []string{`client\NodelightClient.exe`, `NodelightClient.exe`} {
		p := filepath.Join(exeDir, filepath.FromSlash(strings.ReplaceAll(rel, `\`, "/")))
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func parseOptions() (options, error) {
	dir, err := exeDir()
	if err != nil {
		return options{}, err
	}
	var o options
	var flagHosts hostsFlag
	flag.StringVar(&o.configPath, "config", filepath.Join(dir, "hosts.json"), "JSON file listing the hosts and profiles")
	flag.Var(&flagHosts, "host", "host to bridge, name=address[:basePort] (repeatable)")
	flag.StringVar(&o.stateDir, "state-dir", filepath.Join(dir, "data"), "directory for the persistent Tailscale identity")
	flag.StringVar(&o.over.Hostname, "hostname", "", "device name in the Tailscale admin console (default nodelight)")
	flag.StringVar(&o.prefix, "listen-prefix", "127.77.0.", "loopback prefix; host N listens on prefix+N")
	flag.BoolVar(&o.webUI, "web-ui", false, "also forward the Sunshine/Apollo web UI port (base+1)")
	flag.BoolVar(&o.json, "json", false, "write newline-delimited JSON events")
	flag.BoolVar(&o.noBrowser, "no-browser", false, "print the sign-in URL without opening a browser")
	flag.StringVar(&o.moonlight, "moonlight", "", "Moonlight executable to launch (default: client\\NodelightClient.exe beside Nodelight.exe)")
	flag.BoolVar(&o.noLaunch, "no-launch", false, "only run the bridge, do not start Moonlight")
	flag.StringVar(&o.over.ControlURL, "control-url", "", "alternative coordination server, e.g. a self-hosted Headscale (default: Tailscale)")
	flag.StringVar(&o.over.ExitNode, "exit-node", "", "Tailscale name or IP of an exit node used to reach hosts outside the tailnet (e.g. a PC addressed by its LAN IP)")
	flag.Parse()
	o.over.Hosts = flagHosts

	if abs, err := filepath.Abs(o.stateDir); err == nil {
		o.stateDir = abs
	}
	if !strings.HasSuffix(o.prefix, ".") || net.ParseIP(o.prefix+"1") == nil {
		return o, fmt.Errorf("invalid --listen-prefix %q (want something like 127.77.0.)", o.prefix)
	}
	return o, nil
}

func exeDir() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find executable directory: %w", err)
	}
	if rp, err := filepath.EvalSymlinks(p); err == nil {
		p = rp
	}
	return filepath.Dir(p), nil
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// ensurePortable creates dir\portable.dat if it does not exist.
func ensurePortable(dir string) error {
	p := filepath.Join(dir, "portable.dat")
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	return os.WriteFile(p, nil, 0o644)
}

// openLogFile opens Nodelight.log beside the executable (or in the temp
// directory when that folder is read-only). Each run starts a fresh log.
func openLogFile() *os.File {
	var dirs []string
	if d, err := exeDir(); err == nil {
		dirs = append(dirs, d)
	}
	dirs = append(dirs, os.TempDir())
	for _, d := range dirs {
		f, err := os.OpenFile(filepath.Join(d, "Nodelight.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err == nil {
			return f
		}
	}
	return nil
}

func stdinIsTerminal() bool {
	st, err := os.Stdin.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
