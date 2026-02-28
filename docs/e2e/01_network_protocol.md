# E2E Test Category: Network & Protocol

## Scope
Validates the binary protocol, packet marshaling/unmarshaling, cryptography (AES CBC), and connection lifecycle (handshakes, heartbeats, timeouts). This ensures parity with `TcpServer.cs` and `SocketStuff.cs` from the original C# application.

## Implementation Details

### Setup & Mocks
*   Tests will use `NewConnectedPair` to spin up in-memory loopback servers and clients.
*   Wait timeouts are configured using `time.After`.

### Tests

1.  **TestProtocol_Packet_InvalidChecksum**
    *   **Goal:** Ensure corrupted packets are dropped and do not crash the unmarshaler.
    *   **Logic:** Connect two peers, manually craft a byte array mimicking a packet but intentionally flip a bit in the AES payload before decrypting, and write it to the TCP connection. Assert the connection survives but drops the invalid frame.

2.  **TestProtocol_Packet_UnknownType**
    *   **Goal:** Ensure unsupported packet types do not panic the unmarshaler.
    *   **Logic:** Send a packet with a `Type` outside the `protocol.PacketType` enum. Assert the receiver ignores it.

3.  **TestProtocol_Packet_Fragmentation**
    *   **Goal:** TCP is a stream; multiple small packets can arrive at once, or a large packet can be split across multiple reads. Ensure the 100-byte structure reads correctly.
    *   **Logic:** Write exactly 50 bytes of a packet, pause, then write the remaining 50 bytes. Assert successful unmarshaling. Send 2.5 packets in one write, then the rest.

4.  **TestNetwork_Handshake_DuplicateName**
    *   **Goal:** Ensure server handles connections from clients with the same machine name gracefully.
    *   **Logic:** Start a server. Connect Client A as "HostA". Then connect Client B as "HostA". Depending on original logic, either reject B or disconnect A.

5.  **TestNetwork_Heartbeat_Timeout**
    *   **Goal:** Ensure clients disconnect gracefully if heartbeats are missed.
    *   **Logic:** Connect peers. Mock the server's time or sleep the client to exceed the heartbeat timeout threshold. Assert the connection is torn down.

6.  **TestNetwork_Reconnect_After_Drop**
    *   **Goal:** Simulates a dropped connection and asserts the client tries to reconnect and succeeds.
    *   **Logic:** Establish a connection. Abruptly close the server socket. Restart the server. Assert the client eventually reconnects.
