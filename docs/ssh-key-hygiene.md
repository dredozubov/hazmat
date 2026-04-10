# SSH Key Hygiene for Hazmat Users

Hazmat's SSH support is intentionally narrow: it gives a project a specific
Git-over-SSH capability, not general SSH shell access. Every key you assign
with `hazmat config ssh set ...` should be treated as delegated authority for
that project.

The safest mental model is simple:

- never give Hazmat your daily-driver personal SSH key
- one trust boundary, one key
- separate Git hosting access from server shell access
- keep host verification (`known_hosts`) scoped with the key

## The Core Rules

1. Use a dedicated key for Hazmat, not the same key you use in your own shell.
2. Split keys by blast radius, not convenience. Separate keys for:
   - personal GitHub
   - company GitHub
   - staging servers
   - production servers
   - bastions
3. Prefer read-only or narrowly-scoped credentials by default.
4. If a key must reach multiple systems, those systems should belong to the
   same trust domain.
5. Pin host keys. Hazmat reads `known_hosts` from the same directory as the
   selected private key, so the directory layout is part of the security model.

## Recommended Directory Layout

Hazmat expects the selected private key and `known_hosts` to live in the same
directory. If you want isolation between targets, create one directory per
trust domain:

```text
~/.config/hazmat/ssh/
  github-personal/
    id_ed25519
    id_ed25519.pub
    known_hosts
  github-work/
    id_ed25519
    id_ed25519.pub
    known_hosts
  prod-readonly/
    id_ed25519
    id_ed25519.pub
    known_hosts
```

Then assign the exact private key you want:

```bash
hazmat config ssh set ~/.config/hazmat/ssh/github-work/id_ed25519
hazmat config ssh test --host github.com
```

If you keep everything in `~/.ssh`, Hazmat can use that too. You just lose some
of the clarity and isolation you get from per-target directories and
per-target `known_hosts` files.

## Which Credential Model to Use

| Use case | Recommended model | Why |
| --- | --- | --- |
| One GitHub repo | Deploy key | Repo-scoped, simple, read-only by default |
| Several repos in one org | GitHub App if HTTPS is acceptable; otherwise machine user or SSH CA | Better scoping than one broad personal key |
| One SSH server | Dedicated Unix account + dedicated key | Clear blast radius |
| Fleet of servers you control | SSH CA + short-lived user certs | Strongest operational model |

## GitHub Guidance

### Personal account access

For a normal human workstation, GitHub currently recommends Ed25519 keys by
default and recommends using a passphrase. It also recommends giving keys
distinct names instead of overwriting a default key. See:

- [Generating a new SSH key and adding it to the ssh-agent](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/generating-a-new-ssh-key-and-adding-it-to-the-ssh-agent)
- [Adding a new SSH key to your GitHub account](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/adding-a-new-ssh-key-to-your-github-account)

For Hazmat, do not point a project at the same broad account key you use for
all of your interactive GitHub work unless you are comfortable delegating that
entire account's SSH authority to the project session.

### One repository: deploy key

For a single repository, a deploy key is usually the best fit:

- it is attached to one repository
- it can be read-only
- it does not inherit your full user access

GitHub also documents an important limitation: a deploy key cannot be reused
across multiple repositories. That is a feature, not a bug, for Hazmat-style
least privilege.

Source:

- [Managing deploy keys](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/managing-deploy-keys)

### Multiple repositories: machine user or GitHub App

If you need one automation identity to access several repositories:

- prefer a GitHub App if HTTPS-based access is acceptable
- otherwise use a machine user with only the repositories or teams it needs

GitHub's own deploy-key guidance explicitly allows a single machine user for
automation, but it also warns that the access is broader and that personal
repositories cannot make collaborators read-only.

Source:

- [Managing deploy keys](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/managing-deploy-keys)

### GitHub Enterprise Cloud: SSH certificates

If you control an Enterprise Cloud organization, SSH certificates are a strong
fit for Hazmat-style targeted access. Instead of uploading many long-lived
public keys, you trust one SSH CA and issue short-lived user certificates.

That is the best answer on GitHub to "one root of trust, many targeted
credentials." It is better than reusing one static SSH key everywhere.

Source:

