# Inference Through an OpenAI-Compatible Endpoint

Use this shape when a contained harness should send inference traffic through
an OpenAI-compatible facade whose lifecycle and provider routing stay outside
Hazmat.

The adapter is inference egress configuration. It is not MCP containment, an
endpoint process manager, or full tool containment. Hazmat still relies on the
selected session backend for filesystem, process, and network boundaries.

## Runtime Shape

1. The operator starts and authenticates an OpenAI-compatible endpoint outside
   Hazmat.
2. The operator exports `OPENAI_BASE_URL` and `OPENAI_API_KEY` together.
3. Hazmat validates the pair only after explicit proxy-mode selection.
4. The contained Hermes process receives that URL and token.
5. Endpoint lifecycle, provider credentials, routing, model discovery, and
   model changes remain outside Hazmat.

Hazmat never searches for or executes an endpoint helper. It does not fall back
to the stored OpenAI provider key when the environment pair is missing or
partial.

## Hermes Adapter

Hermes is the first supported adapter because Hazmat already launches it as a
foreground process with managed `HERMES_HOME` and does not import host
`~/.hermes` state.

Start the facade separately, then export its standard client configuration:

```bash
export OPENAI_BASE_URL=http://127.0.0.1:18743/v1
export OPENAI_API_KEY='<facade-access-token>'
hazmat hermes --api-proxy=openai-compatible -- chat
```

The mode is native-only and needs session network access. HTTPS endpoints are
accepted. Plain HTTP is accepted only for `localhost` or an IP loopback
address. URLs containing credentials, query parameters, or fragments are
rejected.

Hazmat deliberately does not inject `OPENAI_MODEL`. Hermes and the endpoint can
use standard `GET /v1/models` discovery, and model IDs are treated as opaque
strings. A selected model can be passed through Hermes normally:

```bash
hazmat hermes --api-proxy=openai-compatible -- chat --model '<model-id>'
```

## Credential Boundary

Proxy mode consumes exactly the invoking environment pair. Existing provider
environment values such as `ANTHROPIC_API_KEY`, a stored `OPENAI_API_KEY`,
`GEMINI_API_KEY`, and `OPENROUTER_API_KEY` are not substituted or forwarded by
the adapter.

The contained harness should not receive:

- durable provider API keys unrelated to the facade;
- host provider profile files;
- OAuth or subscription profile state;
- host `~/.hermes`, `~/.codex`, `~/.claude`, or similar harness roots.

Session plans record the `OPENAI_API_KEY` grant as redacted and never print its
value.

## Errors and Testing

Missing or partial environment input, unsafe endpoint URLs, unsupported
harnesses, non-native backends, and `--network none` fail before harness
launch.

Default tests are hermetic and non-sudo:

```bash
go test ./llmproxy ./llmproxyadapter
go test . -run 'Test(HermesOpenAICompatible|OpenAICompatible)'
```

The tests cover typed input validation, URL policy, provider-key exclusion,
model non-injection, redaction, and a regression fixture proving that a
discoverable executable is not run.
