package main

import (
	"errors"
	"os/user"
	"strings"
	"testing"
)

func TestRequireAgentUser(t *testing.T) {
	originalLookup := lookupAgentUser
	t.Cleanup(func() {
		lookupAgentUser = originalLookup
	})

	lookupAgentUser = func() (*user.User, error) {
		return nil, errors.New("missing")
	}

	_, err := requireAgentUser()
	if err == nil || !strings.Contains(err.Error(), "run 'hazmat init' first") {
		t.Fatalf("err = %v, want init guidance", err)
	}

	lookupAgentUser = func() (*user.User, error) {
		return &user.User{Username: agentUser, HomeDir: agentHome}, nil
	}

	agentInfo, err := requireAgentUser()
	if err != nil {
		t.Fatalf("requireAgentUser returned error: %v", err)
	}
	if agentInfo.Username != agentUser {
		t.Fatalf("agentInfo.Username = %q, want %q", agentInfo.Username, agentUser)
	}
}
