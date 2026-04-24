# Synthetic Credential Fixtures

Hazmat docs, tests, examples, screenshots, and shell snippets must not contain
values that match provider-issued credential formats.

## Rule

Use obviously fake `example-*` placeholder values instead of realistic-looking
samples.

Good:

```text
example-anthropic-key
example-openai-key
example-gemini-key
example-google-api-key
example-github-pat
example-aws-access-key-id
example-aws-secret-access-key
example-openrouter-key
example-context7-key
```

Bad:

```text
AIza...
sk-ant-...
ghp_...
github_pat_...
AKIA...
sk-or-v1-...
ctx7sk-...
```

## Allowlist Strategy

Hazmat keeps the allowlist narrow on purpose.

1. First choice: rewrite the fixture so it does not resemble a real secret.
2. If a generic scanner still flags a safe `example-*` value, allowlist the
   specific obviously fake substring in `.hazmat/hooks/gitleaks.toml`.
3. Use path allowlists only for scanner-definition files, lockfiles, or other
   generated files where the literal pattern source must exist.
4. Never path-allowlist docs, tests, examples, or screenshots just because
   they contain credential-shaped material. Change the fixture instead.

This policy exists because a "fake" value that still matches a provider's
format can trigger external incident-response work once it reaches published
history.
