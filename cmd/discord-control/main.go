package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/application"
	"github.com/ken-kai/discord-control-experiments/internal/buildinfo"
	"github.com/ken-kai/discord-control-experiments/internal/cli"
	"github.com/ken-kai/discord-control-experiments/internal/config"
	discordadapter "github.com/ken-kai/discord-control-experiments/internal/discord"
	mcpadapter "github.com/ken-kai/discord-control-experiments/internal/mcp"
	"github.com/ken-kai/discord-control-experiments/internal/observability"
)

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command, err := cli.Parse(args)
	if err != nil {
		return err
	}
	if command.Name == "version" {
		return cli.WriteVersion(os.Stdout, buildinfo.Version, buildinfo.Commit)
	}
	file, err := os.Open(command.ConfigPath)
	if err != nil {
		return fmt.Errorf("open configuration: %w", err)
	}
	defer file.Close()
	resolved, err := config.Load(file, config.EnvironmentResolver{Lookup: os.LookupEnv})
	if err != nil {
		return err
	}

	var discordClient *discordadapter.Client
	var redactor *observability.Redactor
	err = resolved.DiscordToken.Use(func(token string) error {
		redactor = observability.NewRedactor(token)
		discordClient, err = discordadapter.New(discordadapter.TokenUse(resolved.DiscordToken.Use))
		return err
	})
	if err != nil {
		return fmt.Errorf("initialize Discord adapter: %w", err)
	}
	provider, err := observability.New(context.Background(), observability.Options{Writer: os.Stderr, Exporter: resolved.Observability.Exporter, Endpoint: resolved.Observability.Endpoint, Build: buildinfo.Current(), Redactor: redactor})
	if err != nil {
		return fmt.Errorf("initialize observability: %w", err)
	}
	mcpServer, err := mcpadapter.New(provider.Logger, resolved.MCPEnabled)
	if err != nil {
		return err
	}
	app, err := application.New(discordClient, mcpServer, provider, systemClock{}, observability.CorrelationID)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runErr := app.Run(ctx)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return errors.Join(runErr, provider.Shutdown(shutdownCtx))
}
