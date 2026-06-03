package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"hazmat/harnesses"
)

type HarnessState struct {
	StateVersion    string `json:"state_version,omitempty"`
	LastImportRunAt string `json:"last_import_run_at,omitempty"`
}

type Snapshot struct {
	InitVersion string                        `json:"init_version"`
	InitDate    string                        `json:"init_date"`
	Harnesses   map[harnesses.ID]HarnessState `json:"harnesses,omitempty"`
}

func (s Snapshot) HasHarnessState() bool {
	return len(s.Harnesses) > 0
}

type Store struct {
	Path string
	Now  func() time.Time
}

func (s Store) Load() (Snapshot, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return Snapshot{}, nil
		}
		return Snapshot{}, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func (s Store) SaveVersion(version string) error {
	snapshot, err := s.Load()
	if err != nil {
		snapshot = Snapshot{}
	}
	snapshot.InitVersion = version
	snapshot.InitDate = s.now().UTC().Format(time.RFC3339)
	return s.Write(snapshot)
}

func (s Store) Write(snapshot Snapshot) error {
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.Path, append(data, '\n'), 0o600)
}

func (s Store) Remove() error {
	if err := os.Remove(s.Path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
