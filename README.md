# 🐧 open-mwb

> **A Linux client for Microsoft's Mouse Without Borders protocol.**

Mouse Without Borders is a fantastic tool that lets you share a single keyboard and mouse across multiple PCs seamlessly. Unfortunately, it's Windows-only. This project reverse-engineers the MWB wire protocol and implements a Linux peer, letting your Linux machine join the same cross-machine keyboard/mouse/clipboard mesh as your Windows machines.

---

## Features

- **Bidirectional connection** — acts as both client and server, mirroring how the Windows MWB peer-to-peer mesh works
- **Mouse & keyboard forwarding** — move your mouse off the edge of the screen to seamlessly control your Linux machine from Windows
- **Clipboard sync** — copy on Windows, paste on Linux (and vice versa)
- **Absolute coordinate protocol** — uses the same resolution-independent 0–65535 coordinate system as the official client
- **TUI debug mode** — a built-in terminal UI that lets you test the connection without needing real input devices (great for VMs, containers, or SSH sessions)
- **Cross-compilable** — builds natively for Linux, and cross-compiles to `mwb.exe` for Windows TUI testing

---

## How It Works

The MWB protocol is a lightweight, encrypted TCP protocol:

1. Both peers connect to each other on port **15101** (client→server) and listen on **15100** (server)
2. A **CBC-encrypted handshake** derived from a shared security key is exchanged
3. After the handshake, mouse events (absolute 0–65535 coordinates), keyboard events, and clipboard payloads flow over the same persistent connection
4. **Heartbeats** keep the connection alive and prevent the Windows client from timing out

---

## Getting Started

### Prerequisites

- Go 1.21+
- Linux with `/dev/uinput` support (for injecting input events)
- Mouse Without Borders installed and running on another machine (Windows or Linux)

### Build

```bash
git clone https://github.com/youruser/open-mwb.git
cd open-mwb

# Linux binary
go build -o mwb-linux .

# Windows binary (for TUI testing)
GOOS=windows go build -o mwb.exe .
```

### Configure

Create a `config.json` next to the binary (or in the working directory):

```json
{
  "key":    "YourSharedSecurityKey",
  "remote": "[REMOTE_PC_IP_ADDRESS]",
  "port":   15100,
  "id":     2,
  "name":   "[THIS_MACHINE_NAME]",
  "edge":   "right",
  "mode":   "client"
}
```

| Field    | Description                                                         |
|----------|---------------------------------------------------------------------|
| `key`    | The security key set in Mouse Without Borders Settings on Windows   |
| `remote` | IP address of your Windows machine                                  |
| `port`   | MWB listen port (default `15100`)                                   |
| `id`     | A unique integer ID for **this** machine (pick any non-zero value)  |
| `name`   | This machine's hostname (auto-detected from `hostname` if omitted)  |
| `edge`   | Which screen edge is adjacent to Windows: `left`, `right`, `top`, `bottom` |
| `mode`   | `client` (full input forwarding) or `tui` (debug terminal UI)       |

### Run

```bash
# Run as root (required for /dev/uinput and raw input device access)
sudo ./mwb-linux
```

---

## TUI Debug Mode

Set `"mode": "tui"` in your config to launch the built-in terminal debugger. This lets you verify the connection and test mouse/clipboard without needing physical input devices.

```
 ⬤ LOCAL  MWB Debug TUI  |  Edge: right  |  Cursor: (30,10)
 ┌────────────────────────────────────────────────────────────┐
 │                                                            │
 │                              █                             │
 │                                                       ·····│
 └────────────────────────────────────────────────────────────┘
 LOCAL - use arrows to move, hit edge to switch
 [arrows]=move  [space]=return to local  [x]=click  [c]=clipboard  [q]=quit
```

### TUI Key Bindings

| Key        | Action                                                    |
|------------|-----------------------------------------------------------|
| `←↑↓→`     | Move the virtual cursor                                   |
| *(hit edge)*| Switch to REMOTE mode — arrows now control Windows cursor |
| `space`    | Return to LOCAL mode                                      |
| `x`        | Send a left click at the current remote cursor position   |
| `c`        | Send the current timestamp to Windows clipboard (for testing) |
| `q` / `Ctrl+C` | Quit                                               |

---

## Architecture

```
open-mwb/
├── main.go          # Entry point; wires up all modules
├── protocol/        # Packet marshalling / unmarshalling (mirrors MWB's DATA struct)
├── network/         # TCP client & server with CBC encryption and handshake
├── crypto/          # MWB's custom stream cipher derived from the security key
├── input/           # evdev capture (Linux) and uinput injection; Windows stubs
├── tui/             # Terminal UI for debug / testing
├── clipboard/       # Wayland (wl-copy/wl-paste) clipboard watcher
└── config/          # Config file + CLI flag handling
```

---

## Windows Configuration Tips

1. **Open Mouse Without Borders Settings** and note the **Security Key** — paste it into your `config.json`.
2. Under **Name-to-IP Mapping**, add an entry mapping your **Linux hostname** to its LAN IP. This prevents MWB from trying to reverse-DNS resolve Linux's hostname when initiating the return connection.
3. Make sure **TCP port 15101** is allowed through your firewall on both machines.

---

## Status & Roadmap

This is a proof-of-concept / work-in-progress. It currently achieves a stable bidirectional connection with a green status indicator in the Windows MWB client.

| Feature | Status |
|---------|--------|
| TCP connect + encrypted handshake | ✅ Working |
| Mouse forwarding (Linux → Windows) | ✅ Working |
| Keyboard forwarding (Linux → Windows)| ✅ Working |
| Clipboard sync (bidirectional) | ✅ Working |
| Mouse forwarding (Windows → Linux) | ✅ Working (via uinput) |
| Green status on Windows client | ✅ Working |
| TUI debug mode | ✅ Working |
| Multi-machine mesh (3+ PCs) | 🔧 Not yet |
| Drag & Drop | 🔧 Not yet |
| Wayland native input capture | 🔧 Investigating |

---

## Contributing

Pull requests are very welcome. If you're testing against a specific Windows MWB version, please mention it in the PR — the protocol may have some per-version differences, this is tested against MouseWithoutBorders version 1.1.

---

## Disclaimer

This project is not affiliated with Microsoft. It was created by reverse-engineering the network protocol of Mouse Without Borders for interoperability purposes.

---

## License

MIT
