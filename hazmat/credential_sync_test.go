package hazmat

import (
	"strings"
	"testing"
)

func TestCredentialSyncPreLaunchPullsPlainHostKeychainRotation(t *testing.T) {
	store := newFakeCredentialSyncEndpoint("hazmat store", newCredentialSyncMaterial("stored-refresh", 1, 10))
	host := newFakeCredentialSyncEndpoint("host keychain", newCredentialSyncMaterial("host-rotated-refresh", 2, 20))
	agent := newFakeCredentialSyncEndpoint("agent keychain", credentialSyncMaterial{})

	spec := testCredentialSyncSpec("Claude", store.endpoint(), host.endpointPtr(), agent.endpoint())
	if err := syncCredentialBeforeLaunch(spec); err != nil {
		t.Fatalf("syncCredentialBeforeLaunch: %v", err)
	}

	assertCredentialSyncSecret(t, store, "host-rotated-refresh")
	assertCredentialSyncSecret(t, host, "host-rotated-refresh")
	assertCredentialSyncSecret(t, agent, "host-rotated-refresh")
}

func TestCredentialSyncPreLaunchPublishesNewerStoreToHostKeychain(t *testing.T) {
	store := newFakeCredentialSyncEndpoint("hazmat store", newCredentialSyncMaterial("store-rotated-refresh", 3, 30))
	host := newFakeCredentialSyncEndpoint("host keychain", newCredentialSyncMaterial("old-host-refresh", 2, 20))
	agent := newFakeCredentialSyncEndpoint("agent keychain", credentialSyncMaterial{})

	spec := testCredentialSyncSpec("Claude", store.endpoint(), host.endpointPtr(), agent.endpoint())
	if err := syncCredentialBeforeLaunch(spec); err != nil {
		t.Fatalf("syncCredentialBeforeLaunch: %v", err)
	}

	assertCredentialSyncSecret(t, store, "store-rotated-refresh")
	assertCredentialSyncSecret(t, host, "store-rotated-refresh")
	assertCredentialSyncSecret(t, agent, "store-rotated-refresh")
}

func TestCredentialSyncPostExitPublishesAgentKeychainRotation(t *testing.T) {
	store := newFakeCredentialSyncEndpoint("hazmat store", newCredentialSyncMaterial("stored-refresh", 1, 10))
	host := newFakeCredentialSyncEndpoint("host keychain", newCredentialSyncMaterial("stored-refresh", 1, 10))
	agent := newFakeCredentialSyncEndpoint("agent keychain", newCredentialSyncMaterial("agent-rotated-refresh", 2, 20))

	spec := testCredentialSyncSpec("Claude", store.endpoint(), host.endpointPtr(), agent.endpoint())
	if err := syncCredentialAfterExit(spec); err != nil {
		t.Fatalf("syncCredentialAfterExit: %v", err)
	}

	assertCredentialSyncSecret(t, store, "agent-rotated-refresh")
	assertCredentialSyncSecret(t, host, "agent-rotated-refresh")
	assertCredentialSyncEmpty(t, agent)
}

func TestCredentialSyncPostExitPublishesFileBackedRotations(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stored  string
		updated string
	}{
		{name: "Codex", stored: "stored-codex-refresh", updated: "updated-codex-refresh"},
		{name: "OpenCode", stored: "stored-opencode-refresh", updated: "updated-opencode-refresh"},
		{name: "Antigravity file", stored: "stored-antigravity-refresh", updated: "updated-antigravity-refresh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeCredentialSyncEndpoint(tc.name+" auth store", newCredentialSyncMaterial(tc.stored, 1, 10))
			agentFile := newFakeCredentialSyncEndpoint("agent auth file", newCredentialSyncMaterial(tc.updated, 2, 20))

			spec := testCredentialSyncSpec(tc.name, store.endpoint(), nil, agentFile.endpoint())
			if err := syncCredentialAfterExit(spec); err != nil {
				t.Fatalf("syncCredentialAfterExit: %v", err)
			}

			assertCredentialSyncSecret(t, store, tc.updated)
			assertCredentialSyncEmpty(t, agentFile)
		})
	}
}

func TestCredentialSyncConflictRedactsSecretValues(t *testing.T) {
	store := newFakeCredentialSyncEndpoint("hazmat store", newCredentialSyncMaterial("store-secret-should-not-leak", 2, 20))
	host := newFakeCredentialSyncEndpoint("host keychain", newCredentialSyncMaterial("host-secret-should-not-leak", 2, 20))
	agent := newFakeCredentialSyncEndpoint("agent keychain", credentialSyncMaterial{})

	spec := testCredentialSyncSpec("Claude", store.endpoint(), host.endpointPtr(), agent.endpoint())
	err := syncCredentialBeforeLaunch(spec)
	if err == nil {
		t.Fatal("syncCredentialBeforeLaunch succeeded, want conflict")
	}
	msg := err.Error()
	for _, leaked := range []string{"store-secret-should-not-leak", "host-secret-should-not-leak"} {
		if strings.Contains(msg, leaked) {
			t.Fatalf("conflict error leaked secret %q: %s", leaked, msg)
		}
	}
	assertCredentialSyncSecret(t, store, "store-secret-should-not-leak")
	assertCredentialSyncSecret(t, host, "host-secret-should-not-leak")
	assertCredentialSyncEmpty(t, agent)
}

