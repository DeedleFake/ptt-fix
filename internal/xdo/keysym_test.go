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
	// No conn: uses the synthetic map as-is (live connections re-fetch first).
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

func TestKeycodes_seesMapUpdatesWithoutConn(t *testing.T) {
	// When the map is replaced (as refreshKeyboardMap does on a live display),
	// subsequent Keycodes calls resolve against the new table.
	const (
		symAlt  = xproto.Keysym(0xffe9)
		symCtrl = xproto.Keysym(0xffe3)
	)
	x := &Xdo{
		min:               8,
		max:               9,
		keysymsPerKeycode: 1,
		keyMap:            []xproto.Keysym{symAlt, symCtrl},
	}
	kcs, err := x.Keycodes("Alt_L")
	if err != nil || len(kcs) != 1 || kcs[0] != 8 {
		t.Fatalf("initial Alt_L = %v, %v; want [8]", kcs, err)
	}
	x.keyMap = []xproto.Keysym{symCtrl, symAlt}
	kcs, err = x.Keycodes("Alt_L")
	if err != nil || len(kcs) != 1 || kcs[0] != 9 {
		t.Fatalf("after remap Alt_L = %v, %v; want [9]", kcs, err)
	}
}

func TestKeyBinding_upReleasesKeycodesFromDown(t *testing.T) {
	// Mid-hold map change must not change which keycodes Up releases.
	const (
		symAlt  = xproto.Keysym(0xffe9)
		symCtrl = xproto.Keysym(0xffe3)
	)
	var events []struct {
		evType byte
		detail byte
	}
	x := &Xdo{
		min:               8,
		max:               9,
		keysymsPerKeycode: 1,
		keyMap:            []xproto.Keysym{symAlt, symCtrl},
		input: func(evType, detail byte) error {
			events = append(events, struct {
				evType byte
				detail byte
			}{evType, detail})
			return nil
		},
	}
	b, err := x.BindKeys("Alt_L")
	if err != nil {
		t.Fatalf("BindKeys: %v", err)
	}
	if err := b.Down(); err != nil {
		t.Fatalf("Down: %v", err)
	}
	// Swap so Alt_L would now resolve to keycode 9.
	x.keyMap = []xproto.Keysym{symCtrl, symAlt}
	if err := b.Up(); err != nil {
		t.Fatalf("Up: %v", err)
	}
	want := []struct {
		evType byte
		detail byte
	}{
		{xproto.KeyPress, 8},
		{xproto.KeyRelease, 8},
	}
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("event[%d] = %+v, want %+v", i, events[i], want[i])
		}
	}
}

