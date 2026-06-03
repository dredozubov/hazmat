package hazmat

import (
	"sort"
	"strings"
)

type sessionCredentialEnvGrant struct {
	EnvVar          string
	CredentialID    credentialID
	Source          string
	ConsumerHarness HarnessID
}

func appendSessionCredentialEnvGrant(grants []sessionCredentialEnvGrant, grant sessionCredentialEnvGrant) []sessionCredentialEnvGrant {
	if grant.EnvVar == "" {
		return grants
	}
	for _, existing := range grants {
		if existing.EnvVar == grant.EnvVar && existing.CredentialID == grant.CredentialID && existing.ConsumerHarness == grant.ConsumerHarness {
			return grants
		}
	}
	return append(grants, grant)
}

func normalizedSessionCredentialEnvGrants(grants []sessionCredentialEnvGrant) []sessionCredentialEnvGrant {
	if len(grants) == 0 {
		return nil
	}
	out := make([]sessionCredentialEnvGrant, len(grants))
	copy(out, grants)
	sort.Slice(out, func(i, j int) bool {
		if out[i].EnvVar == out[j].EnvVar {
			if out[i].CredentialID == out[j].CredentialID {
				return out[i].ConsumerHarness < out[j].ConsumerHarness
			}
			return out[i].CredentialID < out[j].CredentialID
		}
		return out[i].EnvVar < out[j].EnvVar
	})
	return out
}

func sessionCredentialEnvGrantLabels(grants []sessionCredentialEnvGrant) []string {
	normalized := normalizedSessionCredentialEnvGrants(grants)
	labels := make([]string, 0, len(normalized))
	for _, grant := range normalized {
		label := grant.EnvVar + "=<redacted>"
		var details []string
		if grant.CredentialID != "" {
			details = append(details, string(grant.CredentialID))
		}
		if grant.Source != "" {
			details = append(details, grant.Source)
		}
		if grant.ConsumerHarness != "" {
			details = append(details, "consumer="+string(grant.ConsumerHarness))
		}
		if len(details) > 0 {
			label += " (" + strings.Join(details, ", ") + ")"
		}
		labels = append(labels, label)
	}
	return labels
}
