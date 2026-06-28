//go:build linux && (amd64 || arm64)

package linux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"

	"hazmat/containment"
	linuxspec "hazmat/containment/linux"
)

func HostCurrentUserEnforcer() CurrentUserEnforcer {
	return hostCurrentUserEnforcer{}
}

type hostCurrentUserEnforcer struct{}

func (hostCurrentUserEnforcer) CloseInheritedFDs(_ context.Context, plan FDClosurePlan) error {
	preserve := make(map[int]bool, len(plan.Preserve))
	for _, fd := range plan.Preserve {
		preserve[fd] = true
	}
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return fmt.Errorf("list inherited fds: %w", err)
	}
	for _, entry := range entries {
		fd, err := strconv.Atoi(entry.Name())
		if err != nil || fd < plan.CloseMin || preserve[fd] {
			continue
		}
		if err := unix.Close(fd); err != nil && !errors.Is(err, unix.EBADF) {
			return fmt.Errorf("close inherited fd %d: %w", fd, err)
		}
	}
	return nil
}

func (hostCurrentUserEnforcer) CreateNamespaces(_ context.Context, plan NamespacePlan) error {
	if !plan.User || !plan.Mount {
		return fmt.Errorf("linux current-user enforcer requires user and mount namespaces")
	}
	hostUID, hostGID := os.Getuid(), os.Getgid()
	if err := unix.Unshare(unix.CLONE_NEWUSER); err != nil {
		return fmt.Errorf("unshare user namespace: %w", err)
	}
	if err := writeUserNamespaceMaps(hostUID, hostGID); err != nil {
		return err
	}
	if err := unix.Setresgid(0, 0, 0); err != nil {
		return fmt.Errorf("enter mapped namespace gid: %w", err)
	}
	if err := unix.Setresuid(0, 0, 0); err != nil {
		return fmt.Errorf("enter mapped namespace uid: %w", err)
	}

	flags := unix.CLONE_NEWNS
	if plan.Network {
		flags |= unix.CLONE_NEWNET
	}
	if err := unix.Unshare(flags); err != nil {
		return fmt.Errorf("unshare mount/network namespaces: %w", err)
	}
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("mark mount namespace private: %w", err)
	}
	return nil
}

func (hostCurrentUserEnforcer) ApplyMounts(_ context.Context, spec linuxspec.LaunchSpec) error {
	if spec.Temp.Path == "" || !filepath.IsAbs(spec.Temp.Path) {
		return fmt.Errorf("linux current-user temp path must be absolute")
	}
	if spec.AgentHome.Path == "" || !filepath.IsAbs(spec.AgentHome.Path) {
		return fmt.Errorf("linux current-user agent home path must be absolute")
	}
	if !isPathWithin(spec.Temp.Path, spec.AgentHome.Path) {
		return fmt.Errorf("linux current-user agent home %q must be under session temp %q", spec.AgentHome.Path, spec.Temp.Path)
	}
	if err := os.MkdirAll(spec.Temp.Path, 0o700); err != nil {
		return fmt.Errorf("create session temp mountpoint: %w", err)
	}
	if spec.Temp.Tmpfs {
		if err := unix.Mount("tmpfs", spec.Temp.Path, "tmpfs", unix.MS_NODEV|unix.MS_NOSUID, "mode=700"); err != nil {
			return fmt.Errorf("mount session tmpfs: %w", err)
		}
	}
	if err := os.MkdirAll(spec.AgentHome.Path, 0o700); err != nil {
		return fmt.Errorf("create session home: %w", err)
	}
	for _, mount := range spec.Mounts {
		if err := applyBindMount(mount); err != nil {
			return err
		}
	}
	return nil
}

func (hostCurrentUserEnforcer) ConfigureNetwork(_ context.Context, network NetworkAdmission) error {
	if network.Mode == "" {
		return fmt.Errorf("linux current-user network mode is required")
	}
	// A fresh network namespace has no external route. Loopback setup can be
	// added after VM smokes define the required local-service semantics.
	return nil
}

