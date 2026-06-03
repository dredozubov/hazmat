package runtime

import (
	"fmt"

	"hazmat/internal/runtime/darwin"
	"hazmat/internal/runtime/docker"
	"hazmat/internal/runtime/linux"
	"hazmat/sessionbackend"
)

type Selection struct {
	Backend       sessionbackend.Kind
	PackagePath   string
	Native        bool
	DockerSandbox bool
	PlanOnly      bool
	Remote        bool
	Unsupported   bool
}

func Select(plan sessionbackend.Plan) (Selection, error) {
	switch plan.Backend {
	case sessionbackend.KindDarwinNative:
		return Selection{
			Backend:     plan.Backend,
			PackagePath: darwin.PackagePath,
			Native:      true,
		}, nil
	case sessionbackend.KindDockerSandbox:
		return Selection{
			Backend:       plan.Backend,
			PackagePath:   docker.PackagePath,
			DockerSandbox: true,
		}, nil
	case sessionbackend.KindLinuxNative:
		return Selection{
			Backend:     plan.Backend,
			PackagePath: linux.PackagePath,
			Native:      true,
			PlanOnly:    linux.PlanOnly,
		}, nil
	case sessionbackend.KindRemoteEnvelope:
		return Selection{
			Backend:  plan.Backend,
			PlanOnly: true,
			Remote:   true,
		}, nil
	case sessionbackend.KindUnsupportedNative:
		return Selection{
			Backend:     plan.Backend,
			PlanOnly:    true,
			Unsupported: true,
		}, nil
	case "":
		return Selection{}, fmt.Errorf("runtime selection requires a backend kind")
	default:
		return Selection{}, fmt.Errorf("runtime backend %q is not registered", plan.Backend)
	}
}

func (s Selection) UsesDockerSandbox() bool {
	return s.DockerSandbox
}

func (s Selection) UsesNativeLaunch() bool {
	return s.Native
}
