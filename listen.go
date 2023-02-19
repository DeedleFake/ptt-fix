package main

/*
#include <linux/input.h>
*/
import "C"

import (
	"context"
	"errors"
	"io/fs"
	"time"

	"deedles.dev/ptt-fix/internal/evdev"
	"golang.org/x/sys/unix"
)

type Listener struct {
	Device  string
	Keycode uint16
	C       chan<- int
	Retry   time.Duration
}

func (lis Listener) Run(ctx context.Context) error {
	logger := Logger(ctx).With("device", lis.Device)
	ctx = WithLogger(ctx, logger)

	defer func() {
		select {
		case <-ctx.Done():
		case lis.C <- eventDone:
			// TODO: This is an awkward hack and needs to be done more properly.
		}
	}()

	for {
		retry, err := lis.listen(ctx)
		if (lis.Retry <= 0) || !retry {
			return err
		}

		logger.Info("waiting before retrying", "duration", lis.Retry, slogErr(err))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(lis.Retry):
		}
	}
}

func (lis *Listener) listen(ctx context.Context) (retry bool, err error) {
	logger := Logger(ctx)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	d, err := evdev.Open(lis.Device)
	if err != nil {
		retry := isTemporary(err) || errors.Is(err, fs.ErrNotExist)
		if retry {
			return true, err
		}

		logger.Warn("ignoring device", "reason", "failed to open", slogErr(err))
		return false, nil
	}
	defer d.Close()

	go func() {
		<-ctx.Done()
		d.Close()
	}()

	logger.Info(
		"initialized device",
		"name", d.Name,
		"bus", d.ID.BusType,
		"vendor", d.ID.Vendor,
		"product", d.ID.Product,
	)

	if !d.HasEventCode(C.EV_KEY, lis.Keycode) {
		logger.Info("ignoring device", "reason", "incapable of sending requested key code")
		return false, nil
	}

	for {
		ev, err := d.NextEvent()
		if err != nil {
			if ctx.Err() != nil {
				return false, err
			}
			if errors.Is(err, fs.ErrClosed) {
				logger.Warn("device closed while reading")
				return false, nil
			}
			if isTemporary(err) {
				logger.Warn("device disappeared while reading", slogErr(err))
				return true, err
			}

			logger.Warn("read event", slogErr(err))
			continue
		}

		if !ev.Is(C.EV_KEY, lis.Keycode) {
			continue
		}

		switch ev.Value {
		case 2:
		case 1:
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case lis.C <- eventDown:
			}
		default:
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case lis.C <- eventUp:
			}
		}
	}
}

func isTemporary(err error) bool {
	var errno unix.Errno
	return errors.As(err, &errno) && errno.Temporary()
}
