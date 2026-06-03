// Package sessionrequest builds validated session requests from typed path
// policy constructors.
package sessionrequest

import "hazmat/pathpolicy"

type Stage string

const (
	StageProject   Stage = "project"
	StageReadOnly  Stage = "read-only"
	StageReadWrite Stage = "read-write"
)

type Error struct {
	Stage Stage
	Err   error
}

func (e Error) Error() string {
	if e.Err == nil {
		return string(e.Stage)
	}
	return e.Err.Error()
}

func (e Error) Unwrap() error {
	return e.Err
}

type Input struct {
	Project             string
	DefaultProjectToCwd bool
	ReadOnlyPaths       []string
	ReadWritePaths      []string
	DenyPolicy          pathpolicy.DenyPolicy
}

type Request struct {
	projectRoot     pathpolicy.ProjectRoot
	readOnlyGrants  []pathpolicy.ReadOnlyGrant
	readWriteGrants []pathpolicy.ReadWriteGrant
}

func New(input Input) (Request, error) {
	projectRoot, err := ResolveProjectRoot(input.Project, input.DefaultProjectToCwd, input.DenyPolicy)
	if err != nil {
		return Request{}, Error{Stage: StageProject, Err: err}
	}
	readOnlyGrants, err := ResolveReadOnlyGrants(input.ReadOnlyPaths, input.DenyPolicy)
	if err != nil {
		return Request{}, Error{Stage: StageReadOnly, Err: err}
	}
	readWriteGrants, err := ResolveReadWriteGrants(input.ReadWritePaths, input.DenyPolicy)
	if err != nil {
		return Request{}, Error{Stage: StageReadWrite, Err: err}
	}
	return Request{
		projectRoot:     projectRoot,
		readOnlyGrants:  readOnlyGrants,
		readWriteGrants: readWriteGrants,
	}, nil
}

func ResolveProjectRoot(project string, defaultToCwd bool, policy pathpolicy.DenyPolicy) (pathpolicy.ProjectRoot, error) {
	return pathpolicy.ResolveProjectRoot(project, defaultToCwd, policy)
}

func ResolveReadOnlyGrants(paths []string, policy pathpolicy.DenyPolicy) ([]pathpolicy.ReadOnlyGrant, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(paths))
	grants := make([]pathpolicy.ReadOnlyGrant, 0, len(paths))
	for _, path := range paths {
		grant, err := pathpolicy.ResolveReadOnlyGrant(path, policy)
		if err != nil {
			return nil, err
		}
		dir := grant.String()
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		grants = append(grants, grant)
	}
	return grants, nil
}

func ResolveReadWriteGrants(paths []string, policy pathpolicy.DenyPolicy) ([]pathpolicy.ReadWriteGrant, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(paths))
	grants := make([]pathpolicy.ReadWriteGrant, 0, len(paths))
	for _, path := range paths {
		grant, err := pathpolicy.ResolveReadWriteGrant(path, policy)
		if err != nil {
			return nil, err
		}
		dir := grant.String()
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		grants = append(grants, grant)
	}
	return grants, nil
}

func (r Request) ProjectRoot() pathpolicy.ProjectRoot {
	return r.projectRoot
}

func (r Request) ProjectDir() string {
	return r.projectRoot.String()
}

func (r Request) ReadOnlyGrants() []pathpolicy.ReadOnlyGrant {
	return append([]pathpolicy.ReadOnlyGrant(nil), r.readOnlyGrants...)
}

func (r Request) ReadOnlyDirs() []string {
	out := make([]string, 0, len(r.readOnlyGrants))
	for _, grant := range r.readOnlyGrants {
		out = append(out, grant.String())
	}
	return out
}

func (r Request) ReadWriteGrants() []pathpolicy.ReadWriteGrant {
	return append([]pathpolicy.ReadWriteGrant(nil), r.readWriteGrants...)
}

func (r Request) ReadWriteDirs() []string {
	out := make([]string, 0, len(r.readWriteGrants))
	for _, grant := range r.readWriteGrants {
		out = append(out, grant.String())
	}
	return out
}
