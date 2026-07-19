package xdo

import (
	"strings"
	"testing"

	"github.com/jezek/xgb/xproto"
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

func TestKeysymByName_exactOnly(t *testing.T) {
	// Case folding was removed: multi-segment names must match exactly.
	if _, ok := KeysymByName("alt_l"); ok {
		t.Error("alt_l must not resolve (exact names only)")
	}
	if _, ok := KeysymByName("ALT_L"); ok {
		t.Error("ALT_L must not resolve (exact names only)")
	}
	if _, ok := KeysymByName("Alt_l"); ok {
		t.Error("Alt_l must not resolve (exact names only)")
	}
	// Canonical form still works.
	if _, ok := KeysymByName("Alt_L"); !ok {
		t.Error("Alt_L must resolve")
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

func TestKeycodeForKeysym_baseLevelOnly(t *testing.T) {
	// Synthetic map without a display:
	// keycode min+0: base 'a' (0x61), level1 'A' (0x41)
	// keycode min+1: base NoSymbol (0), level1 Alt_L (0xffe9)
	const (
		symA      = xproto.Keysym(0x61)
		symShiftA = xproto.Keysym(0x41)
		symAltL   = xproto.Keysym(0xffe9)
	)
	x := &Xdo{
		min:               8,
		max:               9,
		keysymsPerKeycode: 2,
		keyMap: []xproto.Keysym{
			symA, symShiftA,
			0, symAltL,
		},
	}

	kc, err := x.keycodeForKeysym(symA)
	if err != nil {
		t.Fatalf("base 'a': %v", err)
	}
	if kc != 8 {
		t.Fatalf("base 'a' keycode = %d, want 8", kc)
	}

	_, err = x.keycodeForKeysym(symShiftA)
	if err == nil {
		t.Fatal("shifted-only 'A' must be rejected")
	}
	if !strings.Contains(err.Error(), "modifiers") {
		t.Fatalf("shifted-only error should mention modifiers, got: %v", err)
	}

	_, err = x.keycodeForKeysym(symAltL)
	if err == nil {
		t.Fatal("non-base Alt_L must be rejected")
	}

	_, err = x.keycodeForKeysym(0x123456) // unmapped
	if err == nil {
		t.Fatal("unmapped keysym must fail")
	}
	if strings.Contains(err.Error(), "modifiers") {
		t.Fatalf("unmapped should not claim modifiers-only: %v", err)
	}
}

func TestKeycodes_usesBaseLevelPolicy(t *testing.T) {
	// Keycodes() wires name lookup to base-level keycode resolution.
	const symA = xproto.Keysym(0x61)
	x := &Xdo{
		min:               8,
		max:               8,
		keysymsPerKeycode: 2,
		keyMap:            []xproto.Keysym{symA, 0x41},
	}
	kcs, err := x.Keycodes("a")
	if err != nil {
		t.Fatalf("Keycodes(a): %v", err)
	}
	if len(kcs) != 1 || kcs[0] != 8 {
		t.Fatalf("Keycodes(a) = %v, want [8]", kcs)
	}
	_, err = x.Keycodes("A")
	if err == nil {
		t.Fatal("Keycodes(A) must fail (shifted-only on this map)")
	}
}
