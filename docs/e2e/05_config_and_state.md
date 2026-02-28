# E2E Test Category: Configuration & State

## Scope
Validates loading, saving, and applying configuration changes at runtime. Ensures parity with `Setting.cs`, `MachinePool.cs`, and `appsettings.json` parsing.

## Implementation Details

### Setup & Mocks
*   Use a temporary directory to create and manipulate a `config.json` file.

### Tests

1.  **TestConfig_Load_Default**
    *   **Goal:** Ensure starting with no config creates a default secure config.
    *   **Logic:** Initialize the config manager in an empty dir. Assert a new key is generated, local machine name is set, and a default 1x1 matrix is created.

2.  **TestConfig_Update_Matrix_Runtime**
    *   **Goal:** Changing the matrix layout updates the active routing.
    *   **Logic:** Start with a 1x1 matrix. Programmatically update the config to a 1x2 matrix with a new peer. Assert the matrix manager reloads and allows transitioning to the new peer.

3.  **TestConfig_Machine_Pool_Management**
    *   **Goal:** Validates adding/removing machines to the trusted pool.
    *   **Logic:** Add a machine to the pool via config update. Assert that a connection from that machine's IP/ID is now accepted (if previously rejected). Remove it, and assert the connection drops or is rejected.