func (hostCurrentUserEnforcer) DropPrivileges(_ context.Context, process linuxspec.ProcessSpec) error {
	if !process.NoNewPrivs || !process.DropCapabilities {
		return fmt.Errorf("linux current-user process policy requires no_new_privs and capability drop")
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("set no_new_privs: %w", err)
	}
	if err := dropLinuxCapabilities(); err != nil {
		return err
	}
	return nil
}

func (hostCurrentUserEnforcer) ApplyLandlock(_ context.Context, plan PolicyPlan) error {
	if !plan.Landlock.Enforced {
		return fmt.Errorf("linux current-user requires enforced Landlock plan")
	}
	return applyLandlock(plan.Landlock.Rules)
}

func (hostCurrentUserEnforcer) ApplySeccomp(_ context.Context, plan PolicyPlan) error {
	if !plan.Seccomp.NoNewPrivs || plan.Seccomp.DefaultAction != "errno" {
		return fmt.Errorf("linux current-user requires no_new_privs seccomp errno profile")
	}
	return applySeccompErrnoProfile()
}

func (hostCurrentUserEnforcer) Exec(ctx context.Context, spec linuxspec.LaunchSpec, opts RunOptions) (ExecResult, error) {
	if len(spec.Command) == 0 {
		return ExecResult{}, fmt.Errorf("linux current-user exec requires command argv")
	}
	cmd := exec.CommandContext(ctx, spec.Command[0], spec.Command[1:]...)
	cmd.Dir = linuxWorkdir(spec)
	cmd.Env = linuxSessionEnv(spec)
	cmd.Stdin = opts.Stdin
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	err := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ExecResult{}, ctxErr
	}
	if err == nil {
		return ExecResult{ExitCode: cmd.ProcessState.ExitCode()}, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return ExecResult{ExitCode: exitErr.ExitCode()}, nil
	}
	return ExecResult{}, err
}

func NewCommandAgentUserRootHelper(path string) (AgentUserRootHelper, error) {
	if err := validateAgentUserRootHelperPath(path); err != nil {
		return nil, err
	}
	return commandAgentUserRootHelper{Path: path}, nil
}

type commandAgentUserRootHelper struct {
	Path string
}

func (h commandAgentUserRootHelper) Execute(ctx context.Context, request AgentUserHelperRequest, opts RunOptions) (ExecResult, error) {
	cmd := exec.CommandContext(ctx, h.Path,
		"run-agent",
		"--spec", request.SpecPath,
		"--spec-sha256", request.SpecSHA256,
		"--nonce", request.SpecNonce,
		"--metadata", request.MetadataPath,
	)
	cmd.Stdin = opts.Stdin
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	err := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ExecResult{}, ctxErr
	}
	if err == nil {
		return ExecResult{ExitCode: cmd.ProcessState.ExitCode()}, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return ExecResult{ExitCode: exitErr.ExitCode()}, nil
	}
	return ExecResult{}, err
}

func writeUserNamespaceMaps(hostUID, hostGID int) error {
	_ = os.WriteFile("/proc/self/setgroups", []byte("deny\n"), 0o600)
	if err := os.WriteFile("/proc/self/uid_map", []byte(fmt.Sprintf("0 %d 1\n", hostUID)), 0o600); err != nil {
		return fmt.Errorf("write uid_map: %w", err)
	}
	if err := os.WriteFile("/proc/self/gid_map", []byte(fmt.Sprintf("0 %d 1\n", hostGID)), 0o600); err != nil {
		return fmt.Errorf("write gid_map: %w", err)
	}
	return nil
}

