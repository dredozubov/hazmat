package main

import (
	"os/exec"
	"strconv"
	"strings"
)

// ACLKind distinguishes allow from deny entries. Hazmat only emits allow
// grants; deny appears only on parsed rows so they can be filtered out.
type ACLKind int

const (
	ACLAllow ACLKind = iota
	ACLDeny
)

func (k ACLKind) String() string {
	if k == ACLDeny {
		return "deny"
	}
	return "allow"
}

// ACLGrant is an allow entry that hazmat emits via `chmod +a` or removes
// via `chmod -a`. Perms use the chmod-input vocabulary (read, write,
// execute, append, ...); macOS renders these differently for files and
// directories, which ACLRow.Satisfies + GrantsPerm handle via aclPermAliases.
type ACLGrant struct {
	Principal string
	Perms     []string
	Inherit   bool
}

// String renders the grant in the exact format accepted by `chmod +a` and
// `chmod -a`. Inheritance flags are appended only when Inherit is true —
// files cannot carry inherit flags and the kernel rejects them.
func (g ACLGrant) String() string {
	perms := append([]string{}, g.Perms...)
	if g.Inherit {
		perms = append(perms, "file_inherit", "directory_inherit")
	}
	return g.Principal + " allow " + strings.Join(perms, ",")
}

// agentTraverseGrant allows the agent user to traverse directories on the
// path from / to the project tree. readattr/readextattr/readsecurity are
// included so agent-side tooling can stat the directories it walks; no
// read or write access to contents is granted. Applied to $HOME and
// launch-helper ancestors.
var agentTraverseGrant = ACLGrant{
	Principal: "user:" + agentUser,
	Perms:     []string{"execute", "readattr", "readextattr", "readsecurity"},
	Inherit:   false,
}

// devGroupInheritableGrant is the collaborative ACL applied to the project
// root so existing and future content is read-write for both the host user
// and the agent user. Inherit flags propagate the ACL to new files and
// subdirectories created under the root.
var devGroupInheritableGrant = ACLGrant{
	Principal: "group:" + sharedGroup,
	Perms: []string{
		"read", "write", "execute", "append", "delete", "delete_child",
		"readattr", "writeattr", "readextattr", "writeextattr", "readsecurity",
	},
	Inherit: true,
}

// devGroupGrant is devGroupInheritableGrant without inheritance flags.
// Applied to existing files (which cannot carry inherit flags) during the
// initial project ACL walk so the agent can modify files that pre-date
// the inheritable grant on the parent directory.
var devGroupGrant = ACLGrant{
	Principal: devGroupInheritableGrant.Principal,
	Perms:     devGroupInheritableGrant.Perms,
	Inherit:   false,
}

// ACLRow is a parsed `ls -leOd` entry. Perms reflect the ls rendering,
// which differs from the chmod-input dialect in ACLGrant: macOS normalizes
// execute→search and splits read/write into directory-specific verbs on
// directory ACLs. Row predicates (Satisfies, GrantsPerm) bridge the two
// dialects via aclPermAliases.
type ACLRow struct {
	Index     int
	Principal string
	Kind      ACLKind
	Perms     []string
	Inherit   bool // file_inherit AND directory_inherit both present
}

// parseACLRow parses a single line of `ls -leOd` output. Returns ok=false
// for header rows and blank lines. ACL rows look like:
//
//	" 0: group:dev allow list,add_file,search,file_inherit,directory_inherit"
//	" 1: user:agent inherited allow search,readattr"
//
// The optional "inherited" token between principal and kind marks entries
// propagated from a parent directory; it is informational and does not
// affect our matching.
func parseACLRow(line string) (ACLRow, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	colon := strings.IndexByte(trimmed, ':')
	if colon <= 0 {
		return ACLRow{}, false
	}
	idx, err := strconv.Atoi(trimmed[:colon])
	if err != nil {
		return ACLRow{}, false
	}
	fields := strings.Fields(trimmed[colon+1:])
	if len(fields) < 3 {
		return ACLRow{}, false
	}

	row := ACLRow{Index: idx, Principal: fields[0]}
	kindIdx := 1
	if fields[1] == "inherited" {
		kindIdx = 2
	}
	if kindIdx+1 >= len(fields) {
		return ACLRow{}, false
	}
	switch fields[kindIdx] {
	case "allow":
		row.Kind = ACLAllow
	case "deny":
		row.Kind = ACLDeny
	default:
		return ACLRow{}, false
	}

	row.Perms = strings.Split(fields[kindIdx+1], ",")
	hasFileInherit := false
	hasDirInherit := false
	for _, p := range row.Perms {
		if p == "file_inherit" {
			hasFileInherit = true
		} else if p == "directory_inherit" {
			hasDirInherit = true
		}
	}
	row.Inherit = hasFileInherit && hasDirInherit
	return row, true
}

