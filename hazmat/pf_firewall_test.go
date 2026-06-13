package hazmat

import "testing"

func TestPortInAnchorAcceptsManagedAnchorSourceLists(t *testing.T) {
	rules := `
block return out quick proto tcp from any to any port { 25, 465, 587 } user agent
block return out quick proto tcp from any to any port { 6660, 6661, 6662, 6663, 6664, 6665, 6666, 6667, 6668, 6669, 6697 } user agent
block return out quick proto tcp from any to any port { 20, 21 } user agent
block return out quick proto tcp from any to any port { 9050, 9150 } user agent
`
	for _, port := range []string{"25", "6667", "21", "9050"} {
		if !portInAnchor(rules, port) {
			t.Fatalf("portInAnchor(..., %q) = false, want true for managed anchor source list", port)
		}
	}
}

func TestPortInAnchorAcceptsPfctlNormalizedRules(t *testing.T) {
	rules := `
block return out quick proto tcp from any to any port = 25 user = 599
block return out quick proto tcp from any to any port = { 20, 21 } user = 599
`
	for _, port := range []string{"25", "21"} {
		if !portInAnchor(rules, port) {
			t.Fatalf("portInAnchor(..., %q) = false, want true for pfctl output", port)
		}
	}
}

func TestPortInAnchorUsesTokenBoundaries(t *testing.T) {
	rules := `block return out quick proto tcp from any to any port { 250, 1250 } user agent`
	if portInAnchor(rules, "25") {
		t.Fatal("portInAnchor matched port 25 inside 250/1250")
	}
}