func applyBindMount(mount linuxspec.BindMount) error {
	if mount.Source == "" || mount.Target == "" || !filepath.IsAbs(mount.Source) || !filepath.IsAbs(mount.Target) {
		return fmt.Errorf("linux bind mount requires absolute source and target: %+v", mount)
	}
	if err := unix.Mount(mount.Source, mount.Target, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("bind mount %s -> %s: %w", mount.Source, mount.Target, err)
	}
	if mount.Access == containment.PathReadOnly {
		if err := unix.Mount("", mount.Target, "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY|unix.MS_REC, ""); err != nil {
			return fmt.Errorf("remount read-only %s: %w", mount.Target, err)
		}
	}
	return nil
}

func dropLinuxCapabilities() error {
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	data := [2]unix.CapUserData{}
	if err := unix.Capset(&header, &data[0]); err != nil {
		return fmt.Errorf("clear capability sets: %w", err)
	}
	for capID := uintptr(0); capID <= unix.CAP_LAST_CAP; capID++ {
		if err := unix.Prctl(unix.PR_CAPBSET_DROP, capID, 0, 0, 0); err != nil && !errors.Is(err, unix.EPERM) {
			return fmt.Errorf("drop capability bounding bit %d: %w", capID, err)
		}
	}
	return nil
}

func applyLandlock(rules []LandlockRule) error {
	ruleset := unix.LandlockRulesetAttr{Access_fs: landlockAllFSAccess()}
	fd, _, errno := unix.Syscall(
		unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&ruleset)),
		unsafe.Sizeof(ruleset),
		0,
	)
	if errno != 0 {
		return fmt.Errorf("create Landlock ruleset: %w", errno)
	}
	defer unix.Close(int(fd))
	for _, rule := range append(systemLandlockRules(), rules...) {
		if err := addLandlockRule(int(fd), rule); err != nil {
			return err
		}
	}
	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0); errno != 0 {
		return fmt.Errorf("restrict self with Landlock: %w", errno)
	}
	return nil
}

func addLandlockRule(rulesetFD int, rule LandlockRule) error {
	pathFD, err := unix.Open(rule.Path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("open Landlock path %s: %w", rule.Path, err)
	}
	defer unix.Close(pathFD)
	attr := unix.LandlockPathBeneathAttr{
		Allowed_access: landlockAccess(rule.Access),
		Parent_fd:      int32(pathFD),
	}
	if _, _, errno := unix.Syscall(
		unix.SYS_LANDLOCK_ADD_RULE,
		uintptr(rulesetFD),
		uintptr(unix.LANDLOCK_RULE_PATH_BENEATH),
		uintptr(unsafe.Pointer(&attr)),
	); errno != 0 {
		return fmt.Errorf("add Landlock rule for %s: %w", rule.Path, errno)
	}
	return nil
}

func landlockAllFSAccess() uint64 {
	return uint64(unix.LANDLOCK_ACCESS_FS_EXECUTE |
		unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
		unix.LANDLOCK_ACCESS_FS_READ_FILE |
		unix.LANDLOCK_ACCESS_FS_READ_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_FILE |
		unix.LANDLOCK_ACCESS_FS_MAKE_CHAR |
		unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
		unix.LANDLOCK_ACCESS_FS_MAKE_REG |
		unix.LANDLOCK_ACCESS_FS_MAKE_SOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
		unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_SYM |
		unix.LANDLOCK_ACCESS_FS_REFER |
		unix.LANDLOCK_ACCESS_FS_TRUNCATE)
}

func landlockAccess(access containment.PathAccess) uint64 {
	read := uint64(unix.LANDLOCK_ACCESS_FS_EXECUTE |
		unix.LANDLOCK_ACCESS_FS_READ_FILE |
		unix.LANDLOCK_ACCESS_FS_READ_DIR)
	if access == containment.PathReadOnly {
		return read
	}
	return read |
		uint64(unix.LANDLOCK_ACCESS_FS_WRITE_FILE|
			unix.LANDLOCK_ACCESS_FS_REMOVE_DIR|
			unix.LANDLOCK_ACCESS_FS_REMOVE_FILE|
			unix.LANDLOCK_ACCESS_FS_MAKE_CHAR|
			unix.LANDLOCK_ACCESS_FS_MAKE_DIR|
			unix.LANDLOCK_ACCESS_FS_MAKE_REG|
			unix.LANDLOCK_ACCESS_FS_MAKE_SOCK|
			unix.LANDLOCK_ACCESS_FS_MAKE_FIFO|
			unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK|
			unix.LANDLOCK_ACCESS_FS_MAKE_SYM|
			unix.LANDLOCK_ACCESS_FS_REFER|
			unix.LANDLOCK_ACCESS_FS_TRUNCATE)
}

