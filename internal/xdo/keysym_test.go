package xdo

import (
	"strings"
	"testing"
)

func TestKeysymByName_commonPTT(t *testing.T) {
	// Values from X11/keysymdef.h — must match what libxdo/xkb accepted.
	cases := map[string]uint32{
		"Alt_L":            0xffe9,
		"Alt_R":            0xffea,
		"Control_L":        0xffe3,
		"Control_R":        0xffe4,
		"Shift_L":          0xffe1,
		"Shift_R":          0xffe2,
		"Super_L":          0xffeb,
		"Super_R":          0xffec,
		"Meta_L":           0xffe7,
		"Meta_R":           0xffe8,
		"space":            0x0020,
		"Return":           0xff0d,
		"Tab":              0xff09,
		"Escape":           0xff1b,
		"Caps_Lock":        0xffe5,
		"F1":               0xffbe,
		"F13":              0xffca,
		"a":                0x0061,
		"A":                0x0041,
		"ISO_Level3_Shift": 0xfe03,
	}
	for name, want := range cases {
		got, ok := KeysymByName(name)
		if !ok {
			t.Errorf("KeysymByName(%q) missing", name)
			continue
		}
		if got != want {
			t.Errorf("KeysymByName(%q) = 0x%x, want 0x%x", name, got, want)
		}
	}
}

func TestKeysymByName_prefixes(t *testing.T) {
	base, ok := KeysymByName("Alt_L")
	if !ok {
		t.Fatal("Alt_L missing")
	}
	for _, name := range []string{"XK_Alt_L", "XKB_KEY_Alt_L"} {
		got, ok := KeysymByName(name)
		if !ok || got != base {
			t.Errorf("KeysymByName(%q) = (%x, %v), want (%x, true)", name, got, ok, base)
		}
	}
}

func TestKeysymByName_unknown(t *testing.T) {
	if _, ok := KeysymByName("NotARealKeysym_XYZ"); ok {
		t.Fatal("expected unknown keysym to fail")
	}
	if _, ok := KeysymByName(""); ok {
		t.Fatal("expected empty name to fail")
	}
}

func TestSplitKeysequence(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"Alt_L", []string{"Alt_L"}},
		{"Control_L+Alt_L", []string{"Control_L", "Alt_L"}},
		{"  Shift_L + a ", []string{"Shift_L", "a"}},
		{"", nil},
	}
	for _, tc := range cases {
		got := splitKeysequence(tc.in)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("splitKeysequence(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestMouseButtonRange(t *testing.T) {
	// Button validation is pure; use a nil-safe check via ButtonDown error path
	// without needing a display: invalid range only.
	x := &Xdo{}
	if err := x.ButtonDown(0); err == nil {
		t.Error("button 0 should be invalid")
	}
	if err := x.ButtonDown(256); err == nil {
		t.Error("button 256 should be invalid")
	}
	if err := x.ButtonUp(-1); err == nil {
		t.Error("negative button should be invalid")
	}
}
