package config

import (
	"strings"
	"testing"
	"time"
)

func TestParse_defaultStyle(t *testing.T) {
	// Mirrors embedded default semantics (key/sym/retry/device) without relying
	// on host-specific device globs expanding the same way.
	src := `
key 56
sym Alt_L
retry 10s
device /dev/null
`
	c, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Key != 56 {
		t.Errorf("Key = %d, want 56", c.Key)
	}
	if c.Sym.Type != "key" || c.Sym.Val != "Alt_L" {
		t.Errorf("Sym = %+v, want key/Alt_L", c.Sym)
	}
	if c.Retry != 10*time.Second {
		t.Errorf("Retry = %v, want 10s", c.Retry)
	}
	if len(c.Devices) != 1 || c.Devices[0] != "/dev/null" {
		t.Errorf("Devices = %v, want [/dev/null]", c.Devices)
	}
}

func TestParse_mouseSym(t *testing.T) {
	src := `
key 0x1c
sym mouse 2
retry 1s
device /dev/null
`
	c, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Key != 0x1c {
		t.Errorf("Key = %d, want 28", c.Key)
	}
	if c.Sym.Type != "mouse" || c.Sym.Val != "2" {
		t.Errorf("Sym = %+v, want mouse/2", c.Sym)
	}
}

func TestParse_multiDevice(t *testing.T) {
	src := `
key 56
sym Control_L
retry 0s
device /dev/null
device /dev/zero
`
	c, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(c.Devices) < 2 {
		t.Fatalf("Devices = %v, want at least 2 entries", c.Devices)
	}
	// Order of append from multiple device directives.
	if c.Devices[0] != "/dev/null" || c.Devices[1] != "/dev/zero" {
		t.Errorf("Devices = %v, want [/dev/null /dev/zero ...]", c.Devices)
	}
	if c.Sym.Type != "key" || c.Sym.Val != "Control_L" {
		t.Errorf("Sym = %+v", c.Sym)
	}
}

func TestParse_defaultFile(t *testing.T) {
	// Embedded default must still parse (device globs may expand to 0+ paths).
	c, err := Parse(strings.NewReader(DefaultFile()))
	if err != nil {
		t.Fatalf("Parse(DefaultFile): %v", err)
	}
	if c.Key != 56 {
		t.Errorf("default Key = %d, want 56 (KEY_LEFTALT)", c.Key)
	}
	if c.Sym.Type != "key" || c.Sym.Val != "Alt_L" {
		t.Errorf("default Sym = %+v, want key/Alt_L", c.Sym)
	}
	if c.Retry != 10*time.Second {
		t.Errorf("default Retry = %v, want 10s", c.Retry)
	}
}

func TestParse_unknownDirective(t *testing.T) {
	_, err := Parse(strings.NewReader("nope 1\n"))
	if err == nil {
		t.Fatal("expected error for unknown directive")
	}
}
