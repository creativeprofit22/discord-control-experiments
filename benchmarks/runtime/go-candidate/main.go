package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const protocolVersion = "2026-07-28"

type restResult struct {
	DurationsNS []int64 `json:"durationsNs"`
	UserID      string  `json:"userId"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("mode is required")
	}
	switch args[0] {
	case "cold":
		_, _, _ = construct()
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"ready": true, "protocolVersion": protocolVersion})
	case "mcp":
		_, _, server := construct()
		err := server.Run(context.Background(), &mcp.StdioTransport{})
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	case "rest":
		if len(args) != 4 {
			return errors.New("usage: rest URL WARMUPS SAMPLES")
		}
		warmups, err := strconv.Atoi(args[2])
		if err != nil || warmups < 0 {
			return errors.New("invalid warmup count")
		}
		samples, err := strconv.Atoi(args[3])
		if err != nil || samples < 1 {
			return errors.New("invalid sample count")
		}
		return runREST(args[1], warmups, samples)
	default:
		return fmt.Errorf("unknown mode %q", args[0])
	}
}

func construct() (*slog.Logger, *discordgo.Session, *mcp.Server) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	session, _ := discordgo.New("Bot benchmark-placeholder")
	session.Client = &http.Client{Timeout: 5 * time.Second}
	server := mcp.NewServer(&mcp.Implementation{Name: "runtime-benchmark", Version: "1.0.0"}, nil)
	return logger, session, server
}

func runREST(rawURL string, warmups, samples int) error {
	base, err := validateLoopbackURL(rawURL)
	if err != nil {
		return err
	}
	_, session, _ := construct()
	discordgo.EndpointUsers = strings.TrimRight(base, "/") + "/api/v10/users/"

	var user *discordgo.User
	for i := 0; i < warmups; i++ {
		user, err = session.User("@me")
		if err != nil {
			return fmt.Errorf("warmup request: %w", err)
		}
	}
	durations := make([]int64, 0, samples)
	for i := 0; i < samples; i++ {
		start := time.Now()
		user, err = session.User("@me")
		durations = append(durations, time.Since(start).Nanoseconds())
		if err != nil {
			return fmt.Errorf("measured request: %w", err)
		}
	}
	if user == nil || user.ID == "" {
		return errors.New("typed Discord user response was empty")
	}
	return json.NewEncoder(os.Stdout).Encode(restResult{DurationsNS: durations, UserID: user.ID})
}

func validateLoopbackURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Hostname() == "" {
		return "", errors.New("fixture URL must be an HTTP loopback URL")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return "", errors.New("fixture URL must resolve literally to loopback")
	}
	if u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("fixture URL must not contain a path, query, or fragment")
	}
	return strings.TrimRight(raw, "/"), nil
}
