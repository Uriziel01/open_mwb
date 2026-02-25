# Mouse Without Borders (MWB) Architecture & Protocol Analysis

This document outlines the internal workings of the Mouse Without Borders (MWB) module, specifically tailored to serve as a reference for developing a cross-platform Linux equivalent.

## 1. Overview and Architecture
- MWB operates as a software KVM (Keyboard, Video, Mouse) switch.
- The core logic is implemented in C#, located in the `src/modules/MouseWithoutBorders/App/Core` and `App/Class` directories inside the PowerToys repository.
- The architecture is peer-to-peer over a TCP connection where machines can act as both Server (`TcpServer.cs`) and Client (`SocketStuff.cs`), allowing symmetrical control.

## 2. Network Protocol
The network protocol is designed for low latency and utilizes fixed-size binary packets for most operations.

- **Transport**: TCP sockets.
- **Port**: The default base listening port is `15100` (and `15101` as a fallback).
- **Encryption** (`Encryption.cs`):
  - Secures the TCP stream using **AES-256** in CBC mode.
  - The encryption key is derived using PBKDF2 (`Rfc2898DeriveBytes`) with **SHA512**, **50,000 iterations**. 
  - It uses the user-configured security key as the password and a fixed `InitialIV` string representation of `ulong.MaxValue` as the salt.
  - The IV for the symmetric algorithm is generated from the first bytes of that same `InitialIV`.
- **Packet Format** (`DATA.cs`, `Package.cs`):
  - Packets are fixed-size: 32 bytes (`PACKAGE_SIZE`) or 64 bytes (`PACKAGE_SIZE_EX` for big packets).
  - A packet is a C-style union structure mapped to a byte array using `[StructLayout(LayoutKind.Explicit)]`.
  - **Header Layout**:
    - `Type` (1 byte): The `PackageType` enum value.
    - `Id` (4 bytes): Sequential packet ID.
    - `Src` (4 bytes): Source Machine ID.
    - `Des` (4 bytes): Destination Machine ID.
    - `DateTime` (8 bytes): Tick count/Timestamp of the event.
  - **Payload Layout**:
    - The remaining bytes are an overlapping union containing either `Kd` (Keyboard Data), `Md` (Mouse Data), `ClipboardPostAction`, or an array of `Machine` IDs depending on the `PackageType`.
- **Message Types** (`PackageType.cs`):
  - **Connection/Heartbeat**: `Heartbeat` (20), `Awake` (21), `Handshake` (126), `HandshakeAck` (127).
  - **Input Events**: `Keyboard` (122), `Mouse` (123).
  - **Clipboard**: `Clipboard` (69), `ClipboardText` (124), `ClipboardImage` (125), `ClipboardAsk` (78), `ClipboardPush` (79).
  - **Topology**: `Matrix` (128), `NextMachine` (121).

## 3. Input Capturing (Hooks) - `InputHook.cs`
To intercept local input from being dispatched to the active machine, MWB uses Windows Hooks:
- **APIs**: `SetWindowsHookEx` with `WH_MOUSE_LL` (14) and `WH_KEYBOARD_LL` (13).
- **Process**:
  - The low-level hooks monitor all mouse movements, button clicks, and keyboard strokes globally.
  - When the user transitions the mouse cursor across the screen edge into another machine's screen bounds, MWB captures the inputs.
  - It suppresses the event from reaching the local OS by returning a non-zero value (`1`) from the hook callback.
  - Captures are packed into `KEYBDDATA` or `MOUSEDATA` structs inside a `DATA` payload, and sent over the TCP socket as a `Keyboard` or `Mouse` package type.
- **Linux Port Implication**: On Linux, achieving this requires hooking into `libinput`, reading directly from `/dev/input/event*` using `evdev`, or using X11/Wayland specific display server hooks (e.g., `XQueryPointer` and `XGrabKeyboard` for X11, or `wlr-virtual-pointer-unstable-v1` for Wayland).

## 4. Input Simulation - `InputSimulation.cs`
When a remote machine receives input packets over TCP, MWB simulates those events on the target machine:
- **APIs**: It relies heavily on the `SendInput` function from `user32.dll`.
- **Process**:
  - The `MOUSEDATA` or `KEYBDDATA` from the TCP package is translated back into the native Windows `INPUT` structure (e.g. `MOUSEINPUT` and `KEYBDINPUT`).
  - Absolute mouse positioning is tracked across the virtual matrix using `Md.X` and `Md.Y` (scaled from 0 to 65535 natively by Windows). Relative delta shifts are also supported for edge-cases.
- **Linux Port Implication**: On Linux, `uinput` kernel module is normally used to create virtual keyboard and mouse devices to inject these input events seamlessly at the kernel level, effectively bypassing Wayland/X11 restrictions for input simulation.

## 5. Clipboard Sharing - `Clipboard.cs` & `IClipboardHelper.cs`
Clipboard synchronization allows copying text, images, and files between machines:
- **Detection**: MWB detects local clipboard changes by listening to `WM_CLIPBOARDUPDATE` via the modern `AddClipboardFormatListener` API, or gracefully falls back to the legacy `SetClipboardViewer` hook chain.
- **Transmission**: 
  - Upon a change, the format is identified (Text, Image, FileDropList).
  - A notification (`ClipboardPush` or `Clipboard`) is sent via TCP.
  - Large payloads (like Images or dragging files) trigger dedicated TCP file transfer routines (`SendClipboardDataUsingTCP` and `ReceiveClipboardDataUsingTCP` in `Clipboard.cs`).
