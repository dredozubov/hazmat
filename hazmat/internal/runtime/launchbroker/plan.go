package launchbroker

import "errors"

type ChildFDPolicy uint8

const (
	ChildFDPolicyUnset ChildFDPolicy = iota
	ChildFDPolicyCloseInherited
)

type ChildPlan struct {
	Request  VerifiedLaunchRequest
	FDPolicy ChildFDPolicy
}

func NewChildPlan(req VerifiedLaunchRequest, policy ChildFDPolicy) (ChildPlan, error) {
	if req.peer.uid <= 0 {
		return ChildPlan{}, errors.New("verified launch request is required")
	}
	if policy != ChildFDPolicyCloseInherited {
		return ChildPlan{}, errors.New("launch child must close inherited file descriptors before sandbox_init")
	}
	return ChildPlan{Request: req, FDPolicy: policy}, nil
}

func (p ChildPlan) RequiresInheritedFDCleanup() bool {
	return p.FDPolicy == ChildFDPolicyCloseInherited
}
