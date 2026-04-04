package main

import (
	"fmt"
	"os/user"
)

var lookupAgentUser = func() (*user.User, error) {
	return user.Lookup(agentUser)
}

func requireAgentUser() (*user.User, error) {
	agentInfo, err := lookupAgentUser()
	if err != nil {
		return nil, fmt.Errorf("agent user %q not found — run 'hazmat init' first", agentUser)
	}
	return agentInfo, nil
}
