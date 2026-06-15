//go:build !darwin

package hazmat

func currentACLHealthPathState(string) (aclHealthPathState, bool) {
	return aclHealthPathState{}, false
}
