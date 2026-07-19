package main

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"deedles.dev/ptt-fix/internal/config"
	"deedles.dev/ptt-fix/internal/xdo"
)

// Ensures default and representative configs still produce the same sym shapes
// the sender path expects, and that key names resolve via the shipped keysym table.
func TestConfigSymsResolveForSenders(t *testing.T) {
	cases := []struct {
		name string
		src  string
		typ  string
		val  string
	}{
		{
			name: "default embedded",
			src:  config.DefaultFile(),
			typ:  "key",
			val:  "Alt_L",
		},
		{
			name: "mouse button",
			src:  "key 56\nsym mouse 2\nretry 1s\ndevice /dev/null\n",
			typ:  "mouse",
			val:  "2",
		},
		{
			name: "control key",
			src:  "key 29\nsym Control_L\nretry 1s\ndevice /dev/null\n",
			typ:  "key",
			val:  "Control_L",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := config.Parse(strings.NewReader(tc.src))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if c.Sym.Type != tc.typ || c.Sym.Val != tc.val {
				t.Fatalf("Sym = %+v, want %s/%s", c.Sym, tc.typ, tc.val)
			}
			switch c.Sym.Type {
			case "key":
				if _, ok := xdo.KeysymByName(c.Sym.Val); !ok {
					t.Fatalf("keysym %q not in shipped table", c.Sym.Val)
				}
			case "mouse":
				if err := validMouseButton(2); err != nil {
					t.Fatalf("mouse validation: %v", err)
				}
			}
		})
	}
}

func TestNewSender_invalidMouseButton(t *testing.T) {
	// newSender validates mouse buttons without needing a live display when
	// the type is mouse (no Keycodes call). Use a nil *xdo.Xdo — only reached
	// for key syms.
	_, err := newSender(nil, config.Sym{Type: "mouse", Val: "0"})
	if err == nil {
		t.Fatal("expected invalid button 0")
	}
	_, err = newSender(nil, config.Sym{Type: "mouse", Val: "256"})
	if err == nil {
		t.Fatal("expected invalid button 256")
	}
	_, err = newSender(nil, config.Sym{Type: "mouse", Val: "2"})
	if err != nil {
		t.Fatalf("button 2 should be valid: %v", err)
	}
}

type stubSender struct {
	upErr, downErr error
	ups, downs     int
}

func (s *stubSender) Up() error {
	s.ups++
	return s.upErr
}

func (s *stubSender) Down() error {
	s.downs++
	return s.downErr
}

func TestApplyEvent_propagatesInjectionErrors(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	s := &stubSender{upErr: errors.New("xtest failed")}
	err := applyEvent(logger, s, event{Type: eventUp, Device: "kbd0"})
	if err == nil {
		t.Fatal("expected error from Up")
	}
	if !strings.Contains(err.Error(), "xtest failed") {
		t.Fatalf("error should wrap injection failure: %v", err)
	}
	if !strings.Contains(err.Error(), "kbd0") {
		t.Fatalf("error should mention device: %v", err)
	}
	if s.ups != 1 || s.downs != 0 {
		t.Fatalf("ups=%d downs=%d", s.ups, s.downs)
	}
	// Failed injection must not log success.
	if strings.Contains(buf.String(), "deactivated") {
		t.Fatalf("should not log deactivated on failure: %q", buf.String())
	}

	buf.Reset()
	s = &stubSender{downErr: errors.New("conn closed")}
	err = applyEvent(logger, s, event{Type: eventDown, Device: "mouse0"})
	if err == nil || !strings.Contains(err.Error(), "conn closed") {
		t.Fatalf("expected down error, got %v", err)
	}

	buf.Reset()
	s = &stubSender{}
	if err := applyEvent(logger, s, event{Type: eventDown, Device: "d1"}); err != nil {
		t.Fatal(err)
	}
	if err := applyEvent(logger, s, event{Type: eventUp, Device: "d1"}); err != nil {
		t.Fatal(err)
	}
	if s.downs != 1 || s.ups != 1 {
		t.Fatalf("ups=%d downs=%d", s.ups, s.downs)
	}
	out := buf.String()
	if !strings.Contains(out, "activated") || !strings.Contains(out, "deactivated") {
		t.Fatalf("expected success logs, got %q", out)
	}
}

func TestApplyEvent_invalidType(t *testing.T) {
	logger := slog.Default()
	err := applyEvent(logger, &stubSender{}, event{Type: eventInvalid})
	if err == nil {
		t.Fatal("expected invalid event error")
	}
}
