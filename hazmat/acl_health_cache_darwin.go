//go:build darwin

package hazmat

import (
	"os"
	"syscall"
)

func currentACLHealthPathState(path string) (aclHealthPathState, bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return aclHealthPathState{}, false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return aclHealthPathState{}, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return aclHealthPathState{}, false
	}
	return aclHealthPathState{
		Device:    uint64(st.Dev),
		Inode:     st.Ino,
		Mode:      uint32(info.Mode().Perm()),
		UID:       st.Uid,
		GID:       st.Gid,
		CTimeSec:  st.Ctimespec.Sec,
		CTimeNsec: st.Ctimespec.Nsec,
	}, true
}
