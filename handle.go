package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"deedles.dev/ptt-fix/internal/xdo"
)

func handle(ctx context.Context, key string, ev <-chan int) error {
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
			switch ev {
			case eventUp:
				sender.Up()
				logger.Debug("deactivated")
			case eventDown:
				sender.Down()
				logger.Debug("activated")
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

func newSender(do *xdo.Xdo, sym string) (sender, error) {
	if n, ok := strings.CutPrefix(sym, "mouse:"); ok {
		v, err := strconv.ParseInt(n, 10, 0)
		if err != nil {
			return nil, fmt.Errorf("parse mouse button %q: %v", n, err)
		}
		return mouseSender{do, int(v)}, nil
	}

	return keySender{do, sym}, nil
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