func TestKeyBinding_downUsesCurrentMap(t *testing.T) {
	const (
		symAlt  = xproto.Keysym(0xffe9)
		symCtrl = xproto.Keysym(0xffe3)
	)
	var pressed []byte
	x := &Xdo{
		min:               8,
		max:               9,
		keysymsPerKeycode: 1,
		keyMap:            []xproto.Keysym{symAlt, symCtrl},
		input: func(evType, detail byte) error {
			if evType == xproto.KeyPress {
				pressed = append(pressed, detail)
			}
			return nil
		},
	}
	b, err := x.BindKeys("Alt_L")
	if err != nil {
		t.Fatalf("BindKeys: %v", err)
	}
	x.keyMap = []xproto.Keysym{symCtrl, symAlt}
	if err := b.Down(); err != nil {
		t.Fatalf("Down: %v", err)
	}
	if len(pressed) != 1 || pressed[0] != 9 {
		t.Fatalf("pressed = %v, want [9] after remap", pressed)
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

func TestKeyUp_partialFailureContinuesReleases(t *testing.T) {
	releaseErr := errors.New("release failed on middle key")
	var events []struct {
		evType byte
		detail byte
	}
	x := &Xdo{
		input: func(evType, detail byte) error {
			events = append(events, struct {
				evType byte
				detail byte
			}{evType, detail})
			// Reverse order: 30, 20, 10 — fail on 20, still release 10.
			if evType == xproto.KeyRelease && detail == 20 {
				return releaseErr
			}
			return nil
		},
	}

	err := x.KeyUp([]byte{10, 20, 30})
	if !errors.Is(err, releaseErr) {
		t.Fatalf("KeyUp error = %v, want releaseErr", err)
	}
	want := []struct {
		evType byte
		detail byte
	}{
		{xproto.KeyRelease, 30},
		{xproto.KeyRelease, 20},
		{xproto.KeyRelease, 10},
	}
	if len(events) != len(want) {
		t.Fatalf("events = %v (len %d), want %d", events, len(events), len(want))
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

func TestScratch_bindsUnmappedOnEmptyKeycode(t *testing.T) {
	// keycode 8: base 'a'; keycode 9: fully empty → unmapped Alt_R gets 9.
	const (
		symA    = xproto.Keysym(0x61)
		symAltR = xproto.Keysym(0xffea)
	)
	var changes []struct {
		kc   xproto.Keycode
		syms []xproto.Keysym
	}
	x := &Xdo{
		min:               8,
		max:               9,
		keysymsPerKeycode: 2,
		keyMap: []xproto.Keysym{
			symA, 0x41,
			0, 0,
		},
		changeMapping: func(kc xproto.Keycode, keysyms []xproto.Keysym) error {
			syms := append([]xproto.Keysym(nil), keysyms...)
			changes = append(changes, struct {
				kc   xproto.Keycode
				syms []xproto.Keysym
			}{kc, syms})
			return nil
		},
	}

	kc, err := x.keycodeForKeysym(symAltR)
	if err != nil {
		t.Fatalf("unmapped Alt_R with empty slot: %v", err)
	}
	if kc != 9 {
		t.Fatalf("scratch keycode = %d, want 9", kc)
	}
	if x.baseKeysym(9) != symAltR {
		t.Fatalf("local map base[9] = 0x%x, want Alt_R", x.baseKeysym(9))
	}
	if len(changes) != 1 || changes[0].kc != 9 || changes[0].syms[0] != symAltR {
		t.Fatalf("changeMapping calls = %+v, want one bind of Alt_R on 9", changes)
	}
	if _, ok := x.scratches[symAltR]; !ok {
		t.Fatal("expected scratch recorded for Alt_R")
	}

	// Same keysym reuses the scratch (no second ChangeKeyboardMapping).
	kc2, err := x.keycodeForKeysym(symAltR)
	if err != nil {
		t.Fatalf("reuse: %v", err)
	}
	if kc2 != 9 {
		t.Fatalf("reuse keycode = %d, want 9", kc2)
	}
	if len(changes) != 1 {
		t.Fatalf("reuse must not re-bind; changeMapping calls = %d", len(changes))
	}
}

func TestScratch_prefersExistingBaseOverEmpty(t *testing.T) {
	// Alt_L already on base of keycode 8; empty slot at 9 must not be used.
	const symAltL = xproto.Keysym(0xffe9)
	var nChanges int
	x := &Xdo{
		min:               8,
		max:               9,
		keysymsPerKeycode: 1,
		keyMap:            []xproto.Keysym{symAltL, 0},
		changeMapping: func(xproto.Keycode, []xproto.Keysym) error {
			nChanges++
			return nil
		},
	}
	kc, err := x.keycodeForKeysym(symAltL)
	if err != nil {
		t.Fatalf("mapped Alt_L: %v", err)
	}
	if kc != 8 {
		t.Fatalf("keycode = %d, want 8 (existing base), not empty slot", kc)
	}
	if nChanges != 0 {
		t.Fatalf("must not scratch-bind when base exists; changes = %d", nChanges)
	}
	if len(x.scratches) != 0 {
		t.Fatalf("scratches = %v, want empty", x.scratches)
	}
}

func TestScratch_noEmptyKeycodeError(t *testing.T) {
	// Every keycode has at least one non-zero column → no scratch slot.
	const symA = xproto.Keysym(0x61)
	x := &Xdo{
		min:               8,
		max:               9,
		keysymsPerKeycode: 2,
		keyMap: []xproto.Keysym{
			symA, 0x41,
			0, 0xffe9, // base empty but level1 occupied → not fully empty
		},
	}
	_, err := x.keycodeForKeysym(0x123456)
	if err == nil {
		t.Fatal("unmapped keysym with no fully empty keycode must fail")
	}
	if !strings.Contains(err.Error(), "no empty keycode available for scratch binding") {
		t.Fatalf("error should mention empty keycode / scratch binding, got: %v", err)
	}
	if strings.Contains(err.Error(), "modifiers") {
		t.Fatalf("should not claim modifiers-only: %v", err)
	}
	if len(x.scratches) != 0 {
		t.Fatalf("scratches = %v, want empty", x.scratches)
	}
}

func TestScratch_multipleUnmappedKeysyms(t *testing.T) {
	const (
		symAltR   = xproto.Keysym(0xffea)
		symSuperR = xproto.Keysym(0xffec)
		symA      = xproto.Keysym(0x61)
	)
	x := &Xdo{
		min:               8,
		max:               10,
		keysymsPerKeycode: 1,
		keyMap:            []xproto.Keysym{symA, 0, 0},
	}
	kc1, err := x.keycodeForKeysym(symAltR)
	if err != nil {
		t.Fatalf("Alt_R: %v", err)
	}
	if kc1 != 10 {
		t.Fatalf("Alt_R keycode = %d, want 10 (high keycode first)", kc1)
	}
	kc2, err := x.keycodeForKeysym(symSuperR)
	if err != nil {
		t.Fatalf("Super_R: %v", err)
	}
	if kc2 != 9 {
		t.Fatalf("Super_R keycode = %d, want 9 (next highest empty)", kc2)
	}
	if len(x.scratches) != 2 {
		t.Fatalf("scratches len = %d, want 2", len(x.scratches))
	}
	// Third unmapped keysym: no empty slots left.
	_, err = x.keycodeForKeysym(0x123456)
	if err == nil {
		t.Fatal("third unmapped keysym must fail when slots exhausted")
	}
	if !strings.Contains(err.Error(), "no empty keycode available for scratch binding") {
		t.Fatalf("exhaustion error = %v", err)
	}
}

func TestScratch_closeRestoresMapping(t *testing.T) {
	const (
		symA    = xproto.Keysym(0x61)
		symAltR = xproto.Keysym(0xffea)
	)
	var changes []struct {
		kc   xproto.Keycode
		syms []xproto.Keysym
	}
	x := &Xdo{
		min:               8,
		max:               9,
		keysymsPerKeycode: 2,
		keyMap: []xproto.Keysym{
			symA, 0,
			0, 0,
		},
		changeMapping: func(kc xproto.Keycode, keysyms []xproto.Keysym) error {
			syms := append([]xproto.Keysym(nil), keysyms...)
			changes = append(changes, struct {
				kc   xproto.Keycode
				syms []xproto.Keysym
			}{kc, syms})
			return nil
		},
	}
	if _, err := x.keycodeForKeysym(symAltR); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if x.baseKeysym(9) != symAltR {
		t.Fatalf("before Close base[9] = 0x%x", x.baseKeysym(9))
	}
	x.Close()
	if len(x.scratches) != 0 {
		t.Fatalf("after Close scratches = %v, want nil/empty", x.scratches)
	}
	if x.baseKeysym(9) != 0 {
		t.Fatalf("after Close base[9] = 0x%x, want NoSymbol", x.baseKeysym(9))
	}
	// Last changeMapping call should restore previous (zeros) on keycode 9.
	if len(changes) < 2 {
		t.Fatalf("expected bind + restore; changes = %+v", changes)
	}
	last := changes[len(changes)-1]
	if last.kc != 9 || last.syms[0] != 0 || last.syms[1] != 0 {
		t.Fatalf("restore call = %+v, want keycode 9 all NoSymbol", last)
	}
}

func TestScratch_keycodesNamePath(t *testing.T) {
	// Keycodes("Alt_R") with empty slot succeeds (the v0.9.0 regression).
	x := &Xdo{
		min:               8,
		max:               9,
		keysymsPerKeycode: 1,
		keyMap:            []xproto.Keysym{0x61, 0}, // 'a' + empty
	}
	kcs, err := x.Keycodes("Alt_R")
	if err != nil {
		t.Fatalf("Keycodes(Alt_R): %v", err)
	}
	if len(kcs) != 1 || kcs[0] != 9 {
		t.Fatalf("Keycodes(Alt_R) = %v, want [9]", kcs)
	}
}

func TestScratch_ensureAfterMapReload(t *testing.T) {
	// After a synthetic map reload that drops the scratch, ensureScratches
	// re-applies it onto the same empty keycode.
	const symAltR = xproto.Keysym(0xffea)
	x := &Xdo{
		min:               8,
		max:               9,
		keysymsPerKeycode: 1,
		keyMap:            []xproto.Keysym{0x61, 0},
	}
	kc, err := x.keycodeForKeysym(symAltR)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if kc != 9 {
		t.Fatalf("keycode = %d, want 9", kc)
	}
	// Simulate refresh that lost the scratch mapping (empty again).
	x.keyMap = []xproto.Keysym{0x61, 0}
	if err := x.ensureScratches(); err != nil {
		t.Fatalf("ensureScratches: %v", err)
	}
	if x.baseKeysym(9) != symAltR {
		t.Fatalf("after ensure base[9] = 0x%x, want Alt_R", x.baseKeysym(9))
	}
	if s, ok := x.scratches[symAltR]; !ok || s.keycode != 9 {
		t.Fatalf("scratch entry = %v, %v; want keycode 9", s, ok)
	}
}

func TestScratch_nonBaseRejectedEvenWithEmptySlot(t *testing.T) {
	// Alt_L only on non-base of keycode 8; keycode 9 fully empty.
	// Must reject with modifiers error and must not scratch-bind.
	const symAltL = xproto.Keysym(0xffe9)
	var nChanges int
	x := &Xdo{
		min:               8,
		max:               9,
		keysymsPerKeycode: 2,
		keyMap: []xproto.Keysym{
			0, symAltL, // non-base only
			0, 0, // fully empty
		},
		changeMapping: func(xproto.Keycode, []xproto.Keysym) error {
			nChanges++
			return nil
		},
	}
	_, err := x.keycodeForKeysym(symAltL)
	if err == nil {
		t.Fatal("non-base Alt_L must fail even when empty slot exists")
	}
	if !strings.Contains(err.Error(), "modifiers") {
		t.Fatalf("error should mention modifiers, got: %v", err)
	}
	if nChanges != 0 {
		t.Fatalf("must not call changeMapping; calls = %d", nChanges)
	}
	if len(x.scratches) != 0 {
		t.Fatalf("scratches = %v, want empty", x.scratches)
	}
}

func TestScratch_ensureRebindsWhenSlotTaken(t *testing.T) {
	// After bind on 10, occupy 10 and leave 9 empty; ensure moves scratch to 9.
	const (
		symAltR = xproto.Keysym(0xffea)
		symA    = xproto.Keysym(0x61)
		symB    = xproto.Keysym(0x62)
	)
	x := &Xdo{
		min:               8,
		max:               10,
		keysymsPerKeycode: 1,
		keyMap:            []xproto.Keysym{symA, 0, 0},
	}
	kc, err := x.keycodeForKeysym(symAltR)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if kc != 10 {
		t.Fatalf("keycode = %d, want 10", kc)
	}
	// Slot 10 taken by something else; 9 still empty; 8 remains 'a'.
	x.keyMap = []xproto.Keysym{symA, 0, symB}
	if err := x.ensureScratches(); err != nil {
		t.Fatalf("ensureScratches: %v", err)
	}
	s, ok := x.scratches[symAltR]
	if !ok || s.keycode != 9 {
		t.Fatalf("scratch = %v, %v; want keycode 9", s, ok)
	}
	if x.baseKeysym(9) != symAltR {
		t.Fatalf("base[9] = 0x%x, want Alt_R", x.baseKeysym(9))
	}
	if x.baseKeysym(10) != symB {
		t.Fatalf("base[10] = 0x%x, want b (untouched)", x.baseKeysym(10))
	}
	x.Close()
	if x.baseKeysym(9) != 0 {
		t.Fatalf("after Close base[9] = 0x%x, want NoSymbol", x.baseKeysym(9))
	}
	if x.baseKeysym(10) != symB {
		t.Fatalf("after Close base[10] = 0x%x, want b (only new slot restored)", x.baseKeysym(10))
	}
	if len(x.scratches) != 0 {
		t.Fatalf("scratches after Close = %v", x.scratches)
	}
}

func TestScratch_ensureFailsWhenNoEmptyLeft(t *testing.T) {
	const symAltR = xproto.Keysym(0xffea)
	x := &Xdo{
		min:               8,
		max:               9,
		keysymsPerKeycode: 1,
		keyMap:            []xproto.Keysym{0x61, 0},
	}
	if _, err := x.keycodeForKeysym(symAltR); err != nil {
		t.Fatalf("bind: %v", err)
	}
	// Old slot taken, no other empties.
	x.keyMap = []xproto.Keysym{0x61, 0x62}
	err := x.ensureScratches()
	if err == nil {
		t.Fatal("ensureScratches must fail when no empty keycode remains")
	}
	if !strings.Contains(err.Error(), "no empty keycode to re-bind") {
		t.Fatalf("error = %v", err)
	}
	// Undo entry retained for Close attempt.
	if _, ok := x.scratches[symAltR]; !ok {
		t.Fatal("scratch entry should remain after failed ensure")
	}
}

func TestScratch_ensureDropsWhenNativeBaseAppears(t *testing.T) {
	const symAltR = xproto.Keysym(0xffea)
	x := &Xdo{
		min:               8,
		max:               10,
		keysymsPerKeycode: 1,
		keyMap:            []xproto.Keysym{0x61, 0, 0},
	}
	if _, err := x.keycodeForKeysym(symAltR); err != nil {
		t.Fatalf("bind: %v", err)
	}
	// Native base for Alt_R now on 8; old scratch slot 10 empty again in reload view.
	x.keyMap = []xproto.Keysym{symAltR, 0, 0}
	// But undo still thinks we own 10 with Alt_R — simulate still having our binding on 10.
	x.keyMap = []xproto.Keysym{symAltR, 0, symAltR}
	if err := x.ensureScratches(); err != nil {
		t.Fatalf("ensureScratches: %v", err)
	}
	if _, ok := x.scratches[symAltR]; ok {
		t.Fatal("scratch should be dropped when native base exists elsewhere")
	}
	// Old scratch slot restored to previous (NoSymbol).
	if x.baseKeysym(10) != 0 {
		t.Fatalf("base[10] = 0x%x, want NoSymbol after releasing obsolete scratch", x.baseKeysym(10))
	}
	if x.baseKeysym(8) != symAltR {
		t.Fatalf("base[8] = 0x%x, want native Alt_R", x.baseKeysym(8))
	}
}

func TestScratch_closeRestoresMultiple(t *testing.T) {
	const (
		symAltR   = xproto.Keysym(0xffea)
		symSuperR = xproto.Keysym(0xffec)
	)
	x := &Xdo{
		min:               8,
		max:               10,
		keysymsPerKeycode: 1,
		keyMap:            []xproto.Keysym{0x61, 0, 0},
	}
	if _, err := x.keycodeForKeysym(symAltR); err != nil {
		t.Fatalf("Alt_R: %v", err)
	}
	if _, err := x.keycodeForKeysym(symSuperR); err != nil {
		t.Fatalf("Super_R: %v", err)
	}
	if x.baseKeysym(10) != symAltR || x.baseKeysym(9) != symSuperR {
		t.Fatalf("before Close map = %v", x.keyMap)
	}
	x.Close()
	if len(x.scratches) != 0 {
		t.Fatalf("scratches = %v", x.scratches)
	}
	if x.baseKeysym(9) != 0 || x.baseKeysym(10) != 0 {
		t.Fatalf("after Close map = %v, want empties at 9 and 10", x.keyMap)
	}
}

func TestScratch_partialRestoreContinues(t *testing.T) {
	const (
		symAltR   = xproto.Keysym(0xffea)
		symSuperR = xproto.Keysym(0xffec)
	)
	failKc := xproto.Keycode(10)
	x := &Xdo{
		min:               8,
		max:               10,
		keysymsPerKeycode: 1,
		keyMap:            []xproto.Keysym{0x61, 0, 0},
		changeMapping: func(kc xproto.Keycode, keysyms []xproto.Keysym) error {
			if kc == failKc && keysyms[0] == 0 {
				return errors.New("restore failed")
			}
			return nil
		},
	}
	if _, err := x.keycodeForKeysym(symAltR); err != nil {
		t.Fatalf("Alt_R: %v", err)
	}
	if _, err := x.keycodeForKeysym(symSuperR); err != nil {
		t.Fatalf("Super_R: %v", err)
	}
	// Alt_R on 10 (fails restore), Super_R on 9 (succeeds).
	x.Close()
	if len(x.scratches) != 0 {
		t.Fatalf("scratches must be cleared even on partial restore failure: %v", x.scratches)
	}
	// Super_R slot restored; Alt_R may remain if restore failed before local write.
	if x.baseKeysym(9) != 0 {
		t.Fatalf("base[9] = 0x%x, want restored NoSymbol", x.baseKeysym(9))
	}
}

func TestScratch_changeMappingBindFailure(t *testing.T) {
	bindErr := errors.New("change mapping refused")
	x := &Xdo{
		min:               8,
		max:               9,
		keysymsPerKeycode: 1,
		keyMap:            []xproto.Keysym{0x61, 0},
		changeMapping: func(xproto.Keycode, []xproto.Keysym) error {
			return bindErr
		},
	}
	_, err := x.keycodeForKeysym(0xffea)
	if err == nil {
		t.Fatal("bind must fail when changeMapping fails")
	}
	if !errors.Is(err, bindErr) {
		t.Fatalf("error = %v, want wrap of bindErr", err)
	}
	if len(x.scratches) != 0 {
		t.Fatalf("scratches = %v, want empty on failed bind", x.scratches)
	}
	if x.baseKeysym(9) != 0 {
		t.Fatalf("map must be unchanged; base[9] = 0x%x", x.baseKeysym(9))
	}
}

func TestScratch_changeMappingEnsureFailure(t *testing.T) {
	const symAltR = xproto.Keysym(0xffea)
	var failReapply bool
	x := &Xdo{
		min:               8,
		max:               9,
		keysymsPerKeycode: 1,
		keyMap:            []xproto.Keysym{0x61, 0},
		changeMapping: func(xproto.Keycode, []xproto.Keysym) error {
			if failReapply {
				return errors.New("re-apply refused")
			}
			return nil
		},
	}
	if _, err := x.keycodeForKeysym(symAltR); err != nil {
		t.Fatalf("bind: %v", err)
	}
	x.keyMap = []xproto.Keysym{0x61, 0} // lost scratch locally
	failReapply = true
	err := x.ensureScratches()
	if err == nil {
		t.Fatal("ensureScratches must fail when changeMapping fails")
	}
	if !strings.Contains(err.Error(), "re-apply scratch") {
		t.Fatalf("error = %v", err)
	}
}

func TestScratch_keycodeForKeysymReensuresOnDrift(t *testing.T) {
	// Map drift: scratch recorded, local base cleared; keycodeForKeysym re-ensures.
	const symAltR = xproto.Keysym(0xffea)
	x := &Xdo{
		min:               8,
		max:               9,
		keysymsPerKeycode: 1,
		keyMap:            []xproto.Keysym{0x61, 0},
	}
	if _, err := x.keycodeForKeysym(symAltR); err != nil {
		t.Fatalf("bind: %v", err)
	}
	x.keyMap = []xproto.Keysym{0x61, 0} // drift: lost base
	kc, err := x.keycodeForKeysym(symAltR)
	if err != nil {
		t.Fatalf("keycodeForKeysym after drift: %v", err)
	}
	if kc != 9 {
		t.Fatalf("keycode = %d, want 9", kc)
	}
	if x.baseKeysym(9) != symAltR {
		t.Fatalf("base[9] = 0x%x after re-ensure via keycodeForKeysym", x.baseKeysym(9))
	}
}

func TestScratch_skipsReservedKeycodeOnStaleMap(t *testing.T) {
	// Stale local map shows keycode 10 empty, but undo still owns it for Alt_R.
	// Super_R must take 9, not double-book 10.
	const (
		symAltR   = xproto.Keysym(0xffea)
		symSuperR = xproto.Keysym(0xffec)
	)
	x := &Xdo{
		min:               8,
		max:               10,
		keysymsPerKeycode: 1,
		keyMap:            []xproto.Keysym{0x61, 0, 0},
	}
	if _, err := x.keycodeForKeysym(symAltR); err != nil {
		t.Fatalf("Alt_R: %v", err)
	}
	// Stale: pretend 10 is empty again without clearing undo.
	x.keyMap[2] = 0
	kc, err := x.keycodeForKeysym(symSuperR)
	if err != nil {
		t.Fatalf("Super_R: %v", err)
	}
	if kc != 9 {
		t.Fatalf("Super_R keycode = %d, want 9 (10 reserved by Alt_R scratch)", kc)
	}
	if x.scratches[symAltR].keycode != 10 {
		t.Fatalf("Alt_R scratch keycode = %d, want 10", x.scratches[symAltR].keycode)
	}
}

func TestScratch_ensureTwoPassReapplyBeforeRebind(t *testing.T) {
	// Two scratches: after reload, A' s slot is taken (needs rebind) and B's slot
	// is empty/reserved (needs re-apply). Only B's slot is empty. Single-pass
	// ensure that rebinds first can fail closed before re-applying B; two-pass
	// must re-apply B even when A cannot rebind.
	const (
		symAltR   = xproto.Keysym(0xffea) // A — will need rebind
		symSuperR = xproto.Keysym(0xffec) // B — will need re-apply
		symA      = xproto.Keysym(0x61)
		symB      = xproto.Keysym(0x62)
	)
	x := &Xdo{
		min:               8,
		max:               10,
		keysymsPerKeycode: 1,
		keyMap:            []xproto.Keysym{symA, 0, 0},
	}
	if _, err := x.keycodeForKeysym(symAltR); err != nil {
		t.Fatalf("Alt_R: %v", err)
	}
	if _, err := x.keycodeForKeysym(symSuperR); err != nil {
		t.Fatalf("Super_R: %v", err)
	}
	// After binds: 10=Alt_R, 9=Super_R (high-first).
	if x.scratches[symAltR].keycode != 10 || x.scratches[symSuperR].keycode != 9 {
		t.Fatalf("scratch keycodes Alt_R=%d Super_R=%d, want 10 and 9",
			x.scratches[symAltR].keycode, x.scratches[symSuperR].keycode)
	}
	// Reload: Alt_R slot taken; Super_R slot empty again; no free empty left.
	x.keyMap = []xproto.Keysym{symA, 0, symB}
	err := x.ensureScratches()
	// Alt_R cannot rebind (only empty was Super_R's reserved slot, now re-applied).
	if err == nil {
		t.Fatal("ensure must fail closed for Alt_R rebind with no free empty")
	}
	if !strings.Contains(err.Error(), "no empty keycode to re-bind") {
		t.Fatalf("error = %v", err)
	}
	// Super_R must still have been re-applied in pass 1.
	if x.baseKeysym(9) != symSuperR {
		t.Fatalf("base[9] = 0x%x, want Super_R re-applied despite Alt_R rebind failure", x.baseKeysym(9))
	}
	if s, ok := x.scratches[symSuperR]; !ok || s.keycode != 9 {
		t.Fatalf("Super_R scratch = %v, %v; want keycode 9", s, ok)
	}
	// Alt_R undo retained for Close.
	if _, ok := x.scratches[symAltR]; !ok {
		t.Fatal("Alt_R scratch should remain after failed rebind")
	}
}

func TestScratch_ensureTwoPassRebindAfterReapply(t *testing.T) {
	// Same setup as above but with a free empty after B re-applies so A rebinds.
	const (
		symAltR   = xproto.Keysym(0xffea)
		symSuperR = xproto.Keysym(0xffec)
		symA      = xproto.Keysym(0x61)
		symB      = xproto.Keysym(0x62)
	)
	x := &Xdo{
		min:               8,
		max:               11,
		keysymsPerKeycode: 1,
		keyMap:            []xproto.Keysym{symA, 0, 0, 0},
	}
	if _, err := x.keycodeForKeysym(symAltR); err != nil {
		t.Fatalf("Alt_R: %v", err)
	}
	if _, err := x.keycodeForKeysym(symSuperR); err != nil {
		t.Fatalf("Super_R: %v", err)
	}
	// High-first: Alt_R=11, Super_R=10; 9 still empty.
	if x.scratches[symAltR].keycode != 11 || x.scratches[symSuperR].keycode != 10 {
		t.Fatalf("scratch keycodes Alt_R=%d Super_R=%d, want 11 and 10",
			x.scratches[symAltR].keycode, x.scratches[symSuperR].keycode)
	}
	// Alt_R slot taken; Super_R empty; free empty at 9 for rebind.
	x.keyMap = []xproto.Keysym{symA, 0, 0, symB}
	if err := x.ensureScratches(); err != nil {
		t.Fatalf("ensureScratches: %v", err)
	}
	if x.baseKeysym(10) != symSuperR {
		t.Fatalf("base[10] = 0x%x, want Super_R re-applied", x.baseKeysym(10))
	}
	s, ok := x.scratches[symAltR]
	if !ok || s.keycode != 9 {
		t.Fatalf("Alt_R scratch = %v, %v; want rebind to 9", s, ok)
	}
	if x.baseKeysym(9) != symAltR {
		t.Fatalf("base[9] = 0x%x, want Alt_R after rebind", x.baseKeysym(9))
	}
	if x.baseKeysym(11) != symB {
		t.Fatalf("base[11] = 0x%x, want b left untouched", x.baseKeysym(11))
	}
}
