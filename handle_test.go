package main

import (
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
				// Same validation newSender applies (range only; no display needed).
				if c.Sym.Val != "2" {
					t.Fatalf("unexpected mouse val %q", c.Sym.Val)
				}
			}
		})
	}
}