- **Injection**: The receiving machine asks for the data if needed (`ClipboardAsk`) and injects it into the local clipboard using WinForms standard APIs (`System.Windows.Forms.Clipboard.SetText`, `SetImage`).
- **Linux Port Implication**: Clipboard APIs on Linux are highly fragmented between X11 (`xclip`/`xsel` or direct Xlib API) and Wayland (e.g., `wl-clipboard` or manipulating Wayland data offers). File drops are usually conveyed as `text/uri-list`.

## 6. Byte-Level Payload Structures
To guarantee protocol compatibility with the Windows version, a Linux port must precisely implement the serialized payload structures and cryptography implementation.

### C# Payload Struct (DATA)
The primary packet (`DATA` union) is either 32 or 64 bytes (`PACKAGE_SIZE` or `PACKAGE_SIZE_EX`) and is mapped to a byte array starting with this 24-byte header:
- **Offset `0`**: `Type` (4 bytes, 32-bit integer `PackageType` enum)
- **Offset `4`**: `Id` (4 bytes, 32-bit integer)
- **Offset `8`**: `Src` (4 bytes, 32-bit unsigned integer `ID`)
- **Offset `12`**: `Des` (4 bytes, 32-bit unsigned integer `ID`)
- **Offset `16`**: `DateTime` (8 bytes, 64-bit long integer, tick count)

The payload begins at **Offset `24`** as a shared memory union over the remaining bytes:
- **`KEYBDDATA` (8 bytes)**: 
  - `wVk` (4 bytes, 32-bit integer) -> Virtual Key Code
  - `dwFlags` (4 bytes, 32-bit int) -> Windows KEYEVENTF constants (e.g., KeyUp, ExtendedKey)
- **`MOUSEDATA` (16 bytes)**: 
  - `X` (4 bytes, 32-bit int) -> Mouse X absolute or relative delta
  - `Y` (4 bytes, 32-bit int) -> Mouse Y absolute or relative delta
  - `WheelDelta` (4 bytes, 32-bit int) -> Scroll delta (often 120 or -120)
  - `dwFlags` (4 bytes, 32-bit int) -> Windows MOUSEEVENTF constants (e.g., Absolute, Move, LeftDown, RightUp)

### Cryptography (Encryption.cs)
- **Symmetric Algorithm**: AES-256 in CBC mode (Cipher Block Chaining).
- **Block Size**: 128 bits (16 bytes).
- **Key Derivation (PBKDF2)**: 
  - Hash: HMAC-SHA512
  - Password: The user-defined security key.
  - Salt: The `InitialIV` string which is hardcoded as the string representation of `ulong.MaxValue` (`"18446744073709551615"`).
  - Iterations: `50,000`.
  - Output Size: 32 bytes (256-bit key).
- **IV Derivation**: 
  - The IV (16 bytes) is generated by copying the first 16 characters/bytes of the `InitialIV` string padding with spaces if needed.

## 7. Machine Discovery and Handshake
Unlike some zero-conf protocols, Mouse Without Borders does **not** rely on UDP broadcast or mDNS for automatic discovery.

- **Discovery Strategy (`MachinePool.cs`, `SocketStuff.cs`)**:
  - The tool maintains a user-defined list of machine names or IP addresses.
  - It attempts to resolve these names to IPv4 addresses using standard DNS (`Dns.GetHostEntry`).
  - Active connections are attempted periodically to Port `15100` (`bASE_PORT` / `TcpPort`) on resolved IPs.

- **Handshake Sequence (`MainTCPRoutine`)**:
  1. Once a TCP socket connects, the initiator crafts a `DATA` packet of type `Handshake` (126).
  2. To circumvent potential network packet loss during initialization, it sends this exact same `Handshake` packet **10 times** consecutively.
  3. The receiver accepts the `Handshake` packet, changes the type to `HandshakeAck` (127), sets the `Src` field to `ID.NONE` (0), and importantly applies a bitwise NOT (`~`) to the payload fields `Machine1`, `Machine2`, `Machine3`, and `Machine4` as a rudimentary cryptographic proof in addition to AES.
  4. The receiver sends the packet back.
  5. The initiator receives the `HandshakeAck`, verifies the bitwise inversion of the Machine ID fields, and transitions the socket to the `Connected` state, trusting the connection.

## 8. Screen Matrix and Coordinate Mapping
To understand *when* the mouse should transition to another machine, MWB uses a conceptual grid.

- **Matrix Definition (`MachineStuff.cs`)**:
  - The topology is defined by an array of EXACTLY 4 strings (`MachineMatrix`), representing either a `1x4` horizontal row or a `2x2` grid.
  - Each machine occupies a slot (index 0 to 3) storing its resolved Name.
  - The system assigns each machine a numerical 32-bit `ID` (1 to 4) corresponding to its matrix index `+ 1`.
- **Edge Transitioning**:
  - MWB locally monitors absolute mouse bounds. When the cursor hits the right edge (e.g. `primaryScreenBounds.Right`), `MachineStuff.MoveRight()` looks at the current machine's index in the `MachineMatrix` array and finds the Name/ID of the machine conceptually to the right.
  - The target machine is set as `desMachineID` and local input suppression begins.
- **Coordinate Scaling**:
  - To support variable resolutions and multi-monitor setups gracefully without complex coordinate sync, Windows absolute mouse positioning relies on values between `0` and `65535` for the entire virtual desktop bounds.
  - When switching bounds, the entrance coordinate (e.g. the Y coordinate when moving Right) is mathematically scaled to this 0-65535 universal value and sent over the wire, allowing `SendInput` on the remote end to place the cursor on its own edge preserving relative vertical/horizontal alignment without needing to know the exact pixel resolution of the adjacent screen.
