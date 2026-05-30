//go:build hazmat_debug

package main

type linuxTraceToolResolver func(name string) (string, bool)

type linuxStracePlan struct {
	Enabled        bool
	ToolPath       string
	DegradedReason string
}

func planLinuxStrace(opts traceOptions, resolve linuxTraceToolResolver) linuxStracePlan {
	if !opts.Syscalls {
		return linuxStracePlan{}
	}
	path, ok := resolve("strace")
	if !ok {
		return linuxStracePlan{
			DegradedReason: "strace not found in supported Linux tool paths; continuing without syscall trace",
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
