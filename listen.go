package main

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
	C       chan<- event
	Retry   time.Duration
}

func (lis Listener) Run(ctx context.Context) error {
	logger := Logger(ctx).With("device", lis.Device)
	ctx = WithLogger(ctx, logger)

	for {
		retry, err := lis.listen(ctx)
		if (lis.Retry <= 0) || !retry {
			return err
		}

		logger.Info("waiting before retrying", "duration", lis.Retry, errKey, err)
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
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

		logger.Warn("ignoring device", "reason", "failed to open", errKey, err)
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

	if !d.HasEventCode(evdev.EvKey, lis.Keycode) {
		logger.Info("ignoring device", "reason", "incapable of sending requested key code")
		return false, nil
	}

	for {
		ev, err := d.NextEvent()
		if err != nil {
			if context.Cause(ctx) != nil {
				return false, err
			}
			if errors.Is(err, fs.ErrClosed) {
				logger.Warn("device closed while reading")
				return false, nil
			}
			if isTemporary(err) {
				logger.Warn("device disappeared while reading", errKey, err)
				return true, err
			}

			logger.Warn("read event", errKey, err)
			continue
		}

		if !ev.Is(evdev.EvKey, lis.Keycode) {
			continue
		}

		switch ev.Value {
		case 2:
		case 1:
			select {
			case <-ctx.Done():
				return false, context.Cause(ctx)
			case lis.C <- event{Type: eventDown, Device: lis.Device}:
			}
		default:
			select {
			case <-ctx.Done():
				return false, context.Cause(ctx)
			case lis.C <- event{Type: eventUp, Device: lis.Device}:
			}
		}
	}
}

func isTemporary(err error) bool {
	var errno unix.Errno
	return errors.As(err, &errno) && errno.Temporary()
}
