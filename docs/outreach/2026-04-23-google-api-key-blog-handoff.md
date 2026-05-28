# Hazmat blog handoff

**Date:** 2026-04-23
**Owner:** operator
**Audience:** blog-writing agent
**Status:** review-ready handoff - no external writes executed

## 1) What this handoff is

This is the internal source packet for a blog article about the April 23, 2026
Google / Gemini API key exposure incident in `dredozubov/hazmat`.

The article should read as a clear engineering postmortem, not as damage
control copy. The most useful version is honest about the failure:

- a real credential was committed into a test fixture
- the exposure was detected by external secret scanning
- the root failure was process, not just one bad string
- the fixes had to cover revocation, history cleanup, local residue, and
  default-path prevention

This handoff is meant to help the writing agent stay accurate, avoid saying too
much, and keep the article focused on durable lessons rather than incident
theater.

---

## 2) Recommended editorial direction

### Recommended angle
**A realistic fake secret is still a real security incident.**

Why this is the strongest angle:

- it is true
- it is useful to other teams
- it avoids turning the post into a vendor blame piece
- it keeps the emphasis on engineering controls and review discipline

### Reserve angle 1
**What our secret-scanning gap looked like in an agent-heavy development flow**

Use this only if the piece needs more framing around AI coding harnesses. If
you go this route, keep the harness discussion secondary. The root cause was
still unsafe fixture shape plus missing local and CI gates.

### Reserve angle 2
**History rewrite is not incident response by itself**

This angle works if the post wants to stress the operational sequence:
revoke/rotate, clean published refs, scrub local residue, then add prevention.

### Suggested title options

1. `Postmortem: We Exposed a Google API Key in a Test Fixture`
2. `A Fake Secret Shaped Like a Real One Is Still a Real Incident`
3. `What a GitGuardian Alert Taught Us About Test Data and Secret Scanning`

### Recommendation

Lead with option 2 if the goal is broad usefulness.

Lead with option 1 if the goal is plain-spoken engineering transparency.

---

## 3) Non-negotiable facts

These are the facts the article should preserve.

1. GitGuardian reported a Google API key exposure on **2026-04-23 16:04:26
   UTC** for the public `dredozubov/hazmat` repository.
2. The exposed value lived in
   [hazmat/config_agent_test.go](../../hazmat/config_agent_test.go),
   not production runtime code.
3. The introducing change was the original public commit `48fce7e`, later
   rewritten. The current equivalent cleaned commit is
   `c9a406ace8e8606478e6706b6e77370513d4f8c4`.
4. The change was part of a legitimate feature: extending `hazmat config
   agent` so installed harnesses could seed `ANTHROPIC_API_KEY`,
   `OPENAI_API_KEY`, and `GEMINI_API_KEY`.
5. The incident was real even though the secret appeared in a test fixture. A
   public repo containing a provider-shaped or real secret still creates
   incident-response work and possible abuse opportunity.
6. The response included:
   - replacing the offending literal
   - rewriting history and force-pushing cleaned refs
   - rotating and deleting the key
   - scrubbing local log/session residue that captured key material
   - adding a default-path secret-pattern check in hooks and CI
7. The repo now has a Google API key pattern guard in
   [scripts/check-secret-patterns.sh](../../scripts/check-secret-patterns.sh),
   and the testing workflow documents that guard in
   [docs/testing.md](../testing.md).

---

## 4) Important nuance on attribution

If the article mentions which coding harness produced the change, use careful
language.

Best-supported internal conclusion:

- the change appears to have been produced in **Claude Code**
- the model in the local transcript is **`claude-opus-4-7`**
- the strongest supporting evidence is in private local transcript material
  that should not be quoted or linked from the public repo

Important limitation:

- git does not store "which harness authored this commit"
- this is forensic attribution from local transcripts, not a first-class git
  field

Recommended phrasing:

> Local transcripts strongly suggest the change was developed in Claude Code.

Avoid stronger wording like:

- "Git proves Claude Code wrote the commit"
- "Claude leaked the key"
- "The model exfiltrated a secret"

That framing overstates what we actually know.

---

## 5) Publish-safe timeline

Prefer a compressed timeline in the article.

- **2026-04-23 16:04:26 UTC**: GitGuardian reported a Google API key exposure
  in the public repository.
- **Same day**: investigation traced the exposure to a test fixture added while
  extending cross-harness API-key prompting.
- **Same day**: the repository history was rewritten so published refs no
  longer pointed at the exposed blob.
