//go:build linux && amd64

package linux

import "golang.org/x/sys/unix"

func linuxAuditArch() uint32 {
	return unix.AUDIT_ARCH_X86_64
}

func allowedLinuxSyscalls() []int {
	return append(commonAllowedLinuxSyscalls(), unix.SYS_ARCH_PRCTL)
}
