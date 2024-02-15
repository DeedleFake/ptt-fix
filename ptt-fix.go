package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/pprof"
	"strings"

	"deedles.dev/ptt-fix/internal/config"
	"deedles.dev/ptt-fix/internal/glossy"
	"github.com/coreos/go-systemd/v22/journal"
	"golang.org/x/sync/errgroup"
)

type event struct {
	Type   eventType
	Device string
}

type eventType uint8

const (
	eventInvalid eventType = iota
	eventUp
	eventDown
)

const errKey = "err"

type slogCtx struct{}

func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, slogCtx{}, logger)
}

func Logger(ctx context.Context) *slog.Logger {
	logger, _ := ctx.Value(slogCtx{}).(*slog.Logger)
	return logger
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
	logger := Logger(ctx)

	defaultConfigPath, err := config.DefaultPath()
	if err != nil {
		return fmt.Errorf("get default config path: %w", err)
	}

	createConfig := flag.Bool("createconfig", false, "write the default config so that it can be modified and then exit")
	configPath := flag.String("config", defaultConfigPath, "config file to use for either reading or writing")
	flag.Parse()

	if *createConfig {
		err := os.MkdirAll(filepath.Dir(*configPath), 0700)
		if err != nil {
			return fmt.Errorf("create config directory: %w", err)
		}

		file, err := os.Create(*configPath)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = file.WriteString(config.DefaultFile())
		if err != nil {
			return fmt.Errorf("write default config: %w", err)
		}

		logger.Info(
			"write default config file",
			"path", *configPath,
		)
		return nil
	}

	logPath := []any{"path", *configPath}
	c, err := config.Load(*configPath)
	if err != nil {
		if (*configPath != defaultConfigPath) || !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("load config: %w", err)
		}

		logPath = []any{"default", true}
		c, err = config.Parse(strings.NewReader(config.DefaultFile()))
		if err != nil {
			// If this happens, it's a bug.
			return fmt.Errorf("parse default config: %w", err)
		}
	}
	logger.Info("loaded config", logPath...)

	eg, ctx := errgroup.WithContext(ctx)

	var liseg errgroup.Group
	ev := make(chan event)
	for _, dev := range c.Devices {
		dev := dev
		liseg.Go(func() error {
			return Listener{
				Device:  dev,
				Keycode: uint16(c.Key),
				C:       ev,
				Retry:   c.Retry,
			}.Run(ctx)
		})
	}

	eg.Go(func() error {
		err := liseg.Wait()
		if err != nil {
			return err
		}
		return errors.New("no devices available")
	})
	eg.Go(func() error {
		return handle(ctx, c.Sym, ev)
	})

	err = eg.Wait()
	if (err != nil) && !errors.Is(err, context.Canceled) {
		return err
	}

	return nil
}

func profile() func() {
	path, ok := os.LookupEnv("PPROF")
	if !ok {
		return func() {}
	}

	file, err := os.Create(path)
	if err != nil {
		panic(err)
	}

	err = pprof.StartCPUProfile(file)
	if err != nil {
		panic(err)
	}

	return func() {
		pprof.StopCPUProfile()
		err := file.Close()
		if err != nil {
			panic(err)
		}
	}
}

func main() {
	defer profile()()

	usejournal, err := journal.StderrIsJournalStream()
	logger := slog.New(glossy.Handler{
		UseJournal: usejournal,
		Level:      slog.LevelDebug,
		ErrKey:     errKey,
	})
	if err != nil {
		logger.Error("could not determine if output is to journal", errKey, err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	ctx = WithLogger(ctx, logger)

	err = run(ctx)
	if err != nil {
		logger.Error("fatal", errKey, err)
		os.Exit(1)
	}
}