- **Same day**: the compromised key was rotated and then rotated again after
  local log/session cleanup revealed secondary copies on the operator machine.
- **Same day**: pre-commit, pre-push, and CI gained a guard for
  provider-shaped Google API keys.

Optional line if the article wants impact color without drama:

> We also saw opportunistic post-exposure requests against unrelated Google API
> surfaces, which were denied.

If used, keep it restrained and do not speculate beyond what the investigation
actually showed.

---

## 6) What is safe to say publicly

These points are safe and useful:

- the leaked value was in a test fixture
- the value should still be treated as compromised once public
- scanners caught it quickly
- the response required more than changing one file
- local logs and agent transcripts can become secondary exposure surfaces
- "fake but realistic" fixtures are a bad practice
- secret scanning belongs in the default path, not only in external monitors
- reviewers should treat credential-shaped test data as a blocking issue

Good language to reuse:

- "provider-shaped credential"
- "credential-shaped fixture"
- "default-path controls"
- "history cleanup and credential rotation happened in parallel"
- "we found no evidence of successful paid abuse"
- "we saw denied opportunistic traffic after exposure"

---

## 7) What not to publish

Do not include any of the following in the article:

- the full key string
- recognizable key fragments
- Google Cloud project IDs, project numbers, or key resource IDs
- local filesystem paths to secret-bearing logs if not needed for the lesson
- exact operational commands that would recreate access to the replacement key
- claims of certainty that nobody touched the key
- bot attribution presented as fact

Also avoid these narrative mistakes:

- do not frame the whole incident as "Claude Code failure"
- do not imply the harness alone caused the leak
- do not bury the fact that the secret was real enough to require rotation
- do not claim that history rewrite alone solved the problem

---

## 8) Strong sources for the writing agent

### Public or repo-safe sources

- [docs/2026-04-23-google-api-key-exposure-postmortem.md](../2026-04-23-google-api-key-exposure-postmortem.md)
- [scripts/check-secret-patterns.sh](../../scripts/check-secret-patterns.sh)
- [docs/testing.md](../testing.md)
- `git show --format=fuller --no-patch c9a406ace8e8606478e6706b6e77370513d4f8c4`

Private local transcript sources were reviewed for confidence and phrasing
discipline, but they are intentionally omitted from this repo copy. They are
not necessary to make the public article work.

---

## 9) Suggested article structure

### Section 1 - What happened

Open with the plain fact pattern: a Google API key exposure alert landed after
we committed a credential-shaped value into a test fixture while expanding
Hazmat's cross-harness config flow.

### Section 2 - Why this was still a real incident

Explain why "it was only in a test" is not a defense:

- scanners do not know intent
- public history is still public history
- real-looking or real secrets force revocation and cleanup work

### Section 3 - What we investigated

Cover the practical investigation:

- where the value entered the repo
- whether the key had active consumers
- whether post-exposure requests appeared
- which local surfaces also captured the secret

### Section 4 - What we changed

Spell out the response sequence:

- rotate/delete
- rewrite refs
- scrub local residue
- add hook and CI enforcement
- document the rule

### Section 5 - Permanent process changes

Close on process, not drama:

- obviously fake fixtures only
- default-path secret scanning
- review attention for security-shaped tests
- local tooling logs count as exposure surfaces

---

## 10) Recommended thesis paragraph

If the writing agent wants a starting point, this is the right thesis:

> We triggered a real secret incident by doing something that felt harmless: we
> used a realistic credential value in a test fixture. GitGuardian was right to
> alert on it. The lesson was not just "replace the string." It was that
> secret-shaped test data, agent transcripts, local logs, published git
> history, and missing default-path checks all belong to the same failure mode.

This is the core of the piece.

---

## 11) Process lessons worth emphasizing

These are the strongest takeaways for readers:

1. Test fixtures must be structurally impossible to confuse with real provider
   secrets.
2. Secret scanning belongs in local hooks and CI, not only in external
   monitoring.
3. Incident response must run on multiple tracks at once: revoke/rotate, clean
   published refs, and scrub local residue.
4. Reviewers should treat credential-shaped fixtures as blocking defects.
5. Agent-assisted development increases the number of places a secret can get
   copied, even when the original mistake starts in ordinary test code.

---

## 12) Final guidance for tone

The article should sound:

- direct
- technical
- unembarrassed
- specific about failure
- specific about fixes

It should not sound:

- defensive
- self-congratulatory
- anti-AI in a vague way
- broader than the evidence

The best version reads like a team that made a mistake, did the work, and
changed the process so the same class of mistake is much less likely to happen
again.
