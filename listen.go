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
	defer func() {
		select {
		case <-ctx.Done():
		case lis.C <- eventDone:
			// TODO: This is an awkward hack and needs to be done more properly.
		}
	}()

	logger := Logger(ctx)
	logger = logger.With("device", lis.Device)

	for {
		err := lis.listen(WithLogger(ctx, logger))
		if (lis.Retry > 0) && isRetry(err) {
			logger.Info("waiting before retrying", "duration", lis.Retry)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(lis.Retry):
				continue
			}
		}

		return err
	}
}

func (lis *Listener) listen(ctx context.Context) error {
	logger := Logger(ctx)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	d, err := evdev.Open(lis.Device)
	if err != nil {
		return retryError{err}
	}
	defer d.Close()

	go func() {
		<-ctx.Done()
		d.Close()
	}()

	logger.Info(
		"initialized device",
		"name", d.Name,
		"bus", d.BusType,
		"vendor", d.Vendor,
		"product", d.Product,
	)

	if !d.HasEventCode(C.EV_KEY, lis.Keycode) {
		logger.Info("ignoring device", "reason", "incapable of sending requested key code")
		return nil
	}

	for {
		ev, err := d.NextEvent()
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			if errors.Is(err, fs.ErrClosed) {
				logger.Warn("device closed while reading")
				return nil
			}
			if errno := *new(unix.Errno); errors.As(err, &errno) && !errno.Temporary() {
				logger.Warn("device disappeared while reading", slogErr(err))
				return retryError{err}
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
				return ctx.Err()
			case lis.C <- eventDown:
			}
		default:
			select {
			case <-ctx.Done():
				return ctx.Err()
			case lis.C <- eventUp:
			}
		}
	}
}

type retryError struct {
	Err error
}

func (err retryError) Error() string {
	return err.Err.Error()
}

func (err retryError) Unwrap() error {
	return err.Err
}

func isRetry(err error) bool {
	var r retryError
	return errors.As(err, &r)
}
