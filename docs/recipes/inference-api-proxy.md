# Inference API Proxy Through Muginn

Use this shape when a contained harness can talk to an OpenAI-compatible local
API endpoint and provider routing should stay outside the harness process.

The API proxy is inference egress brokering. It is not MCP containment and it is
not full tool containment. Hazmat still relies on the selected session backend
for filesystem, process, and network boundaries.

## Runtime Shape

1. Hazmat starts a session-scoped local HTTP/SSE proxy.
2. The proxy binds only to a local attach point, such as `127.0.0.1`, and
   requires a per-session token.
3. The contained harness receives only the local proxy base URL and that
   session token.
4. The proxy forwards supported OpenAI-compatible routes to Muginn.
5. Durable provider keys stay host-side or Muginn-side.

Supported proxy routes currently include:

| Route | Behavior |
| --- | --- |
| `POST /v1/responses` | Forwarded to the configured upstream |
| `POST /v1/chat/completions` | Forwarded, including SSE streaming responses |
| `POST /v1/embeddings` | Forwarded |

Unsupported endpoints fail closed before any upstream request is made.

## Hermes Adapter

Hermes is the first supported API proxy env adapter because Hazmat already
launches it as a foreground process with managed `HERMES_HOME`, and Hermes v1
does not import host `~/.hermes` profile state.

Start a Hermes session through the local Muginn proxy with:

```bash
hazmat hermes --api-proxy=muginn -- chat --model muginn/subscription-auto
```

On this workstation, Hazmat defaults to
`direnv exec ~/ops ~/workspace/muginn/muginnctl proxy start --daemon --model muginn/subscription-auto --output json`
when `~/ops` and `~/workspace/muginn/muginnctl` exist. Override with
`HAZMAT_MUGINNCTL` or `HAZMAT_MUGINN_OPS_DIR`; set
`HAZMAT_MUGINN_OPS_DIR=-` to run `muginnctl` without the ops direnv profile.

The adapter renders:

```text
OPENAI_BASE_URL=http://127.0.0.1:<port>
OPENAI_API_KEY=<session-token>
```

That `OPENAI_API_KEY` value is the local proxy session token, not a durable
provider key. Existing provider env vars such as `ANTHROPIC_API_KEY`,
`OPENAI_API_KEY`, `GEMINI_API_KEY`, and `OPENROUTER_API_KEY` are excluded in
proxy mode.

## Credential Boundary

The contained harness should not receive:

- Muginn caller tokens
- provider API keys
- host provider profile files
- OAuth or subscription profile state
- host `~/.hermes`, `~/.codex`, `~/.claude`, or similar harness roots

The host-side proxy may add a Muginn caller token on the upstream leg. Proxy
evidence records the credential mode and redaction markers, not raw token
bytes.

## Errors And Streaming

Streaming responses with `Content-Type: text/event-stream` are passed through
without buffering the full stream. Proxy evidence records `stream:start` and
`stream:end`.

Muginn upstream failure bodies are sanitized before they are returned to the
harness, so provider or routing secrets in an upstream error body are not echoed
back into the contained process.

## Testing Boundary

Default tests are hermetic and non-sudo:

```bash
go test ./llmproxy ./llmproxyadapter
```

Those tests use fake upstream and fake Muginn servers. They cover non-streaming
forwarding, SSE pass-through, auth failure, unsupported endpoints, upstream
error sanitization, provider-key absence, and response-body cleanup.

Live Muginn validation is approval-gated. Do not make it part of default local
or CI test runs. If a future smoke script starts a real Muginn path or mints a
caller token, treat the exact command as sudo-adjacent in Hazmat agent
workflows and ask before running it.
