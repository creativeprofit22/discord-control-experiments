package gateway

import (
	"context"
	"errors"
)

type Event struct {
	Type string
	Data []byte
}

type Consumer struct {
	handle func(context.Context, Event) error
}

func New(handle func(context.Context, Event) error) (*Consumer, error) {
	if handle == nil {
		return nil, errors.New("gateway event handler is required")
	}
	return &Consumer{handle: handle}, nil
}

func (c *Consumer) Consume(ctx context.Context, event Event) error {
	if event.Type == "" {
		return errors.New("gateway event type is required")
	}
	return c.handle(ctx, event)
}