func TestCredentialSyncIgnoresInvalidAgentTokenAndCleansResidue(t *testing.T) {
	store := newFakeCredentialSyncEndpoint("hazmat store", newCredentialSyncMaterial("valid-stored-refresh", 2, 20))
	host := newFakeCredentialSyncEndpoint("host keychain", newCredentialSyncMaterial("valid-stored-refresh", 2, 20))
	agent := newFakeCredentialSyncEndpoint("agent keychain", newInvalidCredentialSyncMaterial("invalid-agent-refresh", 3, 30))

	spec := testCredentialSyncSpec("Claude", store.endpoint(), host.endpointPtr(), agent.endpoint())
	if err := syncCredentialAfterExit(spec); err != nil {
		t.Fatalf("syncCredentialAfterExit: %v", err)
	}

	assertCredentialSyncSecret(t, store, "valid-stored-refresh")
	assertCredentialSyncSecret(t, host, "valid-stored-refresh")
	assertCredentialSyncEmpty(t, agent)
}

func TestCredentialSyncAdapterRequiredFailsClosed(t *testing.T) {
	store := newFakeCredentialSyncEndpoint("hazmat store", credentialSyncMaterial{})
	host := newFakeCredentialSyncEndpoint("antigravity keychain", newCredentialSyncMaterial("antigravity-secret", 1, 10))
	agent := newFakeCredentialSyncEndpoint("agent keychain", credentialSyncMaterial{})

	spec := testCredentialSyncSpec("Antigravity Keychain", store.endpoint(), host.endpointPtr(), agent.endpoint())
	spec.support = credentialSyncAdapterRequired
	err := syncCredentialBeforeLaunch(spec)
	if err == nil || !strings.Contains(err.Error(), "requires an adapter") {
		t.Fatalf("syncCredentialBeforeLaunch error = %v, want adapter-required", err)
	}
	assertCredentialSyncEmpty(t, store)
	assertCredentialSyncSecret(t, host, "antigravity-secret")
	assertCredentialSyncEmpty(t, agent)
}

func TestCredentialSyncNoDeliveryProfileStateFailsClosed(t *testing.T) {
	store := newFakeCredentialSyncEndpoint("hazmat store", credentialSyncMaterial{})
	profile := newFakeCredentialSyncEndpoint("hermes profile", newCredentialSyncMaterial("profile-secret", 1, 10))

	spec := testCredentialSyncSpec("Hermes profile", store.endpoint(), nil, profile.endpoint())
	spec.support = credentialSyncNoDelivery
	err := syncCredentialBeforeLaunch(spec)
	if err == nil || !strings.Contains(err.Error(), "contained-only profile state") {
		t.Fatalf("syncCredentialBeforeLaunch error = %v, want no-delivery", err)
	}
	assertCredentialSyncEmpty(t, store)
	assertCredentialSyncSecret(t, profile, "profile-secret")
}

func testCredentialSyncSpec(name string, store credentialSyncEndpoint, host *credentialSyncEndpoint, sinks ...credentialSyncEndpoint) credentialSyncSpec {
	return credentialSyncSpec{
		name:       name,
		support:    credentialSyncSupported,
		store:      store,
		hostCache:  host,
		agentSinks: sinks,
	}
}

type fakeCredentialSyncEndpoint struct {
	label  string
	value  credentialSyncMaterial
	exists bool
	writes int
	clears int
}

func newFakeCredentialSyncEndpoint(label string, value credentialSyncMaterial) *fakeCredentialSyncEndpoint {
	return &fakeCredentialSyncEndpoint{
		label:  label,
		value:  value,
		exists: value.secret != nil,
	}
}

func (f *fakeCredentialSyncEndpoint) endpoint() credentialSyncEndpoint {
	return credentialSyncEndpoint{
		label: f.label,
		read: func() (credentialSyncMaterial, bool, error) {
			return f.value, f.exists, nil
		},
		write: func(value credentialSyncMaterial) error {
			f.value = value
			f.exists = true
			f.writes++
			return nil
		},
		clear: func() error {
			f.value = credentialSyncMaterial{}
			f.exists = false
			f.clears++
			return nil
		},
	}
}

func (f *fakeCredentialSyncEndpoint) endpointPtr() *credentialSyncEndpoint {
	endpoint := f.endpoint()
	return &endpoint
}

func assertCredentialSyncSecret(t *testing.T, endpoint *fakeCredentialSyncEndpoint, want string) {
	t.Helper()
	if !endpoint.exists {
		t.Fatalf("%s is absent, want %q", endpoint.label, want)
	}
	if got := string(endpoint.value.secret); got != want {
		t.Fatalf("%s secret = %q, want %q", endpoint.label, got, want)
	}
}

func assertCredentialSyncEmpty(t *testing.T, endpoint *fakeCredentialSyncEndpoint) {
	t.Helper()
	if endpoint.exists {
		t.Fatalf("%s exists with %q, want absent", endpoint.label, endpoint.value.secret)
	}
}
