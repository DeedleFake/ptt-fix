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
	"golang.org/x/exp/slices"
	"golang.org/x/exp/slog"
)

var bufPool sync.Pool

var (
	styleTime  = lipgloss.NewStyle().Foreground(lipgloss.Color("#AAAAAA"))
	styleKey   = lipgloss.NewStyle().Foreground(lipgloss.Color("#AAAAAA"))
	styleValue = lipgloss.NewStyle()

	styleError = lipgloss.NewStyle().Foreground(lipgloss.Color("#EE0000"))
	styleWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("#EEEE00"))
	styleInfo  = lipgloss.NewStyle().Foreground(lipgloss.Color("#5555EE"))
	styleDebug = lipgloss.NewStyle().Foreground(lipgloss.Color("#00EE00"))
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
	Level slog.Level

	attrs []slog.Attr
	group string
}

func render(v slog.Value) string {
	return renderString(v.String())
}

func renderString(str string) string {
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

func (h Handler) Handle(r slog.Record) error {
	attrs := slices.Grow(h.attrs, r.NumAttrs())
	r.Attrs(func(a slog.Attr) {
		attrs = append(attrs, a)
	})
	if h.group != "" {
		attrs = []slog.Attr{slog.Group(h.group, attrs...)}
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
			styleKey.Render(renderString(attr.Key)),
			styleValue.Render(render(attr.Value)),
		)
	}

	_, err := io.Copy(os.Stderr, buf)
	return err
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
