package harnesses

import "fmt"

type ID string

const (
	Claude      ID = "claude"
	Codex       ID = "codex"
	OpenCode    ID = "opencode"
	Gemini      ID = "gemini"
	Hermes      ID = "hermes"
	Qwen        ID = "qwen"
	CursorAgent ID = "cursor-agent"
	Pi          ID = "pi"
)

const (
	ClaudeStateVersion      = "1"
	CodexStateVersion       = "1"
	OpenCodeStateVersion    = "1"
	GeminiStateVersion      = "1"
	HermesStateVersion      = "1"
	QwenStateVersion        = "1"
	CursorAgentStateVersion = "1"
	PiStateVersion          = "1"
)

type Spec struct {
	ID           ID
	DisplayName  string
	StateVersion string
}

type ImportPolicy struct {
	Supported bool
	Boundary  string
}

type Metadata struct {
	Spec             Spec
	LaunchCommand    string
	BootstrapCommand string
	ImportPolicy     ImportPolicy
}

var builtinMetadata = []Metadata{
	{
		Spec:             Spec{ID: Claude, DisplayName: "Claude Code", StateVersion: ClaudeStateVersion},
		LaunchCommand:    "hazmat claude",
		BootstrapCommand: "hazmat bootstrap claude",
		ImportPolicy: ImportPolicy{
			Supported: true,
			Boundary:  "portable Claude auth, settings, hooks, and project basics",
		},
	},
	{
		Spec:             Spec{ID: Codex, DisplayName: "Codex", StateVersion: CodexStateVersion},
		LaunchCommand:    "hazmat codex",
		BootstrapCommand: "hazmat bootstrap codex",
		ImportPolicy: ImportPolicy{
			Supported: true,
			Boundary:  "portable Codex auth, config, prompts, and session basics",
		},
	},
	{
		Spec:             Spec{ID: OpenCode, DisplayName: "OpenCode", StateVersion: OpenCodeStateVersion},
		LaunchCommand:    "hazmat opencode",
		BootstrapCommand: "hazmat bootstrap opencode",
		ImportPolicy: ImportPolicy{
			Supported: true,
			Boundary:  "portable OpenCode auth, config, command, and agent basics",
		},
	},
	{
		Spec:             Spec{ID: Gemini, DisplayName: "Gemini", StateVersion: GeminiStateVersion},
		LaunchCommand:    "hazmat gemini",
		BootstrapCommand: "hazmat bootstrap gemini",
		ImportPolicy: ImportPolicy{
			Supported: true,
			Boundary:  "file-backed Gemini OAuth/account files sync through registered paths; settings and memory basics import; Keychain OAuth remains external and adapter-required",
		},
	},
	{
		Spec:             Spec{ID: Hermes, DisplayName: "Hermes", StateVersion: HermesStateVersion},
		LaunchCommand:    "hazmat hermes",
		BootstrapCommand: "hazmat bootstrap hermes",
		ImportPolicy: ImportPolicy{
			Supported: false,
			Boundary:  "Hermes v1 has no curated import; contained-only profile roots are preserved and host ~/.hermes is not synced",
		},
	},
	{
		Spec:             Spec{ID: Qwen, DisplayName: "Qwen Code", StateVersion: QwenStateVersion},
		LaunchCommand:    "hazmat qwen",
		BootstrapCommand: "hazmat bootstrap qwen",
		ImportPolicy: ImportPolicy{
			Supported: false,
			Boundary:  "Qwen v1 has no curated import; contained-only profile state is preserved and host ~/.qwen auth/settings are not synced",
		},
	},
	{
		Spec:             Spec{ID: CursorAgent, DisplayName: "Cursor Agent", StateVersion: CursorAgentStateVersion},
		LaunchCommand:    "hazmat cursor-agent",
		BootstrapCommand: "hazmat bootstrap cursor-agent",
		ImportPolicy: ImportPolicy{
			Supported: false,
			Boundary:  "Cursor Agent v1 has no curated import; contained-only profile state is preserved and host Cursor IDE/auth state is not synced",
		},
	},
	{
		Spec:             Spec{ID: Pi, DisplayName: "Pi", StateVersion: PiStateVersion},
		LaunchCommand:    "hazmat pi",
		BootstrapCommand: "hazmat bootstrap pi",
		ImportPolicy: ImportPolicy{
			Supported: false,
			Boundary:  "Pi v1 has no curated import; contained-only profile state is preserved and host ~/.pi/agent settings/trust/sessions/auth are not synced",
		},
	},
}

func BuiltinMetadata() []Metadata {
	out := make([]Metadata, len(builtinMetadata))
	copy(out, builtinMetadata)
	return out
}

func MetadataByID(id ID) (Metadata, bool) {
	for _, metadata := range builtinMetadata {
		if metadata.Spec.ID == id {
			return metadata, true
		}
	}
	return Metadata{}, false
}

func MustMetadata(id ID) Metadata {
	metadata, ok := MetadataByID(id)
	if !ok {
		panic(fmt.Sprintf("missing harness metadata %q", id))
	}
	return metadata
}

func SpecByID(id ID) (Spec, bool) {
	metadata, ok := MetadataByID(id)
	if !ok {
		return Spec{}, false
	}
	return metadata.Spec, true
}

func MustSpec(id ID) Spec {
	return MustMetadata(id).Spec
}
