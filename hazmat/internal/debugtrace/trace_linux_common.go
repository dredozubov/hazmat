//go:build hazmat_debug

package debugtrace

type linuxTraceToolResolver func(name string) (string, bool)

type linuxStracePlan struct {
	Enabled       bool
	ToolPath      string
	MissingReason string
}

func planLinuxStrace(resolve linuxTraceToolResolver) linuxStracePlan {
	path, ok := resolve("strace")
	if !ok {
		return linuxStracePlan{
			MissingReason: "strace not found in supported Linux tool paths",
		}
	}
	return linuxStracePlan{
		Enabled:  true,
		ToolPath: path,
	}
}

func linuxStraceCommandArgs(outputPath string, target []string) []string {
	args := []string{
		"-ff",
		"-ttt",
		"-T",
		"-s", "256",
		"-yy",
		"-o", outputPath,
		"--",
	}
	return append(args, target...)
}
