package mcp

import (
	"context"
	"errors"
	"io"
	"log/slog"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const MaxTools = 16

type Server struct {
	sdk     *sdk.Server
	logger  *slog.Logger
	tools   map[string]struct{}
	enabled bool
	cancel  context.CancelFunc
	done    chan error
}

func New(logger *slog.Logger, enabled bool) (*Server, error) {
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	return &Server{sdk: sdk.NewServer(&sdk.Implementation{Name: "discord-control", Version: "dev"}, nil), logger: logger, tools: make(map[string]struct{}), enabled: enabled}, nil
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
	if name == "" || len(s.tools) >= MaxTools {
		return errors.New("fixed MCP tool registry limit reached or name empty")
	}
	s.tools[name] = struct{}{}
	return nil
}
