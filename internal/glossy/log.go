package glossy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/exp/slices"
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

func (h Handler) writer(r slog.Record) io.WriteCloser {
	buf, _ := bufPool.Get().(*bytes.Buffer)
	if buf == nil {
		buf = new(bytes.Buffer)
	}

	if h.UseJournal {
		return &journalOutput{buf, r}
	}

	return &stderrOutput{buf}
}

func (h Handler) Handle(ctx context.Context, r slog.Record) error {
	attrs := slices.Grow(h.attrs, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	if h.group != "" {
		group := make([]any, 0, len(attrs))
		for _, a := range attrs {
			group = append(group, a)
		}
		attrs = []slog.Attr{slog.Group(h.group, group...)}
	}

	w := h.writer(r)

	if !h.UseJournal {
		fmt.Fprintf(
			w,
			"%v %v ",
			styleTime.Render(r.Time.Format(time.StampMilli)),
			styleLevel(r.Level).Render(r.Level.String()),
		)
	}

	fmt.Fprintf(
		w,
		"%v\n",
		r.Message,
	)
	for _, attr := range attrs {
		fmt.Fprintf(
			w,
			"\t%v=%v\n",
			styleKey.Render(quoteIfNecessary(attr.Key)),
			styleValue.Render(quoteIfNecessary(attr.Value.String())),
		)
	}

	return w.Close()
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
