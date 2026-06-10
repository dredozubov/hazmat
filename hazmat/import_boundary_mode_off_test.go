//go:build !beadpost_hostbroker

package hazmat

// hostbrokerEnabled reports whether this test binary was built with Beadpost
// host-broker support; the default build must not depend on
// local/beadpost-contracts.
const hostbrokerEnabled = false
