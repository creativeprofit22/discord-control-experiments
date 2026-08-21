package discord

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type Client struct{ session *discordgo.Session }

func New(token string) (*Client, error) {
	if token == "" {
		return nil, errors.New("discord token is required")
	}
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}
	session.Client = &http.Client{Timeout: 15 * time.Second}
	return &Client{session: session}, nil
}

func (c *Client) Name() domain.Adapter        { return "discord" }
func (c *Client) Start(context.Context) error { return nil }
func (c *Client) Stop(context.Context) error {
	if c.session.DataReady {
		return c.session.Close()
	}
	return nil
}
