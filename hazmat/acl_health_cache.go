package hazmat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const startupACLHealthCacheVersion = 1

var startupACLHealthCachePath = defaultStartupACLHealthCachePath

type startupACLHealthCache struct {
	Version int                            `json:"version"`
	Entries map[string]aclHealthCacheEntry `json:"entries"`
}

type aclHealthCacheEntry struct {
	State               aclHealthPathState `json:"state"`
	DevGroupInheritable bool               `json:"dev_group_inheritable"`
	AgentTraverse       bool               `json:"agent_traverse"`
}

type aclHealthPathState struct {
	Device    uint64 `json:"device"`
	Inode     uint64 `json:"inode"`
	Mode      uint32 `json:"mode"`
	UID       uint32 `json:"uid"`
	GID       uint32 `json:"gid"`
	CTimeSec  int64  `json:"ctime_sec"`
	CTimeNsec int64  `json:"ctime_nsec"`
}

func readStartupACLsForPaths(paths []string) map[string]aclReadResult {
	results := make(map[string]aclReadResult)
	unique := uniqueACLPaths(paths)
	if len(unique) == 0 {
		return results
	}

	cache, path, cacheOK := loadStartupACLHealthCache()
	if !cacheOK {
		return readACLsForPaths(unique)
	}

	misses := make([]string, 0, len(unique))
	for _, aclPath := range unique {
		if result, ok := cache.resultForPath(aclPath); ok {
			results[aclPath] = result
			continue
		}
		misses = append(misses, aclPath)
	}
	if len(misses) == 0 {
		if cache.retainPaths(unique) {
			saveStartupACLHealthCache(path, cache)
		}
		return results
	}

	fresh := readACLsForPaths(misses)
	for aclPath, result := range fresh {
		results[aclPath] = result
	}
	changed := cache.updateFromResults(fresh)
	if cache.retainPaths(unique) {
		changed = true
	}
	if changed {
		saveStartupACLHealthCache(path, cache)
	}
	return results
}

func defaultStartupACLHealthCachePath() string {
	if override := os.Getenv("HAZMAT_ACL_HEALTH_CACHE"); override != "" {
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
	return filepath.Join(dir, "hazmat", "acl-health-v1.json")
}

func loadStartupACLHealthCache() (startupACLHealthCache, string, bool) {
	path := startupACLHealthCachePath()
	cache := startupACLHealthCache{
		Version: startupACLHealthCacheVersion,
		Entries: make(map[string]aclHealthCacheEntry),
	}
	if path == "" {
		return cache, "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cache, path, true
	}
	var loaded startupACLHealthCache
	if err := json.Unmarshal(data, &loaded); err != nil {
		return cache, path, true
	}
	if loaded.Version != startupACLHealthCacheVersion || loaded.Entries == nil {
		return cache, path, true
	}
	return loaded, path, true
}

func (c startupACLHealthCache) resultForPath(path string) (aclReadResult, bool) {
	entry, ok := c.Entries[path]
	if !ok {
		return aclReadResult{}, false
	}
	state, ok := currentACLHealthPathState(path)
	if !ok || state != entry.State {
		return aclReadResult{}, false
	}
	return aclReadResult{Rows: entry.rows(), OK: true}, true
}

func (c startupACLHealthCache) updateFromResults(results map[string]aclReadResult) bool {
	changed := false
	if c.Entries == nil {
		c.Entries = make(map[string]aclHealthCacheEntry)
		changed = true
	}
	for path, result := range results {
		if !result.OK {
			if _, exists := c.Entries[path]; exists {
				delete(c.Entries, path)
				changed = true
			}
			continue
		}
		state, ok := currentACLHealthPathState(path)
		if !ok {
			if _, exists := c.Entries[path]; exists {
				delete(c.Entries, path)
				changed = true
			}
			continue
		}
		entry := aclHealthCacheEntry{
			State:               state,
			DevGroupInheritable: aclRowsSatisfy(result.Rows, devGroupInheritableGrant),
			AgentTraverse:       homeHasAgentTraverseACLRows(result.Rows),
		}
		if current, exists := c.Entries[path]; !exists || current != entry {
			c.Entries[path] = entry
			changed = true
		}
	}
	return changed
}

func (c startupACLHealthCache) retainPaths(paths []string) bool {
	if len(c.Entries) == 0 {
		return false
	}
	keep := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if path != "" {
			keep[path] = struct{}{}
		}
	}

	changed := false
	for path := range c.Entries {
		if _, ok := keep[path]; ok {
			continue
		}
		delete(c.Entries, path)
		changed = true
	}
	return changed
}

func (e aclHealthCacheEntry) rows() []ACLRow {
	var rows []ACLRow
	if e.DevGroupInheritable {
		rows = append(rows, aclSyntheticRowForGrant(devGroupInheritableGrant))
	}
	if e.AgentTraverse {
		rows = append(rows, aclSyntheticRowForGrant(agentTraverseGrant))
	}
	return rows
}

func saveStartupACLHealthCache(path string, cache startupACLHealthCache) {
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
