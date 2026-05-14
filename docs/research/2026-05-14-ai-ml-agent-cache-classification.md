# AI / ML / Agent Cache Path Classification

**Date**: 2026-05-14
**Scope**: Classify common AI/ML/agent stack paths as either (a) safe to grant read on through a future integration, or (b) credential-bearing and therefore belonging on the seatbelt credential deny list. **Hazmat's threat model is secrets exposure and host integrity, not upstream integrity of what the agent loads** — model weights and downloaded code are not vetted by hazmat, the same way pip/npm wheels are not vetted.
**Status**: Research only. No code changes in this pass. Follow-up beads issues filed for the deny-list additions.

## Classification table

| Path | Default location (macOS) | Class | Notes |
|---|---|---|---|
| HuggingFace model + dataset cache | `~/.cache/huggingface/{hub,datasets,xet}/` | Safe cache | Just model artifacts. No credentials in the data subdirs themselves. |
| HuggingFace token | `~/.cache/huggingface/token` or `~/.cache/huggingface/stored_tokens` or `$HF_HOME/token` | **CREDENTIAL** | Written by `huggingface-cli login`. A bearer token. Must be denied even if the cache root is granted. |
| Ollama models | `~/.ollama/models/` | Safe cache | Local-only model weights. No remote auth. |
| Ollama signing key | `~/.ollama/id_ed25519` (+ `.pub`) | **CREDENTIAL** | Private key used by Ollama for client identity. The pub side is fine; the priv side must not be readable by the agent. |
| Ollama logs | `~/.ollama/logs/` | Safe cache | Runtime logs. No tokens. |
| PyTorch hub cache | `~/.cache/torch/hub/` | Safe cache | Downloaded checkpoints. |
| TensorFlow Hub cache | `~/.cache/tfhub_modules/` | Safe cache | Downloaded modules. |
| Hugging Face Diffusers cache | `~/.cache/huggingface/hub/` | Safe cache | Same dir as above. |
| Jupyter user config | `~/.jupyter/jupyter_*_config.py` | **CREDENTIAL** (potential) | May contain `c.NotebookApp.password` / `c.ServerApp.token` for server auth. Treat as credential. |
| Jupyter runtime | `~/.local/share/jupyter/runtime/` | **CREDENTIAL** | Per-session JSON files with active server tokens (`token` field). Must be denied while a server is live. |
| LangChain project files | usually in-project | Safe (in-project) | No global cache. |
| LangSmith API token | `~/.langsmith/` or `LANGSMITH_API_KEY` env | **CREDENTIAL** | API key for LangSmith tracing service. |
| OpenAI SDK | env var only (`OPENAI_API_KEY`) | **CREDENTIAL via env** | No file cache. Env already covered by hazmat's existing `*_API_KEY` credential-shape rejection. |
| Claude Desktop / Claude Code config | `~/Library/Application Support/Claude/claude_desktop_config.json` + `buddy-tokens.json` | **CREDENTIAL** | MCP server configs commonly embed API tokens for GitHub, Notion, Linear, custom servers. `buddy-tokens.json` is self-explanatory. **Already protected** because it's not under agent home and not in any read_dir today, but worth adding to the deny list as defense-in-depth in case a future integration grants `~/Library/Application Support`. |
| Cursor settings | `~/Library/Application Support/Cursor/User/settings.json` | **CREDENTIAL** (potential) | Can hold provider API keys for AI features. Same protection class as Claude Desktop. |
| MCP server configs (per-project) | `<project>/.mcp.json` | Mixed | In-project. Lives inside the project tree, which is fully read-write to the agent by design. **Not hazmat's problem to deny** — if the user puts secrets in their project, that's outside the containment boundary. Document the convention; don't deny. |
| Continue / Cline / Aider config | `~/.continue/`, `~/.cline/`, `~/.aider.conf.yml` | **CREDENTIAL** (potential) | Provider API keys. Add to deny list. |
| Vector DB local data | `<project>/chroma/`, `<project>/*.idx`, `<project>/data.db` | Mixed (in-project) | In-project. Treat as project data; no special handling. |
| Ollama API socket | localhost:11434 | Network | Not a file path. Network access is governed separately. Out of scope here. |

