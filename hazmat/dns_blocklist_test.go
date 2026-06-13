package hazmat

import "testing"

func TestDNSBlocklistQuickModeSkipsResolverProbes(t *testing.T) {
	resolverCalls := 0
	installDNSBlocklistTestHooks(t, func(string) bool {
		resolverCalls++
		return false
	})

	ui := &UI{Quick: true}
	testDNSBlocklist(ui)

	if resolverCalls != 0 {
		t.Fatalf("resolver calls = %d, want 0 in quick mode", resolverCalls)
	}
	if ui.Fail != 0 || ui.Skip == 0 {
		t.Fatalf("ui counts pass=%d fail=%d warn=%d skip=%d, want no failures and a quick-mode skip", ui.Pass, ui.Fail, ui.Warn, ui.Skip)
	}
}

func TestDNSBlocklistFullModeRunsResolverProbes(t *testing.T) {
	resolverCalls := 0
	installDNSBlocklistTestHooks(t, func(string) bool {
		resolverCalls++
		return true
	})

	ui := &UI{Quick: false}
	testDNSBlocklist(ui)

	if resolverCalls != len(dnsBlocklistProbeDomains) {
		t.Fatalf("resolver calls = %d, want %d in full mode", resolverCalls, len(dnsBlocklistProbeDomains))
	}
	if ui.Fail != 0 || ui.Skip != 0 {
		t.Fatalf("ui counts pass=%d fail=%d warn=%d skip=%d, want resolver pass coverage without skips", ui.Pass, ui.Fail, ui.Warn, ui.Skip)
	}
}

func installDNSBlocklistTestHooks(t *testing.T, resolver func(string) bool) {
	t.Helper()
	oldReadHosts := readDNSBlocklistHosts
	oldResolver := dnsBlocklistDomainBlocked
	oldDomains := dnsBlocklistProbeDomains
	readDNSBlocklistHosts = func() ([]byte, error) {
		return []byte("# AI Agent Blocklist\n0.0.0.0 ngrok.io\n0.0.0.0 pastebin.com\n"), nil
	}
	dnsBlocklistDomainBlocked = resolver
	dnsBlocklistProbeDomains = []string{"ngrok.io", "pastebin.com"}
	t.Cleanup(func() {
		readDNSBlocklistHosts = oldReadHosts
		dnsBlocklistDomainBlocked = oldResolver
		dnsBlocklistProbeDomains = oldDomains
	})
}
