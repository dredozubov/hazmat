package linux

import (
	"fmt"
	"path"
	"strings"

	"hazmat/containment"
	linuxspec "hazmat/containment/linux"
)

type LandlockRule struct {
	Path   string                 `json:"path"`
	Access containment.PathAccess `json:"access"`
	Source string                 `json:"source"`
}

type LandlockPlan struct {
	Enforced bool           `json:"enforced"`
	Rules    []LandlockRule `json:"rules"`
}

type SeccompPlan struct {
	NoNewPrivs    bool   `json:"no_new_privs"`
	DefaultAction string `json:"default_action"`
	AllowFork     bool   `json:"allow_fork"`
}

type PolicyPlan struct {
	Landlock LandlockPlan `json:"landlock"`
	Seccomp  SeccompPlan  `json:"seccomp"`
}

func BuildPolicyPlan(spec linuxspec.LaunchSpec) (PolicyPlan, error) {
	if err := validateCurrentUserSpec(spec); err != nil {
		return PolicyPlan{}, err
	}
	if !spec.Process.NoNewPrivs {
		return PolicyPlan{}, fmt.Errorf("linux current-user policy requires no_new_privs")
	}
	rules := landlockRules(spec)
	if err := rejectCredentialDenyOverlap(rules, spec.CredentialDenies); err != nil {
		return PolicyPlan{}, err
	}
	return PolicyPlan{
		Landlock: LandlockPlan{
			Enforced: true,
			Rules:    rules,
		},
		Seccomp: SeccompPlan{
			NoNewPrivs:    true,
			DefaultAction: "errno",
			AllowFork:     spec.Process.AllowFork,
		},
	}, nil
}

func landlockRules(spec linuxspec.LaunchSpec) []LandlockRule {
	rules := make([]LandlockRule, 0, len(spec.Mounts)+2)
	for _, mount := range spec.Mounts {
		rules = append(rules, LandlockRule{
			Path:   mount.Target,
			Access: mount.Access,
			Source: "mount",
		})
	}
	if spec.AgentHome.Path != "" {
		rules = append(rules, LandlockRule{
			Path:   spec.AgentHome.Path,
			Access: containment.PathReadWrite,
			Source: "agent_home",
		})
	}
	if spec.Temp.Path != "" {
		rules = append(rules, LandlockRule{
			Path:   spec.Temp.Path,
			Access: containment.PathReadWrite,
			Source: "temp",
		})
	}
	return rules
}

func rejectCredentialDenyOverlap(rules []LandlockRule, denies []linuxspec.CredentialDenySpec) error {
	for _, rule := range rules {
		for _, deny := range denies {
			if pathsOverlap(rule.Path, deny.Path) {
				return fmt.Errorf("linux current-user landlock rule %q overlaps credential deny path %q", rule.Path, deny.Path)
			}
		}
	}
	return nil
}

func pathsOverlap(left, right string) bool {
	left = path.Clean(left)
	right = path.Clean(right)
	return left == right ||
		strings.HasPrefix(left, right+"/") ||
		strings.HasPrefix(right, left+"/")
}
