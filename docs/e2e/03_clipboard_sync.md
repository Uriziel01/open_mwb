# E2E Test Category: Clipboard Sync & Drag-Drop

## Scope
Validates syncing of text, images, and files across the network. Ensures parity with `Clipboard.cs` and `DragDrop.cs` from the original C# application.

## Implementation Details

### Setup & Mocks
*   Mock the OS clipboard interface (Wayland/X11 wrappers) to assert that receiving a complete sequence successfully updates the system clipboard, without affecting the developer's actual clipboard during tests.

### Tests

1.  **TestClipboard_Text_Small**
    *   **Goal:** Validate standard small text synchronization.
    *   **Logic:** Send a `ClipboardText` packet (<64 bytes). Assert the OS clipboard mock is updated with the received text.

2.  **TestClipboard_Text_Multipart**
    *   **Goal:** Send text exceeding 64 bytes and verify reconstruction.
    *   **Logic:** Simulate a chunked transmission protocol for a 10KB text file. Send the initialization packet followed by chunks. Assert the reassembled buffer matches the source and the clipboard mock is updated.

3.  **TestClipboard_Image_Sync**
    *   **Goal:** Validate image transfer over clipboard.
    *   **Logic:** Simulate the transfer of a known bitmap/PNG format via the clipboard multipart protocol. Assert the reconstructed byte slice matches the original image hash and sets the correct MIME type on the clipboard mock.

4.  **TestClipboard_File_DragDrop_Protocol**
    *   **Goal:** Mock a file transfer sequence.
    *   **Logic:** Simulate the C# drag-and-drop sequence. Send a file metadata packet (filename, size). Then send a stream of binary chunks. Assert the file is written correctly to a temporary directory on the receiving side with the correct name and contents.

5.  **TestClipboard_Format_Conversion**
    *   **Goal:** Ensure Windows specific clipboard formats translate to Linux equivalents.
    *   **Logic:** Send a clipboard message explicitly flagged as Windows `CF_UNICODETEXT`. Assert it is decoded as UTF-16 (if applicable) and pushed to the Linux clipboard as `text/plain;charset=utf-8`.
