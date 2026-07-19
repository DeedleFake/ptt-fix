package xdo

import (
	"errors"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
	for _, name := range []string{"XK_Alt_L", "XKB_KEY_Alt_L", "XF86XK_AudioMute"} {
		got, ok := KeysymByName(name)
		if name == "XF86XK_AudioMute" {
			want, wantOK := KeysymByName("AudioMute")
			if !ok || !wantOK || got != want {
				t.Errorf("KeysymByName(%q) = (%x, %v), want AudioMute (%x, %v)", name, got, ok, want, wantOK)
			}
			continue
		}
		if !ok || got != base {
			t.Errorf("KeysymByName(%q) = (%x, %v), want (%x, true)", name, got, ok, base)
		}
	}
}

func TestStripKeysymPrefix_longestOnly(t *testing.T) {
	// Single longest-match strip: do not strip XK_ after XKB_KEY_.
	if got := stripKeysymPrefix("XKB_KEY_Alt_L"); got != "Alt_L" {
		t.Fatalf("XKB_KEY_Alt_L -> %q, want Alt_L", got)
	}
	if got := stripKeysymPrefix("XK_Alt_L"); got != "Alt_L" {
		t.Fatalf("XK_Alt_L -> %q, want Alt_L", got)
	}
	if got := stripKeysymPrefix("XF86XK_AudioMute"); got != "AudioMute" {
		t.Fatalf("XF86XK_AudioMute -> %q, want AudioMute", got)
	}
	// Nested/pathological: only one prefix, so XKB_KEY_XK_Foo becomes XK_Foo
	// (not Foo). Lookup would then fail unless XK_Foo exists as a name.
	if got := stripKeysymPrefix("XKB_KEY_XK_Alt_L"); got != "XK_Alt_L" {
		t.Fatalf("XKB_KEY_XK_Alt_L -> %q, want XK_Alt_L (single strip)", got)
	}
	if got := stripKeysymPrefix("Alt_L"); got != "Alt_L" {
		t.Fatalf("Alt_L -> %q, want unchanged", got)
	}
}

func TestKeysymByName_exactOnly(t *testing.T) {
	// Case-sensitive: multi-segment names must match exactly.
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

func TestValidButton(t *testing.T) {
	if err := ValidButton(0); err == nil {
		t.Error("button 0 should be invalid")
	}
	if err := ValidButton(256); err == nil {
		t.Error("button 256 should be invalid")
	}
	if err := ValidButton(-1); err == nil {
		t.Error("negative button should be invalid")
	}
	if err := ValidButton(1); err != nil {
		t.Errorf("button 1: %v", err)
	}
	if err := ValidButton(255); err != nil {
		t.Errorf("button 255: %v", err)
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

func TestKeycodes_nilReceiver(t *testing.T) {
	var x *Xdo
	_, err := x.Keycodes("Alt_L")
	if err == nil {
		t.Fatal("nil *Xdo Keycodes must return an error")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Fatalf("error should mention closed connection: %v", err)
	}
}

func TestKeyDown_partialFailureReleasesPressed(t *testing.T) {
	pressErr := errors.New("press failed on second key")
	var events []struct {
		evType byte
		detail byte
	}
	presses := 0
	x := &Xdo{
		input: func(evType, detail byte) error {
			events = append(events, struct {
				evType byte
				detail byte
			}{evType, detail})
			if evType == xproto.KeyPress {
				presses++
				if presses == 2 {
					return pressErr
				}
			}
			return nil
		},
	}

	err := x.KeyDown([]byte{10, 20, 30})
	if !errors.Is(err, pressErr) {
		t.Fatalf("KeyDown error = %v, want pressErr", err)
	}

	// Expect: Press 10, Press 20 (fail), Release 10 (best-effort reverse).
	if len(events) != 3 {
		t.Fatalf("events = %v (len %d), want 3", events, len(events))
	}
	if events[0].evType != xproto.KeyPress || events[0].detail != 10 {
		t.Fatalf("event0 = %+v, want KeyPress 10", events[0])
	}
	if events[1].evType != xproto.KeyPress || events[1].detail != 20 {
		t.Fatalf("event1 = %+v, want KeyPress 20", events[1])
	}
	if events[2].evType != xproto.KeyRelease || events[2].detail != 10 {
		t.Fatalf("event2 = %+v, want KeyRelease 10", events[2])
	}
}

func TestKeyDown_singleKeyFailureNoRelease(t *testing.T) {
	pressErr := errors.New("only key failed")
	var events []byte // details only; all should be presses
	x := &Xdo{
		input: func(evType, detail byte) error {
			if evType != xproto.KeyPress {
				t.Errorf("unexpected event type %d (detail %d)", evType, detail)
			}
			events = append(events, detail)
			return pressErr
		},
	}
	err := x.KeyDown([]byte{42})
	if !errors.Is(err, pressErr) {
		t.Fatalf("error = %v, want pressErr", err)
	}
	if len(events) != 1 || events[0] != 42 {
		t.Fatalf("events = %v, want single press of 42", events)
	}
}

func TestKeyDown_partialFailureMultipleReleasesReverseOrder(t *testing.T) {
	pressErr := errors.New("fail third")
	var events []struct {
		evType byte
		detail byte
	}
	presses := 0
	x := &Xdo{
		input: func(evType, detail byte) error {
			events = append(events, struct {
				evType byte
				detail byte
			}{evType, detail})
			if evType == xproto.KeyPress {
				presses++
				if presses == 3 {
					return pressErr
				}
			}
			return nil
		},
	}

	err := x.KeyDown([]byte{1, 2, 3, 4})
	if !errors.Is(err, pressErr) {
		t.Fatalf("error = %v, want pressErr", err)
	}
	// Press 1,2,3(fail); Release 2, Release 1
	want := []struct {
		evType byte
		detail byte
	}{
		{xproto.KeyPress, 1},
		{xproto.KeyPress, 2},
		{xproto.KeyPress, 3},
		{xproto.KeyRelease, 2},
		{xproto.KeyRelease, 1},
	}
	if len(events) != len(want) {
		t.Fatalf("events len = %d, want %d: %v", len(events), len(want), events)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("event[%d] = %+v, want %+v", i, events[i], want[i])
		}
	}
}

func TestClose_stopsCleanup(t *testing.T) {
	// If Close omits cleanup.Stop(), GC of the Xdo value runs the cleanup and
	// increments n. With Stop, n stays 0.
	var n atomic.Int32
	func() {
		x := &Xdo{}
		x.cleanup = runtime.AddCleanup(x, func(*atomic.Int32) {
			n.Add(1)
		}, &n)
		x.Close()
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		if n.Load() != 0 {
			t.Fatalf("cleanup ran after Close; Close must Stop the cleanup")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n.Load() != 0 {
		t.Fatalf("cleanup ran %d times after Close+Stop", n.Load())
	}
}
