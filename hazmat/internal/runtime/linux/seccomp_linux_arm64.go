//go:build linux && arm64

package linux

import "golang.org/x/sys/unix"

func linuxAuditArch() uint32 {
	return unix.AUDIT_ARCH_AARCH64
}

func allowedLinuxSyscalls() []int {
	return commonAllowedLinuxSyscalls()
}
