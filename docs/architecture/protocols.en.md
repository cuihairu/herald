> English · [中文](./protocols.md)

# Protocol Design

## External Protocol

### HTTP REST + JSON

This is the **only public protocol**. curl, shell scripts, CI/CD, monitoring-system webhooks, and any language all call the REST interface directly, with no extra client convention required.

## About the Business SDKs

The wire protocol has always been REST only: the notify API can be called with a single POST,

```bash
curl /notify
```

which is enough for any language. On top of that, the repo landed two **convenience-layer** Go SDKs with batch 11 (without changing the wire protocol): `apps-sdk/go` (integrator onboarding through the app namespace) and `worker-sdk/go` (for the remote Worker side).

## Internal Protocol

Internally Herald uses the **Persistent Session Protocol**, carrying six kinds of interaction: Worker registration, registration acknowledgement, heartbeat, reconnection, error notification, and event stream. The `dispatch` type is defined in the protocol (`protocol/message.go`) but has no production call site anywhere in the repo — task distribution goes through the queue, and the Hub does not take part.

### Actual Implementation

The wire format running in production is **WebSocket + JSON**. Transport uses `gorilla/websocket` (a go.mod dependency); the message body is encoded by `protocol.MarshalMessage`, and `Message{Type, Payload}` is serialized and then injected with the type field. The table below was compared at selection time; both the protobuf and Raw TCP options were never implemented:

| Protocol | Selection outcome | Implementation status |
| ------------------------ | --- | --- |
| WebSocket + JSON | adopted | implemented (`core/websocket` + `protocol/message.go`) |
| WebSocket + protobuf | alternative | not implemented |
| Raw TCP + protobuf | alternative | not implemented |
| gRPC | not adopted | — |

### Why gRPC Is Not Used

- Huge C++ footprint
- Complex builds on Windows
- Exploding debug PDBs
- Unsuitable for Runtime Node
- Unsuitable for event streams

Herald is more of a **Session System** than an **RPC Framework**.
