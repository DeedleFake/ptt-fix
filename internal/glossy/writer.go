package glossy

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"unsafe"

	"github.com/coreos/go-systemd/v22/journal"
	"golang.org/x/exp/slog"
)

type stderrOutput struct {
	*bytes.Buffer
}

func (out *stderrOutput) Close() error {
	_, err := io.Copy(os.Stderr, out)

	out.Reset()
	bufPool.Put(out.Buffer)
	out.Buffer = nil

	return err
}

type journalOutput struct {
	*bytes.Buffer
	r slog.Record
}

func (out *journalOutput) Close() error {
	b := out.Bytes()
	str := unsafe.String(&b[0], len(b))
	return journal.Print(levelToPriority(out.r.Level), str)
}

func levelToPriority(level slog.Level) journal.Priority {
	switch {
	case level >= slog.LevelError:
		return journal.PriErr
	case level >= slog.LevelWarn:
		return journal.PriWarning
	case level >= slog.LevelInfo:
		return journal.PriInfo
	case level >= slog.LevelDebug:
		return journal.PriDebug
	default:
		panic(fmt.Errorf("unsupporter log level: %v", level))
	}
}
