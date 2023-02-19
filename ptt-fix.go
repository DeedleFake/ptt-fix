package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"deedles.dev/ptt-fix/internal/xdo"
	"golang.org/x/exp/slog"
	"golang.org/x/sync/errgroup"
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

type slogCtx struct{}

func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, slogCtx{}, logger)
}

func Logger(ctx context.Context) *slog.Logger {
	logger, _ := ctx.Value(slogCtx{}).(*slog.Logger)
	return logger
}

func handle(ctx context.Context, key string, devs int, ev <-chan int) error {
	logger := Logger(ctx)

	do, ok := xdo.New()
	if !ok {
		return errors.New("xdo initialization failed")
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case ev := <-ev:
			switch ev {
			case eventUp:
				do.SendKeysequenceWindowUp(xdo.CurrentWindow, key, 0)
				logger.Debug("deactivated")
			case eventDown:
				do.SendKeysequenceWindowDown(xdo.CurrentWindow, key, 0)
				logger.Debug("activated")
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
	const dir = "/dev/input/by-id"

	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	devices := make([]string, 0, len(files))
	for _, f := range files {
		if f.IsDir() {
			continue
		}

		devices = append(devices, filepath.Join(dir, f.Name()))
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
	retry := flag.Duration("retry", 10*time.Second, "time to wait before retrying devices that disappear (0 to disable)")
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

	eg, ctx := errgroup.WithContext(ctx)

	ev := make(chan int)
	for _, dev := range devices {
		dev := dev
		eg.Go(func() error {
			return Listener{
				Device:  dev,
				Keycode: uint16(*key),
				C:       ev,
				Retry:   *retry,
			}.Run(ctx)
		})
	}

	eg.Go(func() error {
		return handle(ctx, *sym, len(devices), ev)
	})

	err := eg.Wait()
	if (err != nil) && !errors.Is(err, context.Canceled) {
		return err
	}

	return nil
}

func main() {
	var defaultLevel slog.LevelVar
	logger := slog.New(slog.HandlerOptions{
		Level: &defaultLevel,
	}.NewTextHandler(os.Stderr))
	defaultLevel.Set(slog.LevelDebug)

	if addr := os.Getenv("PPROF_ADDR"); addr != "" {
		go func() { logger.Error("start pprof HTTP server", http.ListenAndServe(addr, nil)) }()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	ctx = WithLogger(ctx, logger)

	err := run(ctx)
	if err != nil {
		logger.Error("fatal", err)
		os.Exit(1)
	}
}
