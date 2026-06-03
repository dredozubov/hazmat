// Package hostfacts defines explicit host/platform facts passed into planners.
//
// The package is data-only: callers collect facts at frontend or runtime
// boundaries, then pass these values into pure planners.
package hostfacts

type Platform struct {
	GOOS   string `json:"goos,omitempty"`
	GOARCH string `json:"goarch,omitempty"`
}

type DockerAvailability struct {
	Available bool   `json:"available,omitempty"`
	Source    string `json:"source,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type HarnessStatus struct {
	Installed bool   `json:"installed,omitempty"`
	Version   string `json:"version,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type ProbeFact struct {
	Available bool   `json:"available,omitempty"`
	Source    string `json:"source,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type IntegrationMarker struct {
	Name   string `json:"name,omitempty"`
	Source string `json:"source,omitempty"`
}

type Facts struct {
	Platform           Platform                 `json:"platform"`
	AgentHome          string                   `json:"agent_home,omitempty"`
	InvokerHome        string                   `json:"invoker_home,omitempty"`
	Docker             DockerAvailability       `json:"docker,omitempty"`
	Harnesses          map[string]HarnessStatus `json:"harnesses,omitempty"`
	KernelProbes       map[string]ProbeFact     `json:"kernel_probes,omitempty"`
	PlatformProbes     map[string]ProbeFact     `json:"platform_probes,omitempty"`
	IntegrationMarkers []IntegrationMarker      `json:"integration_markers,omitempty"`
}

func ForPlatform(goos, goarch string) Facts {
	return Facts{Platform: Platform{GOOS: goos, GOARCH: goarch}}
}

func ForGOOS(goos string) Facts {
	return Facts{Platform: Platform{GOOS: goos}}
}

func (f Facts) TargetGOOS() string {
	return f.Platform.GOOS
}

func (f Facts) Normalized() Facts {
	out := f
	if len(f.Harnesses) > 0 {
		out.Harnesses = make(map[string]HarnessStatus, len(f.Harnesses))
		for key, value := range f.Harnesses {
			out.Harnesses[key] = value
		}
	}
	if len(f.KernelProbes) > 0 {
		out.KernelProbes = make(map[string]ProbeFact, len(f.KernelProbes))
		for key, value := range f.KernelProbes {
			out.KernelProbes[key] = value
		}
	}
	if len(f.PlatformProbes) > 0 {
		out.PlatformProbes = make(map[string]ProbeFact, len(f.PlatformProbes))
		for key, value := range f.PlatformProbes {
			out.PlatformProbes[key] = value
		}
	}
	if len(f.IntegrationMarkers) > 0 {
		out.IntegrationMarkers = append([]IntegrationMarker(nil), f.IntegrationMarkers...)
	}
	return out
}
