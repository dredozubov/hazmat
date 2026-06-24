package hazmat

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

// repairAgentDirSubcommand is the hidden subcommand hazmat re-invokes as root
// (through sudo) to repair agent-owned directory ownership without ever
// following a symlink. A plain `sudo chown`/`sudo chmod` on a path under the
// agent home follows symlinks at every component, so the contained agent —
// which owns its home — can swap a component for a symlink between the
// caller's check and the privileged chown (TOCTOU) or pre-plant a parent
// symlink, redirecting the root operation onto an arbitrary target and
// escalating privilege. Routing the repair through this fd-based subcommand
// closes that vector.
const repairAgentDirSubcommand = "__repair-agent-dir"

// repairAgentDirSelfPath resolves the hazmat binary to re-invoke under sudo.
// A var so tests can stub it without shelling out.
var repairAgentDirSelfPath = func() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve hazmat binary for directory repair: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	return self, nil
}

func newRepairAgentDirCmd() *cobra.Command {
	var pathFlag, userFlag, groupFlag, modeFlag string
	cmd := &cobra.Command{
		Use:    repairAgentDirSubcommand,
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runRepairAgentDir(pathFlag, userFlag, groupFlag, modeFlag)
		},
	}
	cmd.Flags().StringVar(&pathFlag, "path", "", "absolute agent directory to repair")
	cmd.Flags().StringVar(&userFlag, "user", "", "owner user name")
	cmd.Flags().StringVar(&groupFlag, "group", "", "owner group name")
	cmd.Flags().StringVar(&modeFlag, "mode", "", "octal mode, e.g. 0755 or 2770")
	return cmd
}

// runRepairAgentDir runs as root. It re-validates the path against the agent
// home (defense in depth — the symlink safety comes from the O_NOFOLLOW walk,
// not this string check), resolves the owner, and applies it.
func runRepairAgentDir(path, userName, groupName, modeStr string) error {
	if err := validateHarnessAssetDestPath(path); err != nil {
		return err
	}
	uid, gid, err := lookupAgentRepairOwner(userName, groupName)
	if err != nil {
		return err
	}
	mode, err := parseAgentRepairMode(modeStr)
	if err != nil {
		return err
	}
	return chownChmodDirNoFollow(harnessAssetAgentHome, path, uid, gid, mode)
}

func lookupAgentRepairOwner(userName, groupName string) (int, int, error) {
	u, err := user.Lookup(userName)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup user %q: %w", userName, err)
	}
	g, err := user.LookupGroup(groupName)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup group %q: %w", groupName, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse uid for %q: %w", userName, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse gid for %q: %w", groupName, err)
	}
	return uid, gid, nil
}

func parseAgentRepairMode(modeStr string) (uint32, error) {
	v, err := strconv.ParseUint(strings.TrimSpace(modeStr), 8, 32)
	if err != nil {
		return 0, fmt.Errorf("parse mode %q: %w", modeStr, err)
	}
	return uint32(v) & 0o7777, nil
}

// chownChmodDirNoFollow applies owner and mode to a directory located strictly
// below the trusted anchor, opening each component below the anchor with
// O_NOFOLLOW so a symlink swapped in at any level (final or parent) makes the
// open fail rather than redirect the privileged chown/chmod. The anchor itself
// (the agent home) is opened with normal resolution because its intermediate
// components are root-owned system directories — on macOS /var, /tmp and /etc
// are themselves symlinks, so anchoring below them is what keeps the descent
// both safe and functional.
func chownChmodDirNoFollow(anchor, path string, uid, gid int, mode uint32) error {
	anchor = filepath.Clean(anchor)
	target := filepath.Clean(path)
	if !filepath.IsAbs(anchor) || !filepath.IsAbs(target) {
		return fmt.Errorf("repair anchor %q and path %q must be absolute", anchor, target)
	}
	rel, err := filepath.Rel(anchor, target)
	if err != nil {
		return fmt.Errorf("repair path %q relative to anchor %q: %w", target, anchor, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("refusing to repair %q outside agent home %q", target, anchor)
	}

	fd, err := unix.Open(anchor, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open repair anchor %s: %w", anchor, err)
	}
	if rel != "." {
		for _, comp := range strings.Split(rel, string(os.PathSeparator)) {
			if comp == "" || comp == "." {
				continue
			}
			next, openErr := unix.Openat(fd, comp,
				unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			_ = unix.Close(fd)
			if openErr != nil {
				return fmt.Errorf("open %q under %s without following symlinks: %w", comp, anchor, openErr)
			}
			fd = next
		}
	}
	defer unix.Close(fd)

	if err := unix.Fchown(fd, uid, gid); err != nil {
		return fmt.Errorf("set owner on %s: %w", target, err)
	}
	if err := unix.Fchmod(fd, mode); err != nil {
		return fmt.Errorf("set mode on %s: %w", target, err)
	}
	return nil
}