// Satisfies reports whether this row represents an allow grant for the
// same principal with compatible inherit flags. It does not verify the
// exact perm set — macOS ACL entries carry kernel-added standard perms,
// so exact perm matching is fragile. Callers that need to verify specific
// permissions (e.g. traverse) layer GrantsPerm on top of Satisfies.
func (r ACLRow) Satisfies(g ACLGrant) bool {
	if r.Kind != ACLAllow {
		return false
	}
	if r.Principal != g.Principal {
		return false
	}
	if g.Inherit && !r.Inherit {
		return false
	}
	return true
}

// GrantsPerm reports whether this row grants the given chmod-input
// permission, accounting for macOS's directory ACL normalization via
// aclPermAliases.
func (r ACLRow) GrantsPerm(chmodInputPerm string) bool {
	for _, alias := range aclPermAliases(chmodInputPerm) {
		for _, rowPerm := range r.Perms {
			if rowPerm == alias {
				return true
			}
		}
	}
	return false
}

// aclPermAliases maps a chmod-input permission token to the set of
// `ls -leOd` output tokens that satisfy it. This is the single reviewable
// place for macOS's directory ACL normalization:
//
//   - execute → search on directories; unchanged on files
//   - read → list on directories; unchanged on files
//   - write → add_file + add_subdirectory on directories (either alone
//     is sufficient evidence that the write grant was stored)
//   - append → add_subdirectory on directories
//   - attribute and security perms keep their token names across both
//
// Any row token matching any alias for the queried chmod-input perm
// satisfies the query. This is intentionally lenient: macOS stores
// whichever directory-normalized tokens the kernel chose at `chmod +a`
// time, and the exact expansion is an implementation detail we should
// not re-derive here.
func aclPermAliases(chmodInputPerm string) []string {
	switch chmodInputPerm {
	case "read":
		return []string{"read", "list"}
	case "write":
		return []string{"write", "add_file", "add_subdirectory"}
	case "execute":
		return []string{"execute", "search"}
	case "append":
		return []string{"append", "add_subdirectory"}
	default:
		return []string{chmodInputPerm}
	}
}

// readACLs parses `ls -leOd` output for path and returns the ACL rows.
// -d keeps directory arguments referring to the directory itself rather
// than its contents. -O surfaces the "inherited" flag on propagated rows.
func readACLs(path string) ([]ACLRow, error) {
	out, err := exec.Command("ls", "-leOd", path).CombinedOutput()
	if err != nil {
		return nil, err
	}
	var rows []ACLRow
	for _, line := range strings.Split(string(out), "\n") {
		if row, ok := parseACLRow(line); ok {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// hasACLSatisfying reports whether any row at path satisfies grant.
// Read errors (path missing, no ACL block, permission denied) return
// false — callers treat "cannot determine" as "not present" and let
// ensureACL re-apply idempotently.
func hasACLSatisfying(path string, grant ACLGrant) bool {
	rows, err := readACLs(path)
	if err != nil {
		return false
	}
	for _, r := range rows {
		if r.Satisfies(grant) {
			return true
		}
	}
	return false
}

// aclInvoker executes chmod for ACL modifications. Distinct implementations
// encode the privilege context at the type level: directACLInvoker for paths
// the calling user owns (no sudo needed), and sudoACLInvoker for root-owned
// paths (home directory, launch-helper parents). Callers pass the invoker
// matching their context — there is no sentinel-nil "maybe sudo" path.
type aclInvoker interface {
	Chmod(args ...string) error
}

// directACLInvoker runs chmod as the calling user. Use when the calling
// user owns the target path — the file owner can modify their own ACLs
// without elevation.
type directACLInvoker struct{}

func (directACLInvoker) Chmod(args ...string) error {
	return exec.Command("chmod", args...).Run()
}

// sudoACLInvoker runs chmod as root via the Runner's sudo wrapper. Use
// when the target path is root-owned (home directory traverse ACL,
// launch-helper ancestor directories under /usr/local).
type sudoACLInvoker struct {
	runner *Runner
	reason string
}

func (s sudoACLInvoker) Chmod(args ...string) error {
	return s.runner.Sudo(s.reason, append([]string{"chmod"}, args...)...)
}

// ensureACL adds grant to path via chmod +a if no existing row at path
// satisfies it. Idempotent by construction.
func ensureACL(inv aclInvoker, path string, grant ACLGrant) error {
	if hasACLSatisfying(path, grant) {
		return nil
	}
	return inv.Chmod("+a", grant.String(), path)
}

// removeACL removes grant from path via chmod -a if a matching row
// exists. No-op when the ACL is already absent.
func removeACL(inv aclInvoker, path string, grant ACLGrant) error {
	if !hasACLSatisfying(path, grant) {
		return nil
	}
	return inv.Chmod("-a", grant.String(), path)
}
