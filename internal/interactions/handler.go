package interactions

import (
	"context"
	"errors"
)

type Handler func(context.Context, []byte) error

type Dispatcher struct{ handlers map[uint8]Handler }

func New(handlers map[uint8]Handler) (*Dispatcher, error) {
	if len(handlers) == 0 {
		return nil, errors.New("interaction handlers are required")
	}
	copyOf := make(map[uint8]Handler, len(handlers))
	for kind, handler := range handlers {
		if kind == 0 || handler == nil {
			return nil, errors.New("invalid interaction handler")
		}
		copyOf[kind] = handler
	}
	return &Dispatcher{handlers: copyOf}, nil
}

func (d *Dispatcher) Dispatch(ctx context.Context, kind uint8, payload []byte) error {
	handler, ok := d.handlers[kind]
	if !ok {
		return errors.New("unsupported interaction type")
	}
	return handler(ctx, payload)
}
