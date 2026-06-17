package hazmat

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

type credentialSyncSupport uint8

const (
	credentialSyncSupported credentialSyncSupport = iota + 1
	credentialSyncAdapterRequired
	credentialSyncNoDelivery
)

type credentialSyncMaterial struct {
	secret     []byte
	generation int
	updatedAt  int64
	valid      bool
}

func newCredentialSyncMaterial(secret string, generation int, updatedAt int64) credentialSyncMaterial {
	return credentialSyncMaterial{
		secret:     []byte(secret),
		generation: generation,
		updatedAt:  updatedAt,
		valid:      true,
	}
}

func newInvalidCredentialSyncMaterial(secret string, generation int, updatedAt int64) credentialSyncMaterial {
	return credentialSyncMaterial{
		secret:     []byte(secret),
		generation: generation,
		updatedAt:  updatedAt,
		valid:      false,
	}
}

type credentialSyncEndpoint struct {
	label string
	read  func() (credentialSyncMaterial, bool, error)
	write func(credentialSyncMaterial) error
	clear func() error
}

func (e credentialSyncEndpoint) readValue() (credentialSyncMaterial, bool, error) {
	if e.read == nil {
		return credentialSyncMaterial{}, false, nil
	}
	return e.read()
}

func (e credentialSyncEndpoint) writeValue(value credentialSyncMaterial) error {
	if e.write == nil {
		return fmt.Errorf("%s is not writable", e.safeLabel())
	}
	return e.write(value)
}

func (e credentialSyncEndpoint) clearValue() error {
	if e.clear == nil {
		return nil
	}
	return e.clear()
}

func (e credentialSyncEndpoint) safeLabel() string {
	label := strings.TrimSpace(e.label)
	if label == "" {
		return "credential endpoint"
	}
	return label
}

type credentialSyncSpec struct {
	name       string
	support    credentialSyncSupport
	store      credentialSyncEndpoint
	hostCache  *credentialSyncEndpoint
	agentSinks []credentialSyncEndpoint
}

func (s credentialSyncSpec) displayName() string {
	name := strings.TrimSpace(s.name)
	if name == "" {
		return "credential"
	}
	return name
}

func syncCredentialBeforeLaunch(spec credentialSyncSpec) error {
	if err := validateCredentialSyncSupport(spec); err != nil {
		return err
	}
	candidates, err := syncReadEndpoints(spec.store, spec.hostCacheEndpoint())
	if err != nil {
		return err
	}
	value, ok, err := chooseCredentialSyncLatest(spec.displayName(), candidates...)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if err := spec.store.writeValue(value); err != nil {
		return err
	}
	if spec.hostCache != nil {
		if err := spec.hostCache.writeValue(value); err != nil {
			return err
		}
	}
	for _, sink := range spec.agentSinks {
		if err := sink.writeValue(value); err != nil {
			return err
		}
	}
	return nil
}

func syncCredentialAfterExit(spec credentialSyncSpec) error {
	if err := validateCredentialSyncSupport(spec); err != nil {
		return err
	}
	endpoints := []credentialSyncEndpoint{spec.store}
	if spec.hostCache != nil {
		endpoints = append(endpoints, *spec.hostCache)
	}
	endpoints = append(endpoints, spec.agentSinks...)
	candidates, err := syncReadEndpoints(endpoints...)
	if err != nil {
		return err
	}
	value, ok, err := chooseCredentialSyncLatest(spec.displayName(), candidates...)
	if err != nil {
		return err
	}
	if ok {
		if err := spec.store.writeValue(value); err != nil {
			return err
		}
		if spec.hostCache != nil {
			if err := spec.hostCache.writeValue(value); err != nil {
				return err
			}
		}
	}
	return clearCredentialSyncAgentSinks(spec.agentSinks)
}

func validateCredentialSyncSupport(spec credentialSyncSpec) error {
	switch spec.support {
	case credentialSyncSupported:
		return nil
	case credentialSyncAdapterRequired:
		return fmt.Errorf("%s sync requires an adapter", spec.displayName())
	case credentialSyncNoDelivery:
		return fmt.Errorf("%s is contained-only profile state, not a credential sync surface", spec.displayName())
	default:
		return fmt.Errorf("%s has unknown credential sync support", spec.displayName())
	}
}

func (s credentialSyncSpec) hostCacheEndpoint() credentialSyncEndpoint {
	if s.hostCache == nil {
		return credentialSyncEndpoint{}
	}
	return *s.hostCache
}

type credentialSyncCandidate struct {
	endpoint string
	value    credentialSyncMaterial
}

func syncReadEndpoints(endpoints ...credentialSyncEndpoint) ([]credentialSyncCandidate, error) {
	var out []credentialSyncCandidate
	for _, endpoint := range endpoints {
		if endpoint.read == nil {
			continue
		}
		value, ok, err := endpoint.readValue()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", endpoint.safeLabel(), err)
		}
		if !ok || !value.valid {
			continue
		}
		out = append(out, credentialSyncCandidate{
			endpoint: endpoint.safeLabel(),
			value:    value,
		})
	}
	return out, nil
}

func chooseCredentialSyncLatest(name string, candidates ...credentialSyncCandidate) (credentialSyncMaterial, bool, error) {
	var best credentialSyncCandidate
	found := false
	for _, candidate := range candidates {
		if !found || credentialSyncNewer(candidate.value, best.value) {
			best = candidate
			found = true
			continue
		}
		if credentialSyncSameFreshness(candidate.value, best.value) &&
			!bytes.Equal(candidate.value.secret, best.value.secret) {
			return credentialSyncMaterial{}, false, fmt.Errorf("%s sync conflict: ambiguous freshest values from %s and %s", name, best.endpoint, candidate.endpoint)
		}
	}
	if !found {
		return credentialSyncMaterial{}, false, nil
	}
	return best.value, true, nil
}

func credentialSyncNewer(a, b credentialSyncMaterial) bool {
	if a.generation != b.generation {
		return a.generation > b.generation
	}
	return a.updatedAt > b.updatedAt
}

func credentialSyncSameFreshness(a, b credentialSyncMaterial) bool {
	return a.generation == b.generation && a.updatedAt == b.updatedAt
}

func clearCredentialSyncAgentSinks(sinks []credentialSyncEndpoint) error {
	var errs []error
	for _, sink := range sinks {
		if err := sink.clearValue(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
