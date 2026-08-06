# Agentagotchi Native Edge Administration

Optional macOS 14+ SwiftUI administration client for the local Edge. The
`AdminClient` library is a pure Foundation/POSIX implementation of
`agentagotchi.admin.v1`; the app is a thin renderer over that client.

## Build and run

```sh
cd clients/macos-admin
swift build
swift test
swift run AgentagotchiAdmin
```

The package can also be opened as a Swift Package in Xcode 15 or newer. The
app uses the same default socket as the Edge CLI:

```text
~/Library/Application Support/Agentagotchi/edge.sock
```

Library users can provide another path with
`UnixSocketTransport(socketPath:)`. Each request opens the configured Unix
socket, writes one JSON object followed by `\n`, and reads one JSON reply.

## Privacy

- Administration stays on the local owner-only Unix socket. The Edge owns the
  authorization and domain rules; this app does not bypass them.
- The dashboard displays only role, version, timestamps, connectivity, pairing
  counts, Task Presence counts, and aggregate state.
- Credential lists are always rendered as redacted. The app never persists or
  logs tokens, prompts, transcripts, commands, tool payloads, filesystem
  metadata, private keys, or Wi-Fi secrets.
- Pairing codes are short-lived and held in memory only. After approval, the
  current Edge contract marks the code approved; the connecting client redeems
  it for its role-scoped credential. The app presents the retained one-use code
  once with a copy button so it can be handed to that client, then discards it.
- If the local Edge is unavailable, the UI reports **Edge not running** rather
  than exposing socket or filesystem details.
