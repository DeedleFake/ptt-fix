package glossy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/coreos/go-systemd/v22/journal"
	"golang.org/x/exp/slices"
	"golang.org/x/exp/slog"
)

var bufPool sync.Pool

var (
	styleTime  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#22222", Dark: "#AAAAAA"})
	styleKey   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#22222", Dark: "#AAAAAA"})
	styleValue = lipgloss.NewStyle()

	styleError = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#AA0000", Dark: "#EE0000"})
	styleWarn  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#AAAA00", Dark: "#EEEE00"})
	styleInfo  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#3333AA", Dark: "#5555EE"})
	styleDebug = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#00AA00", Dark: "#00EE00"})
)

func styleLevel(level slog.Level) lipgloss.Style {
	switch {
	case level >= slog.LevelError:
		return styleError
	case level >= slog.LevelWarn:
		return styleWarn
	case level >= slog.LevelInfo:
		return styleInfo
	case level >= slog.LevelDebug:
		return styleDebug
	default:
		return lipgloss.NewStyle()
	}
}

func toJournalPriority(level slog.Level) journal.Priority {
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

type Handler struct {
	UseJournal bool
	Level      slog.Level

	attrs []slog.Attr
	group string
}

func quoteIfNecessary(str string) string {
	for _, c := range str {
		if unicode.IsSpace(c) {
			return strconv.Quote(str)
		}
	}
	return str
}

func (h Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.Level
}

func (h Handler) Handle(ctx context.Context, r slog.Record) error {
	attrs := slices.Grow(h.attrs, r.NumAttrs())
	r.Attrs(func(a slog.Attr) {
		attrs = append(attrs, a)
	})
	if h.group != "" {
		attrs = []slog.Attr{slog.Group(h.group, attrs...)}
	}

	if h.UseJournal {
		return h.handleJournal(r, attrs)
	}

	buf, _ := bufPool.Get().(*bytes.Buffer)
	if buf == nil {
		buf = new(bytes.Buffer)
	}
	defer func() {
		buf.Reset()
		bufPool.Put(buf)
	}()

	fmt.Fprintf(
		buf,
		"%v %v %v\n",
		styleTime.Render(r.Time.Format(time.StampMilli)),
		styleLevel(r.Level).Render(r.Level.String()),
		r.Message,
	)
	for _, attr := range attrs {
		fmt.Fprintf(
			buf,
			"\t%v=%v\n",
			styleKey.Render(quoteIfNecessary(attr.Key)),
			styleValue.Render(quoteIfNecessary(attr.Value.String())),
		)
	}

	_, err := io.Copy(os.Stderr, buf)
	return err
}

func (h Handler) handleJournal(r slog.Record, attrs []slog.Attr) error {
	vars := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		vars[attr.Key] = attr.Value.String()
	}

	return journal.Send(r.Message, toJournalPriority(r.Level), vars)
}

func (h Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h.attrs = slices.Clip(append(h.attrs, attrs...))
	return h
}

func (h Handler) WithGroup(name string) slog.Handler {
	// TODO: Fix this.
	h.group = name
	return h
}
