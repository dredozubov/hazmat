package main

import "path/filepath"

// nativeSessionPolicy is the backend-neutral containment contract for a native
// Hazmat session. Backends compile this contract into OS-specific enforcement:
// Darwin currently emits SBPL; Linux will compile the same shape into its own
// native primitives.
type nativeSessionPolicy struct {
	ProjectDir         string
	ReadDirs           []string
	WriteDirs          []string
	AgentHome          string
	CredentialDenySubs []string
}

func newNativeSessionPolicy(cfg sessionConfig) nativeSessionPolicy {
	return nativeSessionPolicy{
		ProjectDir:         cfg.ProjectDir,
		ReadDirs:           cloneStringSlice(cfg.ReadDirs),
		WriteDirs:          cloneStringSlice(cfg.WriteDirs),
		AgentHome:          agentHome,
		CredentialDenySubs: cloneStringSlice(credentialDenySubs),
	}
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func (p nativeSessionPolicy) hostPaths() []string {
	hostPaths := append([]string{p.ProjectDir}, p.ReadDirs...)
	hostPaths = append(hostPaths, p.WriteDirs...)
	return hostPaths
}

func (p nativeSessionPolicy) ancestorMetadataDirs() []string {
	ancestors := make(map[string]struct{})
	for _, dir := range p.hostPaths() {
		for cur := filepath.Dir(dir); cur != "/" && cur != "."; cur = filepath.Dir(cur) {
			ancestors[cur] = struct{}{}
		}
	}
	if len(ancestors) == 0 {
		return nil
	}
	dirs := make([]string, 0, len(ancestors))
	for dir := range ancestors {
		dirs = append(dirs, dir)
	}
	return dirs
}

func (p nativeSessionPolicy) effectiveReadOnlyDirs() []string {
	if len(p.ReadDirs) == 0 {
		return nil
	}
	var pending []string
	for _, dir := range p.ReadDirs {
		if isWithinDir(p.ProjectDir, dir) {
			continue
		}
		coveredByWrite := false
		for _, writeDir := range p.WriteDirs {
			if isWithinDir(writeDir, dir) {
				coveredByWrite = true
				break
			}
		}
		if coveredByWrite {
			continue
		}
		covered := false
		for _, other := range p.ReadDirs {
			if other != dir && isWithinDir(other, dir) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		pending = append(pending, dir)
	}
	return pending
}

func (p nativeSessionPolicy) effectiveWritableDirs() []string {
	if len(p.WriteDirs) == 0 {
		return nil
	}
	var pending []string
	for _, dir := range p.WriteDirs {
		if isWithinDir(p.ProjectDir, dir) {
			continue
		}
		covered := false
		for _, other := range p.WriteDirs {
			if other != dir && isWithinDir(other, dir) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		pending = append(pending, dir)
	}
	return pending
}
