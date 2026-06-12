# Local MCP Servers Under Hazmat

Use this recipe when an agent harness starts local stdio MCP servers and you
want those server processes contained by the same Hazmat session.

The key rule is simple: configure MCP inside the contained agent environment.
Do not import a host MCP config wholesale and hope the paths, tokens, sockets,
and network assumptions still mean the same thing.

## What Hazmat Contains

For local stdio MCP servers, Hazmat contains the harness process and the child
processes it starts. That gives the MCP server the same outer boundary as the
agent:

- the dedicated `agent` user
- the session filesystem grants
- the selected network mode
- the credential-deny floor
- the per-session policy and cleanup behavior for the selected backend

MCP annotations are still tool-risk hints. They help classify whether a tool is
read-only, destructive, idempotent, or open-world, but they do not enforce host
access. Hazmat's session contract is the enforcement input because it compiles
to backend policy.

## Recommended Shape

1. Pin or vendor the MCP server package instead of launching an unpinned
   `latest` package at runtime.
2. Configure the MCP server inside the contained harness profile, not by
   copying host config blindly.
3. Preview the session contract:

   ```bash
   hazmat explain --for claude --integration node --network none
   ```

4. Start the harness with the same integrations and network mode:

   ```bash
   hazmat claude --integration node --network none
   ```

5. Add only the smallest extra grants needed for that MCP server.

Use `--network none` by default for filesystem and local developer-tool MCP
servers. Use the default network only when the server genuinely needs outbound
access, such as a documentation search service or a remote API client.

## Filesystem Grants

The project directory is already the writable root. Most MCP servers should not
need more write access.

Typical grants:

| Server type | Usual Hazmat shape |
| --- | --- |
| Project filesystem server | No extra grant. Limit the server's own root to the project directory. |
| Documentation reader | `-R /path/to/docs` for a narrow read-only documentation tree. |
| Language or build tool server | Activate the stack integration, such as `node`, `python-uv`, `go`, or `rust`, so Hazmat resolves read-only toolchain/cache roots. |
| Code generator | Prefer writing into the project tree. If it must write elsewhere, stop and document a narrow `-W` grant. |
| Host Docker helper | Do not punch the host Docker socket into native containment. Use Docker Sandbox or a VM-oriented workflow. |

Never grant broad host roots such as `-R ~`, `-W ~`, `-R /`, or `-W /`.
Never grant credential directories, browser profiles, keychains, cloud config
directories, password-manager state, `SSH_AUTH_SOCK`, or host daemon sockets.

## Network Mode

Choose network mode per session, not per MCP annotation.

- `--network none`: best for filesystem servers, AST/indexing servers, local
  test helpers, and offline code review.
- Default network: acceptable for a server whose core job is remote access,
  such as fetching public docs or calling a remote issue tracker.

If only one MCP server needs network, consider running it in a separate Hazmat
session from offline filesystem tools. Hazmat does not currently provide
per-MCP-server network isolation inside one harness process tree.

## Credential Caveats

MCP subprocesses can inherit credentials intentionally granted to the harness
process. User isolation prevents a compromised MCP server from reading the host
user's ambient `~/.ssh`, cloud config, browser profile, or keychain files, but
it does not make a credential safe after you explicitly hand it to the harness.

Use these rules:

- Do not export provider API keys, GitHub tokens, cloud keys, or SSH agent
  sockets into the harness environment for convenience.
- Prefer dedicated, low-scope tokens for MCP servers that truly need remote API
  access.
- Do not load unrelated MCP servers in a session that has a sensitive
  harness-level credential grant.
- Split sessions when one task needs credentials and another task only needs
  filesystem or local tool access.
- Treat package-manager launched MCP servers as supply-chain code. Pin versions
  and avoid runtime `latest` installs.

If a future harness or broker supports per-server credential handles, use that.
Until then, assume every local MCP child in the session can see credentials
that the harness exports to its process environment.

## Example: Project Filesystem Server

Use this for a server that reads or writes only the repository.

Session shape:

```bash
hazmat explain --for claude --network none
hazmat claude --network none
```

MCP configuration shape:

- command runs from the contained agent profile
- server root is the project directory
- no extra `-R` or `-W` grants
- no credential env vars
- no outbound network

This is the safest local MCP shape. The server can still modify the project,
because the project is the session's writable root, but it cannot reach host
credential directories or the network.

## Example: Developer Tool Server

Use this for a server that shells out to local stack tooling, such as a Node,
Python, Go, or Rust helper.

Session shape:

```bash
hazmat explain --for claude --integration node --network none
hazmat claude --integration node --network none
```

For another stack, replace `node` with the required integration:

```bash
hazmat integration list
hazmat integration show python-uv
hazmat integration show go
```

Keep the MCP server's writable output inside the project. If the tool reports a
missing toolchain path, prefer adding or improving a Hazmat integration over
granting broad home-directory access.

## Example: Remote API Server

Use this for a server that calls a remote issue tracker, documentation API, or
other network service.

Session shape:

```bash
hazmat explain --for claude --integration node
hazmat claude --integration node
```

Before granting credentials, decide whether the session can be split:

- Session A: offline filesystem and developer-tool MCP servers with
  `--network none`.
- Session B: one remote API MCP server with the narrowest possible token and no
  unrelated MCP servers loaded.

Do not pass your normal shell's cloud or Git environment into the session. If
the remote server needs a token, use a dedicated token and document why that
token is safe for every MCP child in that session.

## Import And Migration

Hazmat intentionally does not import host MCP config as part of Claude, Qwen,
Hermes, or other profile migration. MCP config is executable integration state,
not just preference data.

Recreate MCP entries one by one inside the contained profile and preview the
Hazmat session contract after each server class. If a server expects a host path
or token, convert that expectation into an explicit Hazmat grant or credential
decision instead of copying the host assumption.

Read more:

- [AGENTS.md Hazmat security snippet](agents-md.md)
- [Session contract vocabulary](../session-contract-vocabulary.md)
- [Claude import and why MCP is manual](../claude-import.md#why-mcp-is-manual)
- [Threat matrix MCP caveats](../threat-matrix.md)
