package schema

import (
	"context"
	"fmt"
	"sync"

	"dagger.io/dagger/core"
	"dagger.io/dagger/project"
	"dagger.io/dagger/router"
	"github.com/pkg/errors"
)

type projectSchema struct {
	*baseSchema
	projectStates map[string]*project.State
	mu            sync.RWMutex
}

var _ router.ExecutableSchema = &projectSchema{}

func (s *projectSchema) Name() string {
	return "project"
}

func (s *projectSchema) Schema() string {
	return `
"A set of scripts and/or extensions"
type Project {
	"name of the project"
	name: String!

	"schema provided by the project"
	schema: String

	"sdk used to generate code for and/or execute this project"
	sdk: String

	"extensions in this project"
	extensions: [Project!]

	"install the project's schema"
	install: Boolean!

	"Code files generated by the SDKs in the project"
	generatedCode: Directory!
}

extend type Directory {
	"load a project's metadata"
	loadProject(configPath: String!): Project!
}

extend type Query {
	"Look up a project by name"
	project(name: String!): Project!
}
`
}

func (s *projectSchema) Resolvers() router.Resolvers {
	return router.Resolvers{
		"Directory": router.ObjectResolver{
			"loadProject": router.ToResolver(s.loadProject),
		},
		"Query": router.ObjectResolver{
			"project": router.ToResolver(s.project),
		},
		"Project": router.ObjectResolver{
			"schema":        router.ToResolver(s.schema),
			"sdk":           router.ToResolver(s.sdk),
			"extensions":    router.ToResolver(s.extensions),
			"install":       router.ToResolver(s.install),
			"generatedCode": router.ErrResolver(errors.New("not implemented")),
		},
	}
}

func (s *projectSchema) Dependencies() []router.ExecutableSchema {
	return nil
}

type Project struct {
	Name string
}

type loadProjectArgs struct {
	ConfigPath string
}

func (s *projectSchema) loadProject(ctx *router.Context, parent *core.Directory, args loadProjectArgs) (*Project, error) {
	projectState, err := project.Load(ctx, parent, args.ConfigPath, s.projectStates, &s.mu, s.gw)
	if err != nil {
		return nil, err
	}
	return &Project{Name: projectState.Name()}, nil
}

type projectArgs struct {
	Name string
}

func (s *projectSchema) project(ctx *router.Context, parent struct{}, args projectArgs) (*Project, error) {
	_, ok := s.getProjectState(args.Name)
	if !ok {
		return nil, fmt.Errorf("project %q not found", args.Name)
	}
	return &Project{Name: args.Name}, nil
}

func (s *projectSchema) schema(ctx *router.Context, parent *Project, args any) (string, error) {
	projectState, ok := s.getProjectState(parent.Name)
	if !ok {
		return "", fmt.Errorf("project %q not found", parent.Name)
	}
	return projectState.Schema(ctx, s.gw, s.platform, s.sshAuthSockID)
}

func (s *projectSchema) sdk(ctx *router.Context, parent *Project, args any) (string, error) {
	projectState, ok := s.getProjectState(parent.Name)
	if !ok {
		return "", fmt.Errorf("project %q not found", parent.Name)
	}
	return projectState.SDK(), nil
}

func (s *projectSchema) extensions(ctx *router.Context, parent *Project, args any) ([]*Project, error) {
	projectState, ok := s.getProjectState(parent.Name)
	if !ok {
		return nil, fmt.Errorf("project %q not found", parent.Name)
	}

	dependencies, err := projectState.Extensions(ctx, s.projectStates, &s.mu, s.gw, s.platform, s.sshAuthSockID)
	if err != nil {
		return nil, err
	}
	depProjects := make([]*Project, len(dependencies))
	for i, dependency := range dependencies {
		if _, ok := s.projectStates[dependency.Name()]; !ok {
			s.projectStates[dependency.Name()] = dependency
		}
		depProjects[i] = &Project{Name: dependency.Name()}
	}
	return depProjects, nil
}

func (s *projectSchema) install(ctx *router.Context, parent *Project, args any) (bool, error) {
	projectState, ok := s.getProjectState(parent.Name)
	if !ok {
		return false, fmt.Errorf("project %q not found", parent.Name)
	}

	executableSchema, err := s.projectToExecutableSchema(ctx, projectState)
	if err != nil {
		return false, err
	}

	if err := s.router.Add(executableSchema); err != nil {
		return false, err
	}

	return true, nil
}

func (s *projectSchema) getProjectState(name string) (*project.State, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	projectState, ok := s.projectStates[name]
	return projectState, ok
}

func (s *projectSchema) projectToExecutableSchema(ctx context.Context, projectState *project.State) (router.ExecutableSchema, error) {
	schema, err := projectState.Schema(ctx, s.gw, s.platform, s.sshAuthSockID)
	if err != nil {
		return nil, err
	}

	resolvers, err := projectState.Resolvers(ctx, s.gw, s.platform, s.sshAuthSockID)
	if err != nil {
		return nil, err
	}

	params := router.StaticSchemaParams{
		Name:      projectState.Name(),
		Schema:    schema,
		Resolvers: resolvers,
	}

	dependencies, err := projectState.Extensions(ctx, s.projectStates, &s.mu, s.gw, s.platform, s.sshAuthSockID)
	if err != nil {
		return nil, err
	}
	// TODO:(sipsma) guard against circular dependencies, dedupe objects
	for _, dependency := range dependencies {
		remoteSchema, err := s.projectToExecutableSchema(ctx, dependency)
		if err != nil {
			return nil, err
		}
		params.Dependencies = append(params.Dependencies, remoteSchema)
	}

	return router.StaticSchema(params), nil
}