## Recommendations

### To land in a follow-up issue (not this task)

Add to `hazmat/integration_manifest.go` `credentialDenySubs` (paths are relative to home):

```
"/.cache/huggingface/token",            # HF bearer
"/.cache/huggingface/stored_tokens",    # HF newer multi-token store
"/.ollama/id_ed25519",                  # Ollama private signing key
"/.jupyter",                            # Jupyter user config (may hold password/token)
"/.local/share/jupyter/runtime",        # live server tokens
"/.langsmith",                          # LangSmith API key
"/.continue",                           # Continue.dev API keys
"/.cline",                              # Cline API keys
"/.aider.conf.yml",                     # Aider config (file, not dir)
"/Library/Application Support/Claude",  # Claude Desktop config + MCP tokens
"/Library/Application Support/Cursor",  # Cursor settings (provider API keys)
```

Each addition is in `credentialDenySubs` and emitted as a `(deny file-read* file-write* (subpath ...))` rule in section 6 of the SBPL, which is last-match-wins. Adding them does not require any read_dir to be granted first — they fire even if no integration claims the parent — so they are defense-in-depth.

### Eligible for safe-cache integrations (future work)

Three integrations could reasonably ship later, scoped to grant read on cache-only directories:

1. `huggingface` — `~/.cache/huggingface/hub`, `~/.cache/huggingface/datasets`, `~/.cache/huggingface/xet`. Token denied per above. Likely-needed env: `HF_HOME`, `HF_HUB_OFFLINE` (path pointer / mode flag, both safe).
2. `ollama` — `~/.ollama/models`, `~/.ollama/cache`, `~/.ollama/logs`. Private key denied per above. The `OLLAMA_HOST` env points at a network endpoint, not a path — network access is governed by hazmat's pf rules and not in the integration's gift.
3. `pytorch-torch-hub` — `~/.cache/torch/hub`, `~/.cache/torch/checkpoints`. No credentials.

Each ships as a separate small manifest, the same shape as `python-uv`. They are NOT urgent — they only matter for offline ML/RL workflows where the agent invokes `transformers.from_pretrained(...)` against cached weights, etc.

### Out of scope

- Vetting the content of model weights or downloaded code. Hazmat is a containment tool; trust of upstream artifacts is the user's concern, same as for pip wheels / npm packages.
- Pre-emptively integrating LangChain, LangGraph, CrewAI, or other framework-level libraries — their files live inside the user's project, which is already in the agent's read-write scope. No new grants needed.
- Network policy for talking to OpenAI / Anthropic / HF API endpoints. That stays governed by pf rules and the existing `*_API_KEY` rejection in `safeEnvKeys`.

## Smoke fixture candidates (future)

- Local-only Python: a `pyproject.toml` that uses `transformers` to load a small model from `~/.cache/huggingface/hub/` (pre-cached on host). Run `python -c "from transformers import AutoModel; AutoModel.from_pretrained('microsoft/deberta-v3-base')"` inside hazmat exec.
- Local-only Node/TS AI app: an `@huggingface/inference` script that points at `OLLAMA_HOST=http://localhost:11434` against a pre-pulled model. The localhost daemon is intentionally not granted; user must run with `--allow-host-process` or equivalent (separate decision).

These are listed for future test-fixture work; not built in this task.

## Follow-up issues

- `sandboxing-lhd8.5.deny` — add the 11 credentialDenySubs entries listed above, with tests.
- `sandboxing-lhd8.5.huggingface` — minimal `huggingface` integration manifest (cache reads only).
- `sandboxing-lhd8.5.ollama` — minimal `ollama` integration manifest (model dir reads only).
- `sandboxing-lhd8.5.pytorch-hub` — minimal `pytorch-torch-hub` integration manifest.

These are filed but not started.
