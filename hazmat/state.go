package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// stateFilePath is where hazmat records core init state and harness metadata.
var stateFilePath = filepath.Join(os.Getenv("HOME"), ".hazmat/state.json")

// HazmatState tracks the installed core version and any managed harness state.
type HazmatState struct {
	InitVersion string                     `json:"init_version"`
	InitDate    string                     `json:"init_date"`
	Harnesses   map[HarnessID]HarnessState `json:"harnesses,omitempty"`
}

func loadState() (HazmatState, error) {
	data, err := os.ReadFile(stateFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return HazmatState{}, nil // no state = fresh install
		}
		return HazmatState{}, err
	}
	var s HazmatState
	if err := json.Unmarshal(data, &s); err != nil {
		return HazmatState{}, err
	}
	return s, nil
}

func saveState(ver string) error {
	s, err := loadState()
	if err != nil {
		s = HazmatState{}
	}
	s.InitVersion = ver
	s.InitDate = time.Now().UTC().Format(time.RFC3339)
	return writeState(s)
}

func updateHarnessState(id HarnessID, mutate func(HarnessState) HarnessState) error {
	s, err := loadState()
	if err != nil {
		return err
	}
	if s.Harnesses == nil {
		s.Harnesses = make(map[HarnessID]HarnessState)
	}
	s.Harnesses[id] = mutate(s.Harnesses[id])
	return writeState(s)
}

func writeState(s HazmatState) error {
	dir := filepath.Dir(stateFilePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(stateFilePath, append(data, '\n'), 0o600)
}

func (s HazmatState) hasHarnessState() bool {
	return len(s.Harnesses) > 0
}

// semverCompare returns -1, 0, or 1 comparing a and b as semver strings.
// Handles "dev" as greater than any released version.
// Handles commit hashes (non-semver) as equal to "dev".
func semverCompare(a, b string) int {
	if a == b {
		return 0
	}
	na := parseSemver(a)
	nb := parseSemver(b)
	// dev/unknown sorts after everything
	if na == nil && nb == nil {
		return 0
	}
	if na == nil {
		return 1 // a is dev, b is released → a > b
	}
	if nb == nil {
		return -1 // a is released, b is dev → a < b
	}
	for i := 0; i < 3; i++ {
		if na[i] < nb[i] {
			return -1
		}
		if na[i] > nb[i] {
			return 1
		}
	}
	return 0
}

// parseSemver extracts [major, minor, patch] from strings like "0.2.0",
// "v0.2.0", "0.2.0-dirty". Returns nil for non-semver (e.g. "dev", commit hashes).
func parseSemver(s string) []int {
	s = strings.TrimPrefix(s, "v")
	// Strip -dirty, -rc1, etc.
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		s = s[:idx]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return nil
	}
	result := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		result[i] = n
	}
	return result
}

// knownVersions is the ordered list of versions with migration functions.
// Must match the TLA+ spec's Versions / NextVersion / HasMigration.
var knownVersions = []string{"0.1.0", "0.2.0", "0.3.0"}

// pendingMigrations returns the chain of migrations needed to go from
// fromVer to toVer. Returns nil if no migration is needed.
func pendingMigrations(fromVer, toVer string) []migration {
	fromVer = strings.TrimPrefix(fromVer, "v")
	toVer = strings.TrimPrefix(toVer, "v")

	// Find starting position in the version chain.
	fromIdx := -1
	toIdx := -1
	for i, v := range knownVersions {
		if v == fromVer {
			fromIdx = i
		}
		if v == toVer {
			toIdx = i
		}
	}

	// If either version is unknown (dev build, commit hash), or already current,
	// no migrations.
	if fromIdx < 0 || toIdx < 0 || fromIdx >= toIdx {
		return nil
	}

	var chain []migration
	for i := fromIdx; i < toIdx; i++ {
		from := knownVersions[i]
		to := knownVersions[i+1]
		m, ok := migrations[fmt.Sprintf("%s→%s", from, to)]
		if !ok {
			// Gap in migration chain — can't skip versions.
			// This shouldn't happen if knownVersions and migrations are in sync.
			return nil
		}
		chain = append(chain, m)
	}
	return chain
}
