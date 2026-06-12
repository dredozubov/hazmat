package hazmat

import (
	"errors"
	"reflect"
	"testing"

	"hazmat/internal/sessionflow"
	"hazmat/sessionmeta"
)

func TestRunSessionStartupPhasesExecutesPlannedOrder(t *testing.T) {
	tests := []struct {
		name  string
		input sessionflow.Input
		want  []string
	}{
		{
			name:  "native default",
			input: sessionflow.Input{Mode: sessionmeta.ModeNative},
			want:  []string{"render", "snapshot:false", "mutate"},
		},
		{
			name: "native preflight before snapshot",
			input: sessionflow.Input{
				Mode:                    sessionmeta.ModeNative,
				PreflightBeforeSnapshot: true,
			},
			want: []string{"render", "mutate", "snapshot:false"},
		},
		{
			name:  "docker",
			input: sessionflow.Input{Mode: sessionmeta.ModeDockerSandbox},
			want:  []string{"render", "mutate", "snapshot:false"},
		},
		{
			name: "skip snapshot",
			input: sessionflow.Input{
				Mode:         sessionmeta.ModeNative,
				SkipSnapshot: true,
			},
			want: []string{"render", "snapshot:true", "mutate"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := sessionflow.New(tt.input)
			if err != nil {
				t.Fatalf("sessionflow.New: %v", err)
			}
			var got []string
			err = runSessionStartupPhases(plan, sessionStartupActions{
				renderContract: func() {
					got = append(got, "render")
				},
				executeHostMutations: func() error {
					got = append(got, "mutate")
					return nil
				},
				snapshot: func(skip bool) {
					if skip {
						got = append(got, "snapshot:true")
					} else {
						got = append(got, "snapshot:false")
					}
				},
			})
			if err != nil {
				t.Fatalf("runSessionStartupPhases: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("actions = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunSessionStartupPhasesStopsOnMutationError(t *testing.T) {
	plan, err := sessionflow.New(sessionflow.Input{
		Mode:                    sessionmeta.ModeNative,
		PreflightBeforeSnapshot: true,
	})
	if err != nil {
		t.Fatalf("sessionflow.New: %v", err)
	}
	wantErr := errors.New("repair failed")
	var got []string
	err = runSessionStartupPhases(plan, sessionStartupActions{
		renderContract: func() {
			got = append(got, "render")
		},
		executeHostMutations: func() error {
			got = append(got, "mutate")
			return wantErr
		},
		snapshot: func(bool) {
			got = append(got, "snapshot")
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if !reflect.DeepEqual(got, []string{"render", "mutate"}) {
		t.Fatalf("actions = %v, want render then mutation only", got)
	}
}

func TestRunSessionStartupPhasesRequiresActions(t *testing.T) {
	plan, err := sessionflow.New(sessionflow.Input{Mode: sessionmeta.ModeNative})
	if err != nil {
		t.Fatalf("sessionflow.New: %v", err)
	}
	if err := runSessionStartupPhases(plan, sessionStartupActions{}); err == nil {
		t.Fatal("runSessionStartupPhases succeeded without actions")
	}
}
