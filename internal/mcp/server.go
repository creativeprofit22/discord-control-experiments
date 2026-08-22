package mcp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"sync"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const MaxTools = 16

type ToolDescriptor struct {
	Name       string
	Capability domain.Capability
	Mutation   bool
}

type ToolClassifier func(ToolDescriptor) (bool, error)

type Server struct {
	mu      sync.Mutex
	sdk     *sdk.Server
	logger  *slog.Logger
	tools   map[string]ToolDescriptor
	enabled bool
	cancel  context.CancelFunc
	done    chan error
}

func New(logger *slog.Logger, enabled bool) (*Server, error) {
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	return &Server{sdk: sdk.NewServer(&sdk.Implementation{Name: "discord-control", Version: "dev"}, nil), logger: logger, tools: make(map[string]ToolDescriptor), enabled: enabled}, nil
}

func (s *Server) Name() domain.Adapter { return "mcp" }
func (s *Server) Start(ctx context.Context) error {
	if !s.enabled {
		return nil
	}
	serveCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan error, 1)
	go func() { s.done <- s.Serve(serveCtx) }()
	return nil
}
func (s *Server) Stop(ctx context.Context) error {
	if s.cancel == nil {
		return nil
	}
	s.cancel()
	select {
	case err := <-s.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) Serve(ctx context.Context) error {
	err := s.sdk.Run(ctx, &sdk.StdioTransport{})
	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func (s *Server) RegisterToolName(name string) error {
	return s.RegisterTools([]ToolDescriptor{{Name: name}}, func(ToolDescriptor) (bool, error) { return true, nil })
}

func (s *Server) RegisterTools(descriptors []ToolDescriptor, classifier ToolClassifier) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if classifier == nil || len(descriptors) > MaxTools {
		s.tools = make(map[string]ToolDescriptor)
		return errors.New("invalid MCP tool classifier or registry limit")
	}
	next := make(map[string]ToolDescriptor, len(descriptors))
	for _, descriptor := range descriptors {
		if descriptor.Name == "" {
			s.tools = make(map[string]ToolDescriptor)
			return errors.New("MCP tool name is empty")
		}
		allowed, err := classifier(descriptor)
		if err != nil {
			s.tools = make(map[string]ToolDescriptor)
			return errors.New("MCP tool classification failed")
		}
		if !allowed {
			continue
		}
		if _, duplicate := next[descriptor.Name]; duplicate {
			s.tools = make(map[string]ToolDescriptor)
			return errors.New("duplicate MCP tool name")
		}
		next[descriptor.Name] = descriptor
	}
	s.tools = next
	return nil
}

func (s *Server) ToolNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.tools))
	for name := range s.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
