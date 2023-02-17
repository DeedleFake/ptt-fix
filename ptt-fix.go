package main

/*
#cgo pkg-config: libxdo

#include <malloc.h>
#include <errno.h>
#include <linux/input.h>
#include <xdo.h>
*/
import "C"

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	"deedles.dev/ptt-fix/internal/evdev"
	"golang.org/x/exp/slog"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"
)

const (
	eventInvalid = iota
	eventUp
	eventDown
	eventDone
)

func slogErr(err error) slog.Attr {
	return slog.Any(slog.ErrorKey, err)
}

func listen(ctx context.Context, device string, keycode uint16, out chan<- int) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	defer func() {
		select {
		case <-ctx.Done():
		case out <- eventDone:
		}
	}()

	d, err := evdev.Open(device)
	if err != nil {
		panic(err)
	}
	defer d.Close()

	slog := slog.With(slog.Group(
		"device",
		slog.String("path", device),
		slog.String("name", d.Name),
	))

	go func() {
		<-ctx.Done()
		d.Close()
	}()

	slog.Info(
		"initialized device",
		"bus", d.BusType,
		"vendor", d.Vendor,
		"product", d.Product,
	)

	if !d.HasEventCode(C.EV_KEY, keycode) {
		slog.Info("ignoring device", "reason", "incapable of sending requested key code")
		return nil
	}

	for {
		ev, err := d.NextEvent()
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			if errors.Is(err, fs.ErrClosed) {
				slog.Warn("device closed while reading")
				return nil
			}
			if errno := *new(unix.Errno); errors.As(err, &errno) && !errno.Temporary() {
				slog.Warn("device disappeared while reading", slogErr(err))
				return nil
			}

			slog.Warn("read event", slogErr(err))
			continue
		}

		if !ev.Is(C.EV_KEY, keycode) {
			continue
		}

		switch ev.Value {
		case 2:
		case 1:
			select {
			case <-ctx.Done():
				return ctx.Err()
			case out <- eventDown:
			}
		default:
			select {
			case <-ctx.Done():
				return ctx.Err()
			case out <- eventUp:
			}
		}
	}
}

func handle(ctx context.Context, xdo *C.struct_xdo, key *C.char, devs int, ev <-chan int) error {
	defer C.xdo_free(xdo)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case ev := <-ev:
			switch ev {
			case eventUp:
				C.xdo_send_keysequence_window_up(xdo, C.CURRENTWINDOW, key, 0)
				slog.Debug("deactivated")
			case eventDown:
				C.xdo_send_keysequence_window_down(xdo, C.CURRENTWINDOW, key, 0)
				slog.Debug("activated")
			case eventDone:
				devs--
				if devs == 0 {
					return errors.New("all devices have become unavailable")
				}
			default:
				return fmt.Errorf("invalid event: %v", ev)
			}
		}
	}
}

func findDevices() ([]string, error) {
	files, err := os.ReadDir("/dev/input")
	if err != nil {
		return nil, err
	}

	devices := make([]string, 0, len(files))
	for _, f := range files {
		if f.IsDir() || !strings.HasPrefix(f.Name(), "event") {
			continue
		}

		devices = append(devices, filepath.Join("/dev/input", f.Name()))
	}

	return devices, nil
}

func run(ctx context.Context) error {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %v [/dev/input/by-id/<device>...]\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "Options:")
		flag.PrintDefaults()
	}
	key := flag.Uint("key", 56, "keycode to watch for")
	sym := flag.String("sym", "Alt_L", "key symbol to send to X")
	flag.Parse()

	devices := flag.Args()
	if len(devices) == 0 {
		d, err := findDevices()
		if err != nil {
			return fmt.Errorf("find devices: %w", err)
		}
		if len(d) == 0 {
			return errors.New("no devices found")
		}

		devices = d
	}

	xdo := C.xdo_new(nil)
	if xdo == nil {
		return errors.New("initialize xdo")
	}

	xdokey := C.CString(*sym)
	defer C.free(unsafe.Pointer(xdokey))

	eg, ctx := errgroup.WithContext(ctx)

	ev := make(chan int)
	for _, dev := range devices {
		dev := dev
		eg.Go(func() error { return listen(ctx, dev, uint16(*key), ev) })
	}
	eg.Go(func() error { return handle(ctx, xdo, xdokey, len(devices), ev) })

	err := eg.Wait()
	if (err != nil) && !errors.Is(err, context.Canceled) {
		return err
	}

	return nil
}

func main() {
	var defaultLevel slog.LevelVar
	slog.SetDefault(slog.New(slog.HandlerOptions{
		Level: &defaultLevel,
	}.NewTextHandler(os.Stderr)))
	if ll, ok := os.LookupEnv("LOG_LEVEL"); ok {
		if v, err := strconv.ParseInt(ll, 10, 0); err == nil {
			defaultLevel.Set(slog.Level((4 - v - 1) * 4))
		}
	}

	if addr := os.Getenv("PPROF_ADDR"); addr != "" {
		go func() { slog.Error("start pprof HTTP server", http.ListenAndServe(addr, nil)) }()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	err := run(ctx)
	if err != nil {
		slog.Error("fatal", err)
		os.Exit(1)
	}
}
