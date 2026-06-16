package hazmat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const gitMetadataHealthCacheVersion = 1

var (
	gitMetadataHealthCachePath        = defaultGitMetadataHealthCachePath
	collectGitPermissionProblemsFresh = collectGitPermissionProblems
)

type gitMetadataHealthCache struct {
	Version int                                    `json:"version"`
	Entries map[string]gitMetadataHealthCacheEntry `json:"entries"`
}

type gitMetadataHealthCacheEntry struct {
	Healthy bool                   `json:"healthy"`
	Paths   []gitMetadataPathState `json:"paths"`
}

type gitMetadataPathState struct {
	Path     string             `json:"path"`
	Optional bool               `json:"optional"`
	Present  bool               `json:"present"`
	State    aclHealthPathState `json:"state,omitempty"`
}

func collectGitPermissionProblemsCached(gitDir string) []string {
	if gitMetadataHealthCacheHealthy(gitDir) {
		return nil
	}
	problems := collectGitPermissionProblemsFresh(gitDir)
	rememberGitMetadataHealth(gitDir, len(problems) == 0)
	return problems
}

func defaultGitMetadataHealthCachePath() string {
	if override := os.Getenv("HAZMAT_GIT_METADATA_HEALTH_CACHE"); override != "" {
		if override == "off" {
			return ""
		}
		return override
	}
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil || home == "" {
			home = os.Getenv("HOME")
		}
		if home == "" {
			return ""
		}
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "hazmat", "git-metadata-health-v1.json")
}

func gitMetadataHealthCacheHealthy(gitDir string) bool {
	cache, _, ok := loadGitMetadataHealthCache()
	if !ok {
		return false
	}
	entry, ok := cache.Entries[gitDir]
	if !ok || !entry.Healthy {
		return false
	}
	paths, ok := currentGitMetadataPathStates(gitDir)
	return ok && gitMetadataPathStatesEqual(paths, entry.Paths)
}

func rememberGitMetadataHealth(gitDir string, healthy bool) {
	cache, path, ok := loadGitMetadataHealthCache()
	if !ok {
		return
	}
	if cache.Entries == nil {
		cache.Entries = make(map[string]gitMetadataHealthCacheEntry)
	}
	if !healthy {
		if _, exists := cache.Entries[gitDir]; exists {
			delete(cache.Entries, gitDir)
			saveGitMetadataHealthCache(path, cache)
		}
		return
	}

	paths, ok := currentGitMetadataPathStates(gitDir)
	if !ok {
		if _, exists := cache.Entries[gitDir]; exists {
			delete(cache.Entries, gitDir)
			saveGitMetadataHealthCache(path, cache)
		}
		return
	}
	entry := gitMetadataHealthCacheEntry{
		Healthy: true,
		Paths:   paths,
	}
	if current, exists := cache.Entries[gitDir]; exists && gitMetadataHealthEntriesEqual(current, entry) {
		return
	}
	cache.Entries[gitDir] = entry
	saveGitMetadataHealthCache(path, cache)
}

func loadGitMetadataHealthCache() (gitMetadataHealthCache, string, bool) {
	path := gitMetadataHealthCachePath()
	cache := gitMetadataHealthCache{
		Version: gitMetadataHealthCacheVersion,
		Entries: make(map[string]gitMetadataHealthCacheEntry),
	}
	if path == "" {
		return cache, "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cache, path, true
	}
	var loaded gitMetadataHealthCache
	if err := json.Unmarshal(data, &loaded); err != nil {
		return cache, path, true
	}
	if loaded.Version != gitMetadataHealthCacheVersion || loaded.Entries == nil {
		return cache, path, true
	}
	return loaded, path, true
}

func currentGitMetadataPathStates(gitDir string) ([]gitMetadataPathState, bool) {
	requirements := gitPathRequirements(gitDir)
	states := make([]gitMetadataPathState, 0, len(requirements))
	for _, req := range requirements {
		state := gitMetadataPathState{
			Path:     req.path,
			Optional: req.optional,
		}
		if _, err := os.Lstat(req.path); err != nil {
			if os.IsNotExist(err) && req.optional {
				states = append(states, state)
				continue
			}
			return nil, false
		}
		pathState, ok := currentACLHealthPathState(req.path)
		if !ok {
			return nil, false
		}
		state.Present = true
		state.State = pathState
		states = append(states, state)
	}
	return states, true
}

func gitMetadataHealthEntriesEqual(a, b gitMetadataHealthCacheEntry) bool {
	return a.Healthy == b.Healthy && gitMetadataPathStatesEqual(a.Paths, b.Paths)
}

func gitMetadataPathStatesEqual(a, b []gitMetadataPathState) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func saveGitMetadataHealthCache(path string, cache gitMetadataHealthCache) {
	if path == "" {
		return
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	tmp := filepath.Join(dir, fmt.Sprintf(".%s.%d.tmp", filepath.Base(path), os.Getpid()))
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}
