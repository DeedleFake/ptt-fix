package main

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"deedles.dev/ptt-fix/internal/config"
	"deedles.dev/ptt-fix/internal/xdo"
)

func handle(ctx context.Context, key config.Sym, ev <-chan event) error {
	logger := Logger(ctx)

	do, err := xdo.Open()
	if err != nil {
		return fmt.Errorf("xdo initialization failed: %w", err)
	}
	defer do.Close()

	sender, err := newSender(do, key)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)

		case ev := <-ev:
			if err := applyEvent(logger, sender, ev); err != nil {
				return err
			}
		}
	}
}

// applyEvent dispatches a single up/down event through the sender.
// Injection errors are returned so the process can exit (and be restarted).
func applyEvent(logger *slog.Logger, s sender, ev event) error {
	switch ev.Type {
	case eventUp:
		if err := s.Up(); err != nil {
			return fmt.Errorf("deactivate (%s): %w", ev.Device, err)
		}
		logger.Info("deactivated", "device", ev.Device)
		return nil
	case eventDown:
		if err := s.Down(); err != nil {
			return fmt.Errorf("activate (%s): %w", ev.Device, err)
		}
		logger.Info("activated", "device", ev.Device)
		return nil
	default:
		return fmt.Errorf("invalid event: %v", ev)
	}
}

type sender interface {
	Up() error
	Down() error
}

func newSender(do *xdo.Xdo, sym config.Sym) (sender, error) {
	switch sym.Type {
	case "key":
		b, err := do.BindKeys(sym.Val)
		if err != nil {
			return nil, fmt.Errorf("resolve keysym %q: %w", sym.Val, err)
		}
		return b, nil

	case "mouse":
		v, err := strconv.ParseInt(sym.Val, 0, 0)
		if err != nil {
			return nil, fmt.Errorf("invalid mouse button: %w", err)
		}
		if err := xdo.ValidButton(int(v)); err != nil {
			return nil, err
		}
		return mouseSender{do: do, button: int(v)}, nil

	default:
		return nil, fmt.Errorf("invalid sym type: %q", sym.Type)
	}
}

type mouseSender struct {
	do     *xdo.Xdo
	button int
}

func (s mouseSender) Up() error {
	return s.do.ButtonUp(s.button)
}

func (s mouseSender) Down() error {
	return s.do.ButtonDown(s.button)
}
