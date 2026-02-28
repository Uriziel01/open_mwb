# VM Integration Test Plan: AutoIt Network Control Server

## Overview
To achieve 100% protocol compatibility and end-to-end testing without the limitations of purely mocked environments, we will use a Windows 11 VM running the original C# MouseWithoutBorders application alongside a custom AutoIt Network Control Server.

The AutoIt Server listens on `192.168.1.97:15102` and allows us to programmatically query and manipulate the Windows VM's state (Mouse, Keyboard, Clipboard). By cross-referencing `open-mwb` network packets with the actual VM state, we can validate both directions of the KVM implementation.

## Architecture

*   **Linux Host (`open-mwb`)**: Runs the Go test suite.
*   **Windows VM (`192.168.1.97`)**: Runs original C# MWB + AutoIt TCP Server (`Port 15102`).

The tests will act as a client to both the MWB protocol port and the AutoIt server port.

## Test Strategies

### 1. Linux -> Windows (Testing open-mwb as Client/Sender)

When the Linux machine is controlling the Windows VM, `open-mwb` will generate and send input packets. We will use the AutoIt server to verify the Windows OS actually processed them correctly.

*   **Keyboard Injection**:
    *   **Action**: `open-mwb` constructs and sends `protocol.Keyboard` packets (e.g., typing "Hello").
    *   **Validation**: Connect to AutoIt server, send `getInputContent`. Assert the response is "Hello".
*   **Mouse Movement**:
    *   **Action**: `open-mwb` sends `protocol.Mouse` packets with absolute or relative coordinates.
    *   **Validation**: Connect to AutoIt server, send `getMousePos`. Assert the response coordinates match the expected math.
*   **Clipboard Transfer (Linux -> Win)**:
    *   **Action**: `open-mwb` sends `protocol.ClipboardText` or multipart clipboard packets.
    *   **Validation**: Connect to AutoIt server, send `getClipboardContent`. Assert the response matches the payload sent.

### 2. Windows -> Linux (Testing open-mwb as Server/Receiver)

When the Windows machine is controlling the Linux host, the C# MWB app captures Windows inputs and sends them over the network. We will trigger these inputs via the AutoIt server and assert that `open-mwb` receives and unmarshals them perfectly.

*   **Remote Keyboard Capture**:
    *   **Action**: Ensure MWB matrix logic is focused on the Linux machine. Connect to AutoIt server and send `pressKey:{A}`.
    *   **Validation**: The `open-mwb` test suite listens on the MWB socket. Assert it receives a `protocol.Keyboard` packet mapping to the 'A' key (`VK_A`).
*   **Remote Mouse Capture**:
    *   **Action**: Connect to AutoIt server and send `moveMouse:500,500`.
    *   **Validation**: Assert `open-mwb` receives a `protocol.Mouse` packet with the expected deltas or absolute coordinates.
*   **Clipboard Transfer (Win -> Linux)**:
    *   **Action**: Connect to AutoIt server and send `setClipboard:TestStringFromWindows`.
    *   **Validation**: Assert `open-mwb` receives a clipboard packet containing "TestStringFromWindows".

### 3. KVM Matrix & Edge Detection Integration

*   **Action**: Send `moveMouse:9999,500` via AutoIt to slam the Windows cursor against the right edge of its screen.
*   **Validation**: Assert `open-mwb` receives the `Matrix` / `HideMouse` packets indicating the Windows machine is attempting to pass control to the Linux machine.

## Implementation Steps for the Test Suite

1.  **AutoIt Client Wrapper**: Create an internal Go package (e.g., `e2e/autoit`) that handles the TCP string-based protocol (connecting to `192.168.1.97:15102`, sending the command, reading the response until socket close).
2.  **Integration Test Flag**: Since these tests require the live VM, they should be guarded by a build tag (e.g., `//go:build integration`) or an environment variable check (e.g., `if os.Getenv("TEST_VM_IP") == "" { t.Skip() }`).
3.  **Refactoring existing E2E**: Port the skipped tests (`clipboard_sync_test.go`, `matrix_and_switching_test.go`) to use this new `autoit` wrapper to do live validations instead of mocks.