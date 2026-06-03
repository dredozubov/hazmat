package harnessruntime

import (
	"time"

	"hazmat/harnesses"
)

type State struct {
	StateVersion    string `json:"state_version,omitempty"`
	LastImportRunAt string `json:"last_import_run_at,omitempty"`
}

type Snapshot struct {
	InitVersion string                 `json:"init_version"`
	InitDate    string                 `json:"init_date"`
	Harnesses   map[harnesses.ID]State `json:"harnesses,omitempty"`
}

func (s Snapshot) HasHarnessState() bool {
	return len(s.Harnesses) > 0
}

type StateStore interface {
	Load() (Snapshot, error)
	Write(Snapshot) error
	Remove() error
}

func RecordInstalled(store StateStore, spec harnesses.Spec) error {
	return UpdateHarnessState(store, spec.ID, func(state State) State {
		state.StateVersion = spec.StateVersion
		return state
	})
}

func RecordImportRun(store StateStore, spec harnesses.Spec) error {
	return RecordImportRunAt(store, spec, time.Now().UTC())
}

func RecordImportRunAt(store StateStore, spec harnesses.Spec, at time.Time) error {
	return UpdateHarnessState(store, spec.ID, func(state State) State {
		state.StateVersion = spec.StateVersion
		state.LastImportRunAt = at.UTC().Format(time.RFC3339)
		return state
	})
}

func UpdateHarnessState(store StateStore, id harnesses.ID, mutate func(State) State) error {
	snapshot, err := store.Load()
	if err != nil {
		return err
	}
	if snapshot.Harnesses == nil {
		snapshot.Harnesses = make(map[harnesses.ID]State)
	}
	snapshot.Harnesses[id] = mutate(snapshot.Harnesses[id])
	return store.Write(snapshot)
}

func RemoveHarnessState(store StateStore, id harnesses.ID) error {
	snapshot, err := store.Load()
	if err != nil {
		return err
	}
	if snapshot.Harnesses == nil {
		return nil
	}
	if _, ok := snapshot.Harnesses[id]; !ok {
		return nil
	}
	delete(snapshot.Harnesses, id)
	if len(snapshot.Harnesses) == 0 {
		snapshot.Harnesses = nil
	}
	if snapshot.InitVersion == "" && snapshot.InitDate == "" && len(snapshot.Harnesses) == 0 {
		return store.Remove()
	}
	return store.Write(snapshot)
}