- [Managing Git access to your organization's repositories](https://docs.github.com/en/enterprise-cloud@latest/organizations/managing-git-access-to-your-organizations-repositories)

### Pin GitHub host keys

GitHub publishes its SSH host key fingerprints and current `known_hosts`
entries. Use those published values when building a dedicated `known_hosts`
file for a GitHub-specific Hazmat key directory.

Source:

- [GitHub's SSH key fingerprints](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/githubs-ssh-key-fingerprints)

## Remote Server Guidance

### If you do not control the server

Use a separate key per server or per environment. Do not reuse the same key
across unrelated vendors or between staging and production.

If the remote side allows restrictions, ask for:

- a dedicated Unix account
- no sudo unless strictly required
- no shared secrets in that account
- `authorized_keys` restrictions such as `no-agent-forwarding`,
  `no-port-forwarding`, and `no-pty`
- `ForceCommand` or an equivalent forced command when shell access is not
  actually needed

OpenSSH documents these `authorized_keys` restrictions in `sshd(8)`.

Source:

- [sshd(8)](https://man.openbsd.org/sshd)

### If you control the server or fleet

If you control the fleet, avoid managing a growing pile of long-lived per-host
static keys. Use an SSH CA:

- keep the CA key protected and out of normal developer workflows
- issue short-lived user certificates
- use principals to separate roles such as `git`, `deploy`, `readonly`, or
  `breakglass`
- let servers trust the CA instead of listing every user key individually

OpenSSH certificate authentication is the standard supported pattern for this.

Source:

- [ssh-keygen(1)](https://man.openbsd.org/ssh-keygen)
- [ssh(1)](https://man.openbsd.org/ssh)

## Can I Use One Master Key to Produce More Targeted Keys?

Not in the usual "hierarchical deterministic child SSH private keys" sense.
Standard SSH does not have a mainstream, interoperable "wallet-style" model
where you keep one software master key and derive all child SSH keys from it.

The supported equivalents are:

1. SSH CA signing.
   - This is the best "master key" pattern for servers you control.
   - The master key signs leaf certificates; it does not derive child private
     keys.
2. Hardware security keys.
   - GitHub supports generating SSH keys on hardware security keys such as
     `ed25519-sk`.
   - OpenSSH also supports resident keys and host/domain-specific FIDO
     application strings.
   - This gives you one physical root of trust that can back multiple separate
     SSH credentials, but they are still separate credentials, not one derived
     software tree.

OpenSSH notes that FIDO application strings can be used to generate
host-specific or domain-specific resident keys, which is useful if you want one
authenticator and multiple distinct SSH identities.

Sources:

- [Generating a new SSH key and adding it to the ssh-agent](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/generating-a-new-ssh-key-and-adding-it-to-the-ssh-agent)
- [ssh-keygen(1)](https://man.openbsd.org/ssh-keygen)

## Hazmat-Specific Recommendations

If you want a short version, use this order:

1. GitHub single repo: deploy key in its own directory with its own
   `known_hosts`
2. GitHub multi-repo automation: GitHub App if possible; otherwise machine user
   or Enterprise Cloud SSH CA
3. Remote servers you control: SSH CA and short-lived certs
4. Remote servers you do not control: one dedicated key per environment or per
   account

Avoid these patterns:

- assigning your personal all-purpose `~/.ssh/id_ed25519` to Hazmat
- reusing one production-capable key across unrelated projects
- sharing one `known_hosts` file across unrelated trust domains if you want
  strong separation
- treating `hazmat config ssh test` success as proof that arbitrary SSH shell
  access is available inside a session

## Example Setup Recipes

### GitHub repo with a deploy key

```bash
mkdir -p ~/.config/hazmat/ssh/my-repo
ssh-keygen -t ed25519 -f ~/.config/hazmat/ssh/my-repo/id_ed25519 -C "hazmat my-repo"

# Add the public key as a deploy key in GitHub, then pin GitHub host keys
# into ~/.config/hazmat/ssh/my-repo/known_hosts.

hazmat config ssh set ~/.config/hazmat/ssh/my-repo/id_ed25519
hazmat config ssh test --host github.com
```

### One remote server

```bash
mkdir -p ~/.config/hazmat/ssh/staging-box
ssh-keygen -t ed25519 -f ~/.config/hazmat/ssh/staging-box/id_ed25519 -C "hazmat staging-box"

# Install the public key on the server account and pin that server's host key
# into ~/.config/hazmat/ssh/staging-box/known_hosts.

hazmat config ssh set ~/.config/hazmat/ssh/staging-box/id_ed25519
hazmat config ssh test --host staging-box
```

The key point is the same in both cases: one key directory, one trust domain,
one clear story for `known_hosts`.
