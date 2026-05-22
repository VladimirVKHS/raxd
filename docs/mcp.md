# MCP integration guide

This guide describes the **MCP (Model Context Protocol) server** that `raxd` exposes today, and how
to connect an MCP client to it. Everything here is taken from the current code; nothing is
hypothetical.

> Where to run this: per the security baseline, `raxd` is built and run **inside Docker only**. This
> applies to `raxd serve`, which is what hosts the MCP endpoint. See
> [`development.md`](development.md) for the container workflow and
> [`commands.md`](commands.md#raxd-serve) for the `serve` command itself.

## What the raxd MCP server is

The MCP server is **not** a separate process or port. It is mounted **inside the same `raxd serve`
process**, on the route **`/mcp`**, behind the **same** TLS 1.3 transport, the same API-key
authentication, the same `Host`/`Origin` checks, the same rate limiting, and the same audit stream as
the rest of the server. There is one listener, one port, one certificate, one set of keys.

| Property | Value |
|----------|-------|
| Hosted by | `raxd serve` (same process, same port) |
| Route | `/mcp` |
| Transport | Streamable HTTP over TLS 1.3 |
| MCP protocol version | `2025-11-25` |
| SDK | official Go MCP SDK (`github.com/modelcontextprotocol/go-sdk/mcp`) |
| Session mode | stateless — no `MCP-Session-Id` is issued, no server→client SSE |
| Tools | `ping`, `server_info` (read-only) |
| Authentication | inherited from the transport (`Authorization: Bearer rax_live_…`) |

Before this was implemented, `/mcp` returned `501 Not Implemented` like every other non-health route.
That is **no longer true**: a POST to `/mcp` with a valid key now gets a real JSON-RPC response. The
catch-all `501` still applies to *other* unimplemented routes (for example `/exec`), but **not** to
`/mcp`.

The endpoint is **stateless**. The server does not issue an `MCP-Session-Id` and does not open a
server→client SSE stream. A `GET /mcp` (the way a client would try to open such a stream) returns
`405 Method Not Allowed`. All real work happens over `POST`.

## Connection parameters

These are the parameters an MCP client needs. They are the heart of this guide.

| Parameter | Value |
|-----------|-------|
| URL | `https://127.0.0.1:<port>/mcp` |
| Port (`<port>`) | from `config.yaml` (`port:` key); **default `7822`** |
| Method | `POST` (request/response); `GET` returns `405` |
| Auth header | `Authorization: Bearer rax_live_…` (a key from `raxd key create`) |
| `Content-Type` | `application/json` |
| `Accept` | `application/json, text/event-stream` |
| Protocol version header | `MCP-Protocol-Version: 2025-11-25` (sent by the client **after** `initialize`) |
| TLS | self-signed certificate — the client must trust it or skip verification (see below) |

- **URL.** The host and port are exactly the ones `raxd serve` prints on startup
  (`listening https://127.0.0.1:7822` by default). If you changed `port:` in `config.yaml`, use that
  port. The route is always `/mcp`.
- **The key.** Create one with `raxd key create --name <label>` (see
  [`commands.md`](commands.md#raxd-key-create)). The full `rax_live_…` body is printed **once**; copy
  it then. Send it as `Authorization: Bearer <key>`. Without a valid key the request is rejected with
  `401` **before** it reaches the MCP layer — no tool runs.
- **Accept header.** A spec-compliant MCP client sends `Accept: application/json, text/event-stream`.
  For request/response calls (`initialize`, `tools/list`, `tools/call`) the server replies with
  `application/json`.

### Self-signed TLS

`raxd serve` generates a **self-signed** ECDSA P-256 certificate (SAN: `127.0.0.1`, `localhost`) on
first run and reuses it afterward. There is no built-in trust anchor and **no mTLS** in this build. A
client that verifies certificates will reject it by default. You have two options:

1. **Trust the certificate (recommended where supported).** Add the generated `cert.pem` (in the TLS
   directory, default `~/.local/state/raxd/tls/`, shown by `raxd status` as the `tls` line) to your
   client's trust store, and connect using a name the SAN covers (`127.0.0.1` or `localhost`).
2. **Skip verification (development only, insecure).**
   - `curl`: pass `-k` (`--insecure`).
   - Node-based clients (MCP Inspector and similar): set `NODE_TLS_REJECT_UNAUTHORIZED=0` in the
     client's environment. **This disables TLS verification process-wide — use only in a controlled
     local dev setup, never in production.**

> Skipping verification removes the protection TLS gives you against a man-in-the-middle. It is
> acceptable only for a local test against your own `raxd serve`. Prefer trusting the certificate.

## Tools

`tools/list` returns **exactly two** tools. Both are read-only and take no input. There is no
`execute_command`, no `upload_file`, no file or shell access (see
[Scope and limitations](#scope-and-limitations)).

### `ping`

- **Description:** check that the MCP channel to `raxd` is alive. Returns `"pong"`. No side effects on
  the host.
- **Input:** none (an empty object; the schema is `{"type":"object"}` with no properties, so any
  unexpected argument is rejected).
- **Output:** a single text content block, `pong`. The result is not an error (`isError` is absent or
  `false`).[^iserror]

`ping` is what an agent calls to prove the full path — transport → authentication → SDK → tool — works
end to end. It performs no I/O.

### `server_info`

- **Description:** version and basic facts about the `raxd` daemon, with **no secrets**.
- **Input:** none (empty object).
- **Output:** structured content with **exactly three** fields, plus a duplicate human-readable text
  line. The result is not an error (`isError` is absent or `false`).[^iserror]

The structured result is exactly:

```json
{
  "name": "raxd",
  "version": "1.0.0",
  "protocolVersion": "2025-11-25"
}
```

and the accompanying text line is `raxd 1.0.0 (MCP 2025-11-25)`.

| Field | Value | Source |
|-------|-------|--------|
| `name` | always `"raxd"` | constant |
| `version` | the build version, e.g. `"1.0.0"`; `"dev"` on a build without ldflags | `internal/version` |
| `protocolVersion` | `"2025-11-25"` | protocol constant |

`server_info` returns **only** these three fields. It does **not** read secrets, config, or the
environment, and it never exposes API-key bodies or hashes, the private TLS key or its path, the
`keys.db`/`config.yaml` paths, the listening port, the bind address, allowlists, rate-limit settings,
environment variables, the hostname, uptime, PID, or the number of keys. (`version` is build metadata,
not a secret.)

[^iserror]: The `isError` field is serialized with `omitempty`, so for a successful tool result (where
the server does not set it) it is **absent** from the JSON. It appears, set to `true`, only when a
tool reports its own error. See [Behaviour and error handling](#behaviour-and-error-handling).

## Authentication

Authentication is **inherited from the transport** and happens **before** any MCP processing. The MCP
layer has no authentication of its own.

- The transport's auth middleware reads `Authorization: Bearer rax_live_…`, verifies it against
  `keys.db` (constant-time), and only then lets the request reach `/mcp`. The MCP layer never sees the
  key body — only a short, non-reversible fingerprint placed in the request context.
- The MCP **session** (`MCP-Session-Id`) is **not** used for authentication. In fact this build is
  stateless and issues no session id at all. Identity is the transport fingerprint, nothing else.

This means every rejection you would get on `/healthz` you also get on `/mcp`, at the same stage, with
the same HTTP status:

| Condition | HTTP status | Reaches the MCP layer? |
|-----------|-------------|------------------------|
| No `Authorization` header / not `Bearer` / empty token | `401 Unauthorized` | No |
| Unknown or revoked key | `401 Unauthorized` | No |
| Key store unreadable/corrupt at request time | `403 Forbidden` | No |
| `Host` not in the host allowlist | `403 Forbidden` | No |
| `Origin` present and not in the origin allowlist | `403 Forbidden` | No |
| Per-key or per-IP rate limit exceeded | `429 Too Many Requests` | No |

When the transport rejects a request (`401`/`403`/`429`), **no tool runs** — the request never reaches
the SDK dispatcher. The reason is recorded in the audit stream, not in the response body (rejection
bodies are empty by design). For the full transport reference, see
[`commands.md`](commands.md#raxd-serve), and the allowlist/rate-limit settings in
[`configuration.md`](configuration.md#networking-and-serve-fields).

> **`Origin` for browser-based clients.** A request with **no** `Origin` header (the normal case for
> `curl` and most SDK clients) passes the Origin check and goes on to authentication. An `Origin` that
> is **present but not** in `origin_allow` is rejected with `403`. By default `origin_allow` is
> `localhost`, `127.0.0.1`, `::1`.

## Behaviour and error handling

Once a request passes the transport gates and reaches `/mcp`, the SDK handles the JSON-RPC protocol.

| Condition | Result |
|-----------|--------|
| Valid `initialize` / `tools/list` / `tools/call` | JSON-RPC `result` |
| `notifications/initialized` (a notification, not a request) | `202 Accepted`, no body |
| `GET /mcp` (trying to open a server→client SSE stream) | `405 Method Not Allowed` (stateless v1) |
| Malformed JSON in the body | JSON-RPC error `-32700` (Parse error) |
| Valid JSON but not a valid JSON-RPC request | JSON-RPC error `-32600` (Invalid Request) |
| Unknown method | JSON-RPC error `-32601` (Method not found) |
| Unknown tool in `tools/call` (e.g. `execute_command`) / bad params | JSON-RPC error `-32601` (Method not found) or `-32602` (Invalid params), depending on the SDK version |
| A tool's own input-validation error | `isError: true` inside the `result` (a *tool* error, not a protocol error) |

Two points worth stressing:

- **An unknown tool is never executed.** `execute_command` and `upload_file` are not registered in
  this build. Calling them returns a JSON-RPC error (`-32601` or `-32602`, depending on the SDK
  version) and runs nothing — there is no shell, no command execution, no file access behind this
  server today. After such an error the server stays up, and a valid `ping` still returns `pong`.
- **`/mcp` never returns `501`.** The old `501` stub on `/mcp` is gone. Every MCP request gets either
  a correct JSON-RPC response or a correct JSON-RPC error.

## curl smoke-test

Use `curl` to verify the channel end to end. Run this from inside the container running `raxd serve`,
with `KEY` set to a key from `raxd key create` and `<port>` set to your port (default `7822`). `-k`
skips certificate verification for this local test.

`initialize`:

```sh
curl -k https://127.0.0.1:<port>/mcp \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"curl","version":"1"}}}'
```

The response advertises the `tools` capability and the server info:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "2025-11-25",
    "capabilities": { "tools": { "listChanged": false } },
    "serverInfo": { "name": "raxd", "version": "1.0.0" }
  }
}
```

`tools/call ping`:

```sh
curl -k https://127.0.0.1:<port>/mcp \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "MCP-Protocol-Version: 2025-11-25" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ping","arguments":{}}}'
```

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "content": [ { "type": "text", "text": "pong" } ]
  }
}
```

`tools/call server_info` returns the three fields plus a text line:

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "result": {
    "content": [ { "type": "text", "text": "raxd 1.0.0 (MCP 2025-11-25)" } ],
    "structuredContent": { "name": "raxd", "version": "1.0.0", "protocolVersion": "2025-11-25" }
  }
}
```

> On a successful tool result the `isError` field is **omitted** (it is serialized with `omitempty`
> and the server does not set it), which is why it does not appear in the responses above. It shows up,
> set to `true`, only when a tool reports its own error. The exact JSON shape of `capabilities` and
> `serverInfo` in `initialize` is produced by the SDK; the examples above show the expected structure.
> The version string reflects your build (`dev` on a build without ldflags).

## Connecting an MCP client

`raxd` is a **Streamable-HTTP** server, so it is configured as a **remote/HTTP** MCP server — **not**
as a stdio command. The exact configuration syntax depends on the client and its version; the shape is
the same: a `streamable-http` URL plus an `Authorization` header.

A typical client config entry:

```json
{
  "mcpServers": {
    "raxd": {
      "type": "streamable-http",
      "url": "https://127.0.0.1:7822/mcp",
      "headers": { "Authorization": "Bearer rax_live_…" }
    }
  }
}
```

Replace `7822` with your port and `rax_live_…` with a real key from `raxd key create`. For the
self-signed certificate, either trust `cert.pem` in the client's trust store, or, for a Node-based
client in development, set `NODE_TLS_REJECT_UNAUTHORIZED=0` (insecure — dev only).

> **Client caveat (not a server defect).** Some MCP clients/versions do not forward custom headers
> (such as `Authorization`) during their initial health/`initialize` step. If a client cannot reach
> `raxd`, verify the channel directly with the `curl` smoke-test above (which sends the header
> reliably) before assuming the server is at fault.

## Audit

Every `tools/call` writes **one** structured audit line to the same stderr audit stream as the rest of
the server, in `charmbracelet/log` `key=value` form:

```
time=<UTC> level=INFO msg=MCP fp=<fingerprint> remote=<IP:port> tool=<name> result=ok
```

- `fp` is the 12-hex-character key fingerprint (`keystore.Fingerprint`) — **never** the key body.
- `remote` is the client `IP:port` (it matches the `remote=` on the transport `AUTH` line for the same
  request).
- `tool` is the tool name (`ping` or `server_info`).
- `result` is `ok` on success.

Example, with the transport `AUTH` line that precedes it on the same request:

```
time=2026-05-21T14:32:01Z level=INFO msg=AUTH fp=a3f9c1d2e847 remote=127.0.0.1:54312
time=2026-05-21T14:32:01Z level=INFO msg=MCP  fp=a3f9c1d2e847 remote=127.0.0.1:54312 tool=ping result=ok
```

So a single `tools/call` produces two lines: the transport `AUTH` record (the connection passed all
gates) and the MCP record (the tool ran). The `tool=` field appears **only** on MCP records;
connection-only records (`AUTH`/`FAIL`/`DENY`/`RATE`) never carry it. No audit field ever contains the
key body, the raw `Authorization` header, the stored hash, the salt, or the private TLS key. For the
non-MCP audit lines (`AUTH`/`FAIL`/`DENY`/`RATE`), see [`commands.md`](commands.md#audit-stream).

## Scope and limitations

What this build does **not** include:

- **`execute_command` / command execution** — not implemented. There is no shell and no `exec` behind
  the MCP server. The tool is not registered; calling it returns a JSON-RPC error. (Planned in the
  `command-exec` task.)
- **`upload_file` / file transfer** — not implemented; same as above. (Planned in the `file-upload`
  task.)
- **MCP Resources and Prompts** — not advertised and not implemented. `initialize` advertises **only**
  the `tools` capability.
- **mTLS / client certificates** — out of scope. Authentication is by API key only.
- **Sessions / server→client streaming** — the server is stateless; `GET /mcp` returns `405`.

`ping` and `server_info` are intentionally minimal: they prove the protocol, transport,
authentication, and audit work end to end without depending on any unimplemented feature. The two
tools register at the same extension point where `execute_command` and `upload_file` will be added by
later tasks — behind the same authentication, rate limiting, and audit, without changing the route or
the transport.

> **Run it in Docker.** Like all of `raxd`, `serve` (and therefore the MCP server) is built and run
> inside a container only (security baseline §6). It opens a network listener, so running it on the
> host is out of scope.

## Related documents

- [`commands.md`](commands.md#raxd-serve) — the `serve` command, the request pipeline, response codes,
  the audit stream, and startup/shutdown output.
- [`configuration.md`](configuration.md#networking-and-serve-fields) — the port, allowlists, and
  rate-limit settings that `serve` (and the MCP endpoint behind it) reads from `config.yaml`.
- [`troubleshooting.md`](troubleshooting.md) — common `serve`, TLS, key, and connection problems.
- [`development.md`](development.md) — building and testing in Docker.

## Author

**Vladimir Kovalev, OEM TECH** — author of raxd.
