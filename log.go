package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/exp/slices"
	"golang.org/x/exp/slog"
)

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

type GlossyHandler struct {
	Level slog.Level

	attrs []slog.Attr
	group string
}

func (h GlossyHandler) render(v slog.Value) string {
	return h.renderString(v.String())
}

func (h GlossyHandler) renderString(str string) string {
	for _, c := range str {
		if unicode.IsSpace(c) {
			return strconv.Quote(str)
		}
	}
	return str
}

func (h GlossyHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.Level
}

func (h GlossyHandler) Handle(r slog.Record) error {
	attrs := slices.Grow(h.attrs, r.NumAttrs())
	r.Attrs(func(a slog.Attr) {
		attrs = append(attrs, a)
	})
	if h.group != "" {
		attrs = []slog.Attr{slog.Group(h.group, attrs...)}
	}

	fmt.Fprintf(
		os.Stderr,
		"%v %v %v\n",
		styleTime.Render(r.Time.Format(time.StampMilli)),
		styleLevel(r.Level).Render(r.Level.String()),
		r.Message,
	)
	for _, attr := range attrs {
		fmt.Fprintf(
			os.Stderr,
			"\t%v=%v\n",
			styleKey.Render(h.renderString(attr.Key)),
			styleValue.Render(h.render(attr.Value)),
		)
	}

	return nil
}

func (h GlossyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h.attrs = slices.Clip(append(h.attrs, attrs...))
	return h
}

func (h GlossyHandler) WithGroup(name string) slog.Handler {
	h.group = name
	return h
}