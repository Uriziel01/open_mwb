# E2E Test Category: Input Simulation & Hooking

## Scope
Validates the translation of MouseWithoutBorders network packets to OS-level inputs (Linux `uinput`/`evdev`). Ensures parity with `InputHook.cs`, `InputSimulation.cs`, `MOUSEDATA.cs`, and `KEYBDDATA.cs`.

## Implementation Details

### Setup & Mocks
*   Tests require a mock input interface since running real `uinput` in CI requires root. We will define an interface for input emissions and inject a mock for testing.

### Tests

1.  **TestInput_Keyboard_Keymap_Translation**
    *   **Goal:** Assert Windows VK -> Linux EV_KEY mappings.
    *   **Logic:** Send standard C# VK packets (e.g., `VK_SPACE`, `VK_LSHIFT`, `VK_RETURN`) over the mock network and intercept the corresponding Linux `EV_KEY` emissions on the mock `uinput` interface.

2.  **TestInput_Keyboard_Modifiers_Sync**
    *   **Goal:** Assert state of Shift/Ctrl/Alt is held and released correctly.
    *   **Logic:** Send `WM_KEYDOWN` for `VK_LSHIFT`, followed by `WM_KEYDOWN` for `VK_A`. Assert the mock receives the shift modifier state. Send `WM_KEYUP` for `VK_LSHIFT` and assert the release.

3.  **TestInput_Keyboard_Extended_Keys**
    *   **Goal:** Validate media keys, function keys, and numpad keys.
    *   **Logic:** Send `VK_MEDIA_PLAY_PAUSE`, `VK_F12`, `VK_NUMPAD5`. Assert correct translation.

4.  **TestInput_Mouse_Relative_Deltas**
    *   **Goal:** Assert standard X/Y deltas.
    *   **Logic:** Send `protocol.Mouse` packets with `MOUSEEVENTF_MOVE` and `X=10, Y=-5`. Assert the mock emits relative EV_REL events matching these values.

5.  **TestInput_Mouse_Absolute_Bounds**
    *   **Goal:** Assert absolute coordinates are mapped accurately to the mock display size.
    *   **Logic:** Send `protocol.Mouse` packets with `MOUSEEVENTF_ABSOLUTE`. Ensure the coordinate scaling algorithm translates `0-65535` range (Windows standard) to the configured display resolution (e.g., `1920x1080`).

6.  **TestInput_Mouse_Wheel_Scroll**
    *   **Goal:** Assert vertical and horizontal scrolling.
    *   **Logic:** Send `MOUSEEVENTF_WHEEL` and `MOUSEEVENTF_HWHEEL` with standard WHEEL_DELTA (120). Assert mock emits `REL_WHEEL` and `REL_HWHEEL`.

7.  **TestInput_Suppression_State**
    *   **Goal:** When the local machine controls a remote machine, local inputs should be suppressed (captured) but not simulated locally.
    *   **Logic:** Enter "remote control" mode. Inject a local keyboard event via `evdev` mock. Assert it is swallowed, sent over the network, and NOT emitted locally.
