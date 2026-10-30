<p align="center"><img src="assets/icon.png" alt="Nodelight" width="160"></p>

<p align="center"><b>English</b> · <a href="README.fr.md">Français</a> · <a href="README.nl.md">Nederlands</a> · <a href="README.de.md">Deutsch</a> · <a href="README.es.md">Español</a></p>

# Nodelight

A **portable Moonlight client for Windows** that connects to your Sunshine/Apollo PC **through
Tailscale**: no installation, no driver, no administrator rights, and no port to open on your
router.

> **AI-generated project.** All the code, scripts and documentation in this repository were written
> by **Claude Sonnet 5.5** (Anthropic), through Claude Code, under the direction of the repository
> owner ("vibe coding"). The code has **not been audited by a human** and has only been **partially
> tested on a real machine** (it builds, starts and displays; a full stream through Tailscale is not
> confirmed yet): use it at your own risk (see "Verification status").

This repository is a **fork of [moonlight-qt](https://github.com/moonlight-stream/moonlight-qt)**: the
client code is Moonlight's, with Nodelight's changes added as commits on top (see `git log`). The
original repository is configured as `upstream` to pull its updates. The rest of this file only
describes what Nodelight adds.

Nodelight is not affiliated with Tailscale Inc., the Moonlight project, or Artemis/Apollo/Sunshine.
Tailscale is a trademark of Tailscale Inc.

## How it works

| Component | Language | Role |
| --- | --- | --- |
| `bridge/` → `Nodelight.exe` | Go | Userspace Tailscale node (`tsnet`) + local TCP/UDP relay, starts Moonlight |
| `app/`, `moonlight-common-c/`… → `client\NodelightClient.exe` | C++/Qt | The modified Moonlight client: "remote" mode for the relay, Apollo virtual display option ported from Artemis, built-in Tailscale page, Nodelight name and icon |

Moonlight streams over UDP (video, audio, control) with raw sockets that cannot go through a SOCKS5
proxy, and it reads port numbers from the host's RTSP replies. Nodelight therefore listens on **the
same ports as Sunshine** on a dedicated loopback address per PC (`127.77.0.1`, `127.77.0.2`, …) and
forwards them through the tailnet.

```
Moonlight ──► 127.77.0.1:47989/47984/48010 (TCP), :47998-48002 (UDP)
                 │  Nodelight (relay + tsnet/WireGuard, userspace)
                 ▼
          Sunshine/Apollo host PC  (through your tailnet)
```

Ports relayed per host (base 47989): TCP 47984, 47989, 48010 (+ 47990 with `--web-ui`),
UDP 47998, 47999, 48000, 48002.

## Usage

1. Double-click `Nodelight.exe`: **no console opens**, Moonlight starts directly (in portable mode,
   thanks to a `portable.dat` file: settings and pairings stay in the folder). Nodelight closes with
   Moonlight. What it would have printed is written to `Nodelight.log`, and startup errors are shown
   in a dialog box.
2. On first launch, Moonlight opens its **Tailscale** page (a button with a network icon in the top
   bar). Click **Connect**: a web page opens to authorize the device (once). The identity is stored in
   `data\` (keep this folder private; it can be copied from one PC to another). No PC is needed to
   connect.
3. Once connected, pick the **exit node** if needed (see below), then **Add a PC**: a name and the
   Tailscale name or IP of the PC, then **Save PCs**. The PC is taken into account immediately.
4. The PCs appear **automatically** in Moonlight (`127.77.0.1`, `127.77.0.2`… in list order; do not
   reorder them if PCs are already paired). Pair as usual.

Started from a terminal, `Nodelight.exe` writes to that terminal and asks the first-run questions
(useful with `--json` or `--no-launch`).

Useful options: `--host name=address[:basePort]` (repeatable), `--hostname`, `--state-dir`,
`--no-browser`, `--json` (line-by-line events), `--listen-prefix`, `--web-ui`, `--control-url` for a
self-hosted Headscale server, and `--exit-node` (see below).

### Settings page

Connection, exit node, PCs and profiles can be set without editing any file, in two ways:

- **In Moonlight**: **Tailscale (Nodelight)** button (network icon) in the top bar. The page has
  clickable controls for: the connection state and **Connect / Disconnect** (plus a button to open the
  Tailscale sign-in page), the **exit node** (list from your tailnet + **Apply exit node**), the **PC
  list** (name, address, base port, local address `127.77.0.N`, green dot when the PC answers,
  **Add a PC** / ✕ / **Save PCs**) and the **profiles** (switch, delete, create). The button only
  appears if Moonlight was started by Nodelight. A shorter equivalent section remains in Settings.
- **In the browser** (fallback): the address is written to `Nodelight.log`
  (`Settings page: http://127.0.0.1:PORT/?token=…`) or to the terminal; same content.

A **profile** groups a Tailscale identity, a device name, an optional control server (Headscale) and a
list of PCs. It lets you switch from one Tailscale network to another; each profile keeps its
identity in `data\profiles\<name>`, the default profile stays in `data\`. Profiles are saved in
`hosts.json` (files from earlier versions remain valid).

Notes:
- Disconnecting, switching profile or changing the exit node **interrupts the stream in progress**.
- A value given on the command line (`--exit-node`, `--host`…) applies at startup without being
  saved; a choice made afterwards in the page replaces it.
- Security: the server only listens on `127.0.0.1`, every call requires a random token generated at
  each launch (passed to Moonlight through an environment variable), and requests whose `Host` header
  is not the local address are rejected (protection against "DNS rebinding"). Another program running
  in your Windows session can still read that environment: this is not a security boundary against
  code already running on your PC.

### Host PC that is not on Tailscale (exit node)

If your Sunshine/Apollo PC does not have Tailscale but another machine on its local network (Home
Assistant, NAS, Raspberry Pi…) is on your tailnet **as an exit node**, Nodelight can go through it:

```json
{
  "exitNode": "homeassistant",
  "hosts": [ { "name": "desktop", "address": "192.168.1.20" } ]
}
```

- `exitNode`: Tailscale name or IP of the exit node (or `--exit-node`, or the list in the Tailscale
  page). `address` is then the host PC's **local address** (`192.168.x.y`), as seen from the exit
  node's network.
- The exit node must be approved in the Tailscale admin console (Machines → Edit route settings →
  Use as exit node). The `nodelight` device must be allowed to use it (true with the default ACLs).
- Same principle as a remote machine using that exit node: if your PC reaches the host's local
  address that way, Nodelight should too. **Untested**: I could not verify that the `tsnet` library
  really routes the streaming UDP traffic to a local address through an exit node. This is the first
  thing to test.
- Performance: the whole video stream goes through the exit node machine, WireGuard-encrypted. A
  small home-automation server may limit the throughput; lower the video bitrate in Moonlight if the
  picture stutters. If the host can run Tailscale, that is always simpler and faster (direct
  connection, `100.x` address or MagicDNS name instead of the local IP, and no exit node).

### Host side (Sunshine/Apollo)

- The host PC must be in the same tailnet and powered on, with Sunshine/Apollo running. Nodelight
  reports at startup whether each host answers on its HTTP port.
- The host's Windows firewall must allow Sunshine/Apollo on the Tailscale interface (relevant network
  profile); your Tailscale ACLs must allow these ports.
- By default Sunshine only allows pairing (`origin_pin_allowed`) from the local network: if pairing is
  refused, adjust that setting or pair once on the LAN.
- For good performance you need a **direct** Tailscale connection (not through a DERP relay);
  userspace WireGuard encryption uses a little CPU.

## Getting the build without a development environment

The binary is built on GitHub Actions' free Windows machines:

1. **Actions** tab → **Build Windows x64 (portable)** → **Run workflow** (30 to 60 minutes; a `push`
   to `master` also starts it). Moonlight's original workflows (AppImage, Steam Link, Windows/macOS
   signing) were removed from this fork: they need secrets that do not exist here.
2. Download the `Nodelight-win-x64` artifact (a zip) and unzip it: that is all.

The zip contains the Visual C++ runtime (local copy): no prerequisite on the target machine. No
privilege elevation is requested (`asInvoker` manifest, userspace bridge, local ports > 1024 on
loopback only). The executable is not signed: SmartScreen may ask for confirmation, and the Windows
firewall may offer a network permission that you can cancel.

## Manual build (Windows x64)

Prerequisites: Go 1.26+, Git, Qt 6.11+ (MSVC), Visual Studio 2026 (or the version moonlight-qt
requires), PowerShell.

```powershell
git clone --recurse-submodules https://github.com/BKNadir/Nodelight
cd Nodelight
.\scripts\build-bridge.ps1      # tests + dist\Nodelight.exe
.\setup-deps.ps1
scripts\build-arch.bat release  # from a Qt + MSVC command prompt
.\scripts\package.ps1           # dist\Nodelight-win-x64.zip
```

## What comes from Artemis, and what does not

Artemis is an **Android** client for Apollo. Many of its features make no sense on Windows or already
exist in moonlight-qt.

| Artemis feature | Status here |
| --- | --- |
| Apollo virtual display | **Done**: checkbox in the host settings + `--virtual-display` |
| Custom resolution and bitrate | Already in moonlight-qt |
| Apollo server commands | Not done: needs Artemis' fork of `moonlight-common-c` (`LiSendExecServerCmd`) |
| Clipboard synchronization | Not done: `actions/clipboard` HTTP calls + `QClipboard` integration to write |
| Virtual buttons, touch trackpad, DeX, portrait, SBS 3D… | Android-specific, not applicable |

## Verification status — please read

Verified in the development environment (without Windows):
- relay tests (TCP round trip with half-close, independent UDP flows per client, idle flow expiry,
  all-or-nothing binding, clean shutdown): OK with `-race`;
- tests of configuration parsing, profiles and first-run input;
- controller tests (connection with or without PCs, disconnection, connection waiting for
  authorization, errors, hot exit node change, PC list, profile switch and deletion) and local API
  tests (token, `Host` guard, validation), with a fake Tailscale, run several times with `-race`;
- the JavaScript logic of the QML pages was run under Node with the real JSON produced by the Go code;
- the client changes apply cleanly on moonlight-qt upstream commit `3178022`.

Observed by the author on Windows: the GitHub Actions build completes, the application starts, the
icons show in the right places and the Tailscale page opens inside Moonlight.

**Not verified** (to be tested):
- the real Tailscale connection from the application (authorization, exit node list);
- real traffic routing through an exit node with `tsnet` (UDP in particular);
- a complete stream through Tailscale, and firewall/ACL behavior;
- terminal attachment, the log file and the dialog boxes of `Nodelight.exe` in every case.

If a step fails, open an issue with the error message (and `Nodelight.log`): the most likely causes
are a `tsnet` API mismatch (to fix in `bridge/cmd/nodelight/tunnel_ts.go`) or a port already in use on
the loopback address (a local Sunshine, for example).

## License

GPL-3.0 (see `LICENSE`): the code is derived from moonlight-qt. Detailed attributions in
`licenses/NOTICE.md`.
