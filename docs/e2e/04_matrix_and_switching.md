# E2E Test Category: Matrix & Switching

## Scope
Validates the KVM "switch" logic when the cursor hits screen edges. Ensures parity with `MachineMatrix.cs`, `MouseLocation.cs`, and the layout UI logic.

## Implementation Details

### Setup & Mocks
*   Instantiate a virtual matrix manager with mocked display dimensions (e.g., two 1920x1080 screens side-by-side).
*   Need the ability to trigger "mouse move" events directly into the matrix evaluator.

### Tests

1.  **TestMatrix_Edge_Transition_Right**
    *   **Goal:** Simulate mouse hitting the right screen edge and verify target machine switch.
    *   **Logic:** Setup a 1x2 matrix (Local -> Remote). Emulate local mouse moving to X=1919 (edge). Emulate one more move right (+1). Assert the matrix manager transitions state to `Remote`, emits a `HideMouse` packet to the remote, and subsequent inputs are routed to `Remote`.

2.  **TestMatrix_Edge_Transition_Left**
    *   **Goal:** Simulate transitioning back from Remote to Local.
    *   **Logic:** Starting in `Remote` state, emulate mouse moving left until it crosses 0. Assert state transitions to `Local`.

3.  **TestMatrix_Layout_2x2**
    *   **Goal:** Validate complex layouts.
    *   **Logic:** Setup a 2x2 grid. Test moving Top, Bottom, Left, and Right from various quadrants to ensure correct machine resolution.

4.  **TestMatrix_Dead_Corners**
    *   **Goal:** Prevent accidental switching at screen corners (a feature in MWB).
    *   **Logic:** Emulate mouse moving to X=1919, Y=0 (top right corner). Push right (+1). Assert NO switch occurs if dead corners are enabled in config.

5.  **TestMatrix_Wrap_Around**
    *   **Goal:** Moving off the far right edge wraps to the far left edge.
    *   **Logic:** Setup 1x2 matrix. Enable wrap-around. Move right off the rightmost machine. Assert switch to the leftmost machine.

6.  **TestMatrix_Disconnection_Reverts_Focus**
    *   **Goal:** If focused on a remote machine and the connection drops, ensure control reverts to the local machine.
    *   **Logic:** Transition to `Remote` state. Abruptly close the `ConnectedPair` server side. Assert the matrix manager receives the disconnect event and forces the state back to `Local`, restoring the local mouse cursor.
