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
| Tools | `ping`, `server_info` (read-only), **`execute_command`** (runs a command on the host) |
| Authentication | inherited from the transport (`Authorization: Bearer rax_live_…`) |

> **`execute_command` is dangerous.** Unlike `ping` and `server_info`, the third tool actually runs a
> binary on the host on behalf of an authenticated client — remote code execution of the SSH class.
> Read the [`execute_command` security guide](execute-command-security.md) before enabling it against
> any real host, and note in particular that command **arguments are logged verbatim** (do not pass
> secrets in `args` — see [§ secrets in arguments](execute-command-security.md#1-do-not-pass-secrets-in-command-arguments-argv)).

Before the MCP server existed, `/mcp` returned `501 Not Implemented` like every other non-health route.
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

`tools/list` returns **three** tools: `ping`, `server_info` (both read-only, no input), and
`execute_command` (runs a command on the host). There is no `upload_file` and no file transfer (see
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

### `execute_command`

- **Description:** run a **non-interactive** command on the `raxd` host as a **binary plus an argument
  list**, **without a shell**, and return the captured output, exit code, duration, and the timeout /
  truncation flags. This is the SSH-class capability of `raxd`: an authenticated client can run a
  binary with the privileges of the `raxd serve` process.

> **Read the [security guide](execute-command-security.md) first.** This tool is a deliberate "dangerous
> primitive". The safety controls described below (no shell, mandatory timeout, optional allowlist,
> output/argument limits, controlled `cwd`/environment, audit) are enforced **on the server**, not in
> the schema. The two warnings you must not skip: **do not pass secrets in `args`** (they are logged
> verbatim) and **the allowlist matches the raw command string exactly** (`ls` ≠ `/bin/ls`).

#### What it does

The command is launched with `exec.CommandContext(ctx, binary, args...)` — a binary and a literal
argument list. There is no shell: `sh -c <string>` is never used, and shell metacharacters in `args`
(`;`, `|`, `$()`, `&&`, `>`, backticks) are passed to the process as **literal** arguments, not as
shell syntax. Every call is authenticated by the transport before it runs and is recorded in the audit
stream.

The binary is resolved on the daemon's `PATH`. A bare name (for example `ls`) is looked up with
`LookPath`; an absolute path (for example `/bin/ls`) is used as-is after a quick existence check. A
**relative** path from the working directory is rejected (Go's `exec.ErrDot`), so a command cannot be
hijacked by a binary dropped in `cwd`.

#### Input — `ExecInput`

The input schema is **strict** (`additionalProperties: false`): any field other than the four below
makes the call fail validation with `isError: true` **before** anything runs. There is **no `env`
field** — the child environment is server-controlled (see the security guide).

| Field | Type | Required | Meaning |
|-------|------|----------|---------|
| `command` | string | **yes** | binary name or absolute path to run. A bare name is resolved on the daemon's `PATH`; a relative path is rejected |
| `args` | array of strings | no | literal arguments (no shell interpretation). **Logged verbatim — never put a secret here** |
| `timeout_ms` | integer | no | timeout in milliseconds. `0` or omitted → the configured `exec.default_timeout_ms` (default 30000). A value above `exec.max_timeout_ms` (default 300000) is **rejected** |
| `cwd` | string | no | working directory. Omitted → `exec.default_cwd` (default `/tmp`). A provided value must exist and be a directory, otherwise the call is rejected |

#### Output — `ExecOutput`

A call that actually ran the command returns **structured content** with **exactly seven** fields, plus
a short human-readable text summary. The result is **not** an error in this case (`isError` is absent or
`false`), even if the exit code is non-zero or the command timed out.[^iserror]

| Field | Type | Meaning |
|-------|------|---------|
| `stdout` | string | captured standard output (≤ `exec.max_output_bytes`; see `stdout_truncated`) |
| `stderr` | string | captured standard error (≤ `exec.max_output_bytes`; see `stderr_truncated`) |
| `exit_code` | integer | the process exit code. A non-zero value is **not** a tool error |
| `duration_ms` | integer | how long the command ran, in milliseconds |
| `timed_out` | boolean | `true` if the command was killed by the timeout (the whole process tree is killed); output is partial |
| `stdout_truncated` | boolean | `true` if stdout reached `exec.max_output_bytes` and was cut off |
| `stderr_truncated` | boolean | `true` if stderr reached `exec.max_output_bytes` and was cut off |

The text summary block has the form
`exit=<code> duration=<ms>ms timed_out=<bool> stdout=<N>B stderr=<M>B`, with ` truncated` appended when
either stream was truncated. The full output is in `structuredContent`; the text block is a compact
summary for the model.

> **`exit_code` when the command is killed.** When a command is killed by the timeout, `exit_code` is
> reported as `-1`. The defining field in that case is `timed_out: true`, not `exit_code`: treat
> `timed_out` as authoritative for a killed command.

#### Behaviour and error mapping

This is where `execute_command` differs from a naive "run it" tool. Distinguish three layers:

- **Transport rejections (HTTP status, before the tool runs).** Exactly as for `ping`/`server_info`:
  no/invalid/revoked key → `401`; corrupt key store → `403`; bad `Host`/`Origin` → `403`; rate limit
  exceeded → `429`; `GET /mcp` → `405`. The command is **not** started; see
  [Authentication](#authentication).
- **Protocol errors (JSON-RPC, HTTP 200).** Malformed body → `-32700`; not a valid JSON-RPC request →
  `-32600`; unknown method → `-32601`; **unknown tool name** in `tools/call` (for example a typo like
  `exec` instead of `execute_command`) → `-32602` (`Invalid params`). The command is **not** started.
- **Tool results.** Once the SDK dispatches to the tool, the outcome is in the `result`:

| Situation | Result |
|-----------|--------|
| **Non-zero exit code** | **not an error** — `isError` absent/false, `exit_code` is the non-zero value |
| **Timeout** | **not an error** — `isError` absent/false, `timed_out: true`, partial output |
| **Output over the limit** | **not an error** — `isError` absent/false, `stdout_truncated`/`stderr_truncated: true` |
| Extra/unknown input field (incl. `env`), or wrong type | `isError: true` (input validation, by the SDK, before the handler) |
| Command not in the allowlist (allowlist on) | `isError: true`, message `command not allowed`; **not** started; `DENY` audit |
| `args` count over `exec.max_args`, or an argument over `exec.max_arg_len` | `isError: true`, message states the limit; **not** started; `DENY` audit |
| `timeout_ms` above `exec.max_timeout_ms` | `isError: true`, message states the limit; **not** started; `DENY` audit |
| `deny_root: true` and the daemon is root | `isError: true`, `execution as root is forbidden by policy`; **not** started; `WARN` + `DENY` audit |
| Binary not found / relative-path binary | `isError: true`, message `command not found`; **not** started; `FAIL` audit |
| Invalid `cwd` (missing, or not a directory) | `isError: true`, message `command not found`; **not** started; `FAIL` audit |

The key idea for an agent: a **non-zero exit code and a timeout are normal results**, not errors —
inspect `structuredContent` and decide what to do next. `isError: true` means the command was rejected
or could not start (allowlist, limits, `deny_root`, missing binary, bad `cwd`), not that the command ran
and failed.

After any rejected or failed call the server stays up: the next valid call (for example `ping`) still
works.

#### curl examples

Run these from inside the container running `raxd serve`, with `KEY` set to a key from
`raxd key create` and `<port>` set to your port (default `7822`). `-k` skips certificate verification
for this local test. The `result` / `error` shapes match what the Go MCP SDK produces (the same shapes
as the `ping` examples above).

**Success — the command ran, even though it exits non-zero.** A non-zero exit code is **not** a tool
error:

```sh
curl -k https://127.0.0.1:<port>/mcp \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "MCP-Protocol-Version: 2025-11-25" \
  -d '{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"execute_command","arguments":{"command":"ls","args":["-la","/nope"],"timeout_ms":5000}}}'
```

```json
{
  "jsonrpc": "2.0",
  "id": 10,
  "result": {
    "content": [
      { "type": "text", "text": "exit=2 duration=14ms timed_out=false stdout=0B stderr=41B" }
    ],
    "structuredContent": {
      "stdout": "",
      "stderr": "ls: cannot access '/nope': No such file or directory\n",
      "exit_code": 2,
      "duration_ms": 14,
      "timed_out": false,
      "stdout_truncated": false,
      "stderr_truncated": false
    }
  }
}
```

(`isError` is omitted on success — see the note below. The exact numbers, `stderr` text, and
`duration_ms` depend on your system.)

**Deny by allowlist** — when `exec.allowlist` is set and the command is not in it (here `rm`). The
command is **not** started:

```sh
curl -k https://127.0.0.1:<port>/mcp \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "MCP-Protocol-Version: 2025-11-25" \
  -d '{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"execute_command","arguments":{"command":"rm","args":["-rf","/"]}}}'
```

```json
{
  "jsonrpc": "2.0",
  "id": 11,
  "result": {
    "content": [ { "type": "text", "text": "command not allowed" } ],
    "isError": true
  }
}
```

**Binary not found** — `isError: true`, and the server stays up:

```sh
curl -k https://127.0.0.1:<port>/mcp \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "MCP-Protocol-Version: 2025-11-25" \
  -d '{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"execute_command","arguments":{"command":"definitely-not-a-binary-xyz"}}}'
```

```json
{
  "jsonrpc": "2.0",
  "id": 12,
  "result": {
    "content": [ { "type": "text", "text": "command not found" } ],
    "isError": true
  }
}
```

The message is neutral by design — it does not leak paths or internal detail.

**Timeout** — a command that runs longer than its timeout is killed (the whole process tree), and the
result is **not** an error: `timed_out: true` with partial output:

```sh
curl -k https://127.0.0.1:<port>/mcp \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "MCP-Protocol-Version: 2025-11-25" \
  -d '{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"execute_command","arguments":{"command":"sleep","args":["60"],"timeout_ms":1000}}}'
```

```json
{
  "jsonrpc": "2.0",
  "id": 13,
  "result": {
    "content": [ { "type": "text", "text": "exit=-1 duration=1003ms timed_out=true stdout=0B stderr=0B" } ],
    "structuredContent": {
      "stdout": "",
      "stderr": "",
      "exit_code": -1,
      "duration_ms": 1003,
      "timed_out": true,
      "stdout_truncated": false,
      "stderr_truncated": false
    }
  }
}
```

**Extra input field** — the strict schema rejects an unknown field (here `env` and `shell`) **before**
the handler, so the command never runs:

```sh
curl -k https://127.0.0.1:<port>/mcp \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "MCP-Protocol-Version: 2025-11-25" \
  -d '{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"execute_command","arguments":{"command":"echo","env":{"X":"1"},"shell":true}}}'
```

```json
{
  "jsonrpc": "2.0",
  "id": 15,
  "result": {
    "content": [ { "type": "text", "text": "validating \"arguments\": additional properties not allowed: env, shell" } ],
    "isError": true
  }
}
```

(The exact text comes from the SDK; the point is `isError: true` and "nothing ran".)

[^iserror]: The `isError` field is serialized with `omitempty`, so for a successful tool result (where
the server does not set it) it is **absent** from the JSON. It appears, set to `true`, only when a
tool reports its own error. See [Behaviour and error handling](#behaviour-and-error-handling).

## Authentication

Authentication is **inherited from the transport** and happens **before** any MCP processing. The MCP
layer has no authentication of its own. This applies to `execute_command` exactly as to `ping` — an
unauthenticated call never runs a command.

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
the SDK dispatcher, so no command is executed. The reason is recorded in the audit stream, not in the
response body (rejection bodies are empty by design). For the full transport reference, see
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
| Unknown tool name in `tools/call` (a typo, e.g. `exec`) / bad params | JSON-RPC error `-32602` (Invalid params) |
| A tool's own input-validation error (e.g. an extra field) | `isError: true` inside the `result` (a *tool* error, not a protocol error) |
| A tool's own execution error (e.g. `execute_command` deny / not found) | `isError: true` inside the `result` |

Two points worth stressing:

- **An unknown tool name is never executed.** Only `ping`, `server_info`, and `execute_command` are
  registered. Calling any other name (for example a typo, or `upload_file`, which is not implemented)
  returns a JSON-RPC error and runs nothing. After such an error the server stays up, and a valid
  `ping` still returns `pong`.
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

For `execute_command`, see the [examples above](#curl-examples).

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

Every `tools/call` writes structured audit lines to the same stderr audit stream as the rest of the
server, in `charmbracelet/log` `key=value` (logfmt) form. The key body is **never** logged — only the
fingerprint.

For `ping` and `server_info`, one record is written **after** the call:

```
time=<UTC> level=INFO msg=MCP fp=<fingerprint> remote=<IP:port> tool=<name> result=ok
```

- `fp` is the 12-hex-character key fingerprint (`keystore.Fingerprint`) — **never** the key body.
- `remote` is the client `IP:port` (it matches the `remote=` on the transport `AUTH` line for the same
  request).
- `tool` is the tool name (`ping` or `server_info`).
- `result` is `ok` on success.

### `execute_command` audit records

`execute_command` writes its **own** audit record (it is the one tool that owns its audit path, so that
the record can carry the command, arguments, exit code, and duration). It writes **exactly one** primary
record per call, in one of these forms — plus an extra `WARN` record when the daemon is root:

| `msg` | level | When | Extra fields |
|-------|-------|------|--------------|
| `MCP` | `INFO` | the command ran (any exit code, **including a timeout**) | `tool=execute_command result=ok command= args= exit_code= duration= timed_out=` |
| `DENY` | `WARN` | allowlist deny, input-limit deny, or `deny_root` deny — command **not** started | `tool=execute_command reason= command= args=` |
| `FAIL` | `WARN` | binary not found, relative path, or invalid `cwd` — command **not** started | `tool=execute_command reason= command= args=` |
| `WARN` | `WARN` | extra record on **every** call when the daemon is root | `tool=execute_command reason=running-as-root command= args=` |

Notes:

- The command-specific fields (`command=`, `args=`, `exit_code=`, `duration=`, `timed_out=`) appear
  **only** when `tool=execute_command`. The `ping`/`server_info` `MCP` records and the connection
  records (`AUTH`/`FAIL`/`DENY`/`RATE`) are unchanged.
- `exit_code=`, `duration=`, and `timed_out=` appear **only** on the success (`MCP`) record — on
  `DENY`/`FAIL` the command never started.
- A timeout is logged as a **success** record (`msg=MCP result=ok … timed_out=true`), not a `FAIL`.
- **Arguments are logged verbatim** (`args=[…]`) with no masking. **Do not put secrets in `args`.** See
  the [security guide](execute-command-security.md#1-do-not-pass-secrets-in-command-arguments-argv).

Example, with the transport `AUTH` line that precedes the MCP record on the same request:

```
time=2026-05-21T14:32:01Z level=INFO msg=AUTH fp=a3f9c1d2e847 remote=127.0.0.1:54312
time=2026-05-21T14:32:01Z level=INFO msg=MCP  fp=a3f9c1d2e847 remote=127.0.0.1:54312 tool=execute_command result=ok command=ls args=[-la,/nope] exit_code=2 duration=14ms timed_out=false
```

A denied call:

```
time=2026-05-21T14:32:05Z level=WARN msg=DENY fp=a3f9c1d2e847 remote=127.0.0.1:54320 tool=execute_command reason=not-allowed command=rm args=[-rf,/]
```

A root daemon (extra `WARN` record before the command's own record):

```
time=2026-05-21T14:32:09Z level=WARN msg=WARN fp=a3f9c1d2e847 remote=127.0.0.1:54330 tool=execute_command reason="running-as-root: raxd executing commands as root (euid==0); ensure raxd runs as non-root" command=ls args=[]
time=2026-05-21T14:32:09Z level=INFO msg=MCP  fp=a3f9c1d2e847 remote=127.0.0.1:54330 tool=execute_command result=ok command=ls args=[] exit_code=0 duration=3ms timed_out=false
```

So one authenticated `execute_command` call produces the `AUTH` connection record, the tool's own
record (`MCP`/`DENY`/`FAIL`), and — only when the daemon is root — one extra `WARN` record. No audit
field ever contains the key body, the raw `Authorization` header, the stored hash, the salt, or the
private TLS key. For the non-MCP audit lines (`AUTH`/`FAIL`/`DENY`/`RATE`), see
[`commands.md`](commands.md#audit-stream).

## Scope and limitations

What this build does **and does not** include:

- **`execute_command`** — **implemented.** An authenticated client can run a non-interactive command on
  the host (binary + argument list, no shell), with a mandatory timeout, an optional allowlist, output
  and argument limits, a server-controlled working directory and environment, and a per-call audit
  record. See the [tool reference above](#execute_command) and the
  [security guide](execute-command-security.md). What it does **not** do, by design for this version:
  - **No shell, no interactive/PTY sessions, no `stdin` streaming.** Output is returned in full (within
    limits) **after** the command finishes; there is no real-time output streaming.
  - **No client-supplied environment.** There is no `env` field; the child environment is a fixed
    server-side whitelist.
  - **No sandboxing.** No cgroups/rlimits/seccomp/namespaces — isolation relies on running the daemon
    as a non-root user inside a container.
- **`upload_file` / file transfer** — not implemented. (Planned in the `file-upload` task.)
- **MCP Resources and Prompts** — not advertised and not implemented. `initialize` advertises **only**
  the `tools` capability.
- **mTLS / client certificates** — out of scope. Authentication is by API key only.
- **Sessions / server→client streaming** — the server is stateless; `GET /mcp` returns `405`.

`ping` and `server_info` remain intentionally minimal: they prove the protocol, transport,
authentication, and audit work end to end. `execute_command` is registered at the **same** extension
point, behind the **same** authentication, rate limiting, and audit, without changing the route or the
transport.

> **Run it in Docker.** Like all of `raxd`, `serve` (and therefore the MCP server) is built and run
> inside a container only (security baseline §6). It opens a network listener and runs commands on the
> host, so running it on the host is out of scope.

## Related documents

- [`execute-command-security.md`](execute-command-security.md) — **mandatory** security warnings for
  `execute_command` (secrets in argv, allowlist semantics, `deny_root`/root, isolation, residual risks).
- [`commands.md`](commands.md#raxd-serve) — the `serve` command, the request pipeline, response codes,
  the audit stream, and startup/shutdown output.
- [`configuration.md`](configuration.md#command-execution-exec-fields) — the `exec.*` settings (allowlist,
  timeouts, limits, working directory, environment whitelist, `deny_root`) that `execute_command` reads
  from `config.yaml`, plus the networking/`serve` fields.
- [`troubleshooting.md`](troubleshooting.md) — common `serve`, TLS, key, connection, and
  `execute_command` problems.
- [`development.md`](development.md) — building and testing in Docker.

## Author

**Vladimir Kovalev, OEM TECH** — author of raxd.
</content>
