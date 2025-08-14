package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"deedles.dev/ptt-fix/internal/config"
	"deedles.dev/ptt-fix/internal/xdo"
)

func handle(ctx context.Context, key config.Sym, ev <-chan event) error {
	logger := Logger(ctx)

	do, ok := xdo.New()
	if !ok {
		return errors.New("xdo initialization failed")
	}

	sender, err := newSender(do, key)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)

		case ev := <-ev:
			switch ev.Type {
			case eventUp:
				sender.Up()
				logger.Info("deactivated", "device", ev.Device)
			case eventDown:
				sender.Down()
				logger.Info("activated", "device", ev.Device)
			default:
				return fmt.Errorf("invalid event: %v", ev)
			}
		}
	}
}

type sender interface {
	Up()
	Down()
}

func newSender(do *xdo.Xdo, sym config.Sym) (sender, error) {
	switch sym.Type {
	case "key":
		return keySender{do, sym.Val}, nil

	case "mouse":
		v, err := strconv.ParseInt(sym.Val, 0, 0)
		if err != nil {
			return nil, fmt.Errorf("invalid mouse button: %w", err)
		}
		return mouseSender{do, int(v)}, nil

	default:
		return nil, fmt.Errorf("invalid sym type: %q", sym.Type)
	}
}

type keySender struct {
	do  *xdo.Xdo
	sym string
}

func (s keySender) Up() {
	s.do.SendKeysequenceWindowUp(xdo.CurrentWindow, s.sym, 0)
}

func (s keySender) Down() {
	s.do.SendKeysequenceWindowDown(xdo.CurrentWindow, s.sym, 0)
}

type mouseSender struct {
	do     *xdo.Xdo
	button int
}

func (s mouseSender) Up() {
	s.do.MouseUp(xdo.CurrentWindow, s.button)
}

func (s mouseSender) Down() {
	s.do.MouseDown(xdo.CurrentWindow, s.button)
}