func systemLandlockRules() []LandlockRule {
	readOnly := []string{
		"/bin",
		"/lib",
		"/lib64",
		"/usr",
		"/etc/alternatives",
		"/etc/ca-certificates",
		"/etc/group",
		"/etc/hosts",
		"/etc/ld.so.cache",
		"/etc/ld.so.conf",
		"/etc/nsswitch.conf",
		"/etc/passwd",
		"/etc/resolv.conf",
		"/etc/ssl",
		"/nix/store",
	}
	rules := make([]LandlockRule, 0, len(readOnly))
	for _, path := range readOnly {
		rules = append(rules, LandlockRule{Path: path, Access: containment.PathReadOnly, Source: "linux_system"})
	}
	return rules
}

func applySeccompErrnoProfile() error {
	filter := seccompErrnoFilter()
	prog := unix.SockFprog{
		Len:    uint16(len(filter)),
		Filter: &filter[0],
	}
	if err := unix.Prctl(unix.PR_SET_SECCOMP, uintptr(unix.SECCOMP_MODE_FILTER), uintptr(unsafe.Pointer(&prog)), 0, 0); err != nil {
		return fmt.Errorf("apply seccomp errno filter: %w", err)
	}
	return nil
}

func seccompErrnoFilter() []unix.SockFilter {
	const seccompDataArchOffset = 4
	const seccompDataSyscallOffset = 0
	filter := []unix.SockFilter{
		bpfStmt(unix.BPF_LD|unix.BPF_W|unix.BPF_ABS, seccompDataArchOffset),
		bpfJump(unix.BPF_JMP|unix.BPF_JEQ|unix.BPF_K, linuxAuditArch(), 1, 0),
		bpfStmt(unix.BPF_RET|unix.BPF_K, unix.SECCOMP_RET_KILL_PROCESS),
		bpfStmt(unix.BPF_LD|unix.BPF_W|unix.BPF_ABS, seccompDataSyscallOffset),
	}
	for _, nr := range allowedLinuxSyscalls() {
		filter = append(filter,
			bpfJump(unix.BPF_JMP|unix.BPF_JEQ|unix.BPF_K, uint32(nr), 0, 1),
			bpfStmt(unix.BPF_RET|unix.BPF_K, unix.SECCOMP_RET_ALLOW),
		)
	}
	filter = append(filter, bpfStmt(unix.BPF_RET|unix.BPF_K, unix.SECCOMP_RET_ERRNO|uint32(unix.EPERM)))
	return filter
}

func bpfStmt(code uint16, k uint32) unix.SockFilter {
	return unix.SockFilter{Code: code, K: k}
}

func bpfJump(code uint16, k uint32, jt, jf uint8) unix.SockFilter {
	return unix.SockFilter{Code: code, K: k, Jt: jt, Jf: jf}
}

func linuxWorkdir(spec linuxspec.LaunchSpec) string {
	if len(spec.Mounts) > 0 && spec.Mounts[0].Target != "" {
		return spec.Mounts[0].Target
	}
	return "/"
}

func linuxSessionEnv(spec linuxspec.LaunchSpec) []string {
	return []string{
		"HOME=" + spec.AgentHome.Path,
		"TMPDIR=" + spec.Temp.Path,
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"LANG=C.UTF-8",
	}
}

func isPathWithin(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	return child == parent || strings.HasPrefix(child, parent+string(filepath.Separator))
}
