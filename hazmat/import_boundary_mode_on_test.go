//go:build beadpost_hostbroker

package hazmat

// hostbrokerEnabled reports whether this test binary was built with Beadpost
// host-broker support; it drives the import-boundary mode (tagged build may
// depend on local/beadpost-contracts).
const hostbrokerEnabled = true
