// Package xdo synthesizes keyboard and mouse input through the X protocol
// (XTest), so X11/XWayland clients can receive push-to-talk events.
//
// This package is pure Go (no cgo). Keysym names in config are resolved with a
// generated client-side name table (see keysyms.go and go:generate); the X
// protocol does not provide name→keysym lookup.
//
// # Keysym names
//
// Names match the usual X11 / xkbcommon macros with optional prefixes stripped
// (XKB_KEY_, XK_, XF86XK_). Lookup is exact after that strip — no case folding.
// Names are case-sensitive.
//
// # Keycode resolution
//
// Keycodes are taken only from the base column (index 0) of the server keyboard
// map for each keycode. This package does not synthesize Shift/AltGr or other
// modifiers. A keysym that appears only on a non-base level is rejected with an
// error rather than injected as a bare keycode that would type the wrong symbol.
//
// [Xdo.Keycodes] reloads the server keyboard map on every call (when connected
// to a live display), so layout and remapping changes are visible without
// reconnecting. Prefer [Xdo.BindKeys] for hold-style injection: it re-resolves
// on Down but releases the same keycodes on Up if the map changed mid-hold.
package xdo

import (
	"fmt"
	"runtime"
	"slices"
	"strings"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

//go:generate go run ./gen_keysyms.go -o keysyms.go

// keysymPrefixes are optional header-style prefixes, longest first, so at most
// one prefix is stripped (e.g. XKB_KEY_ before XK_).
var keysymPrefixes = []string{"XKB_KEY_", "XF86XK_", "XK_"}

// Xdo is a connection to an X display used to inject input via XTest.
type Xdo struct {
	conn    *xgb.Conn
	cleanup runtime.Cleanup
	min     xproto.Keycode
	max     xproto.Keycode
	// keyMap is keysyms for each keycode: length (max-min+1)*keysymsPerKeycode
	keysymsPerKeycode byte
	keyMap            []xproto.Keysym

	// input, if non-nil, replaces XTest FakeInput. Used by tests to inject
	// failures without a live display.
	input func(evType, detail byte) error
}

// Open connects to the default X display ($DISPLAY), initializes the XTest
// extension, and loads the server keyboard map for [Xdo.Keycodes] lookups.
func Open() (*Xdo, error) {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("connect to X display: %w", err)
	}

	if err := xtest.Init(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("init XTEST extension: %w", err)
	}

	x := &Xdo{conn: conn}
	if err := x.refreshKeyboardMap(); err != nil {
		conn.Close()
		return nil, err
	}
	x.cleanup = runtime.AddCleanup(x, (*xgb.Conn).Close, conn)
	return x, nil
}

// Close closes the underlying X connection. Optional; a cleanup also closes
// the connection when the Xdo value becomes unreachable unless Close has
// already stopped that cleanup.
func (x *Xdo) Close() {
	if x == nil {
		return
	}
	x.cleanup.Stop()
	if x.conn == nil {
		return
	}
	x.conn.Close()
	x.conn = nil
}

// KeysymByName looks up an X11/xkb-style keysym name (e.g. "Alt_L") without
// needing a display connection. Names are exact (case-sensitive) after optional
// prefix stripping (see package docs).
func KeysymByName(name string) (uint32, bool) {
	return lookupKeysym(name)
}

// Keycodes resolves a keysym name (or libxdo-style sequence of names joined
// by '+') to one or more X keycodes using the current server keyboard map.
// When a live connection is present, the map is reloaded from the server first
// so layout changes are observed. Only base column mappings are used (see
// package docs).
func (x *Xdo) Keycodes(keys string) ([]byte, error) {
	if x == nil {
		return nil, fmt.Errorf("xdo connection closed")
	}
	if x.conn != nil {
		if err := x.refreshKeyboardMap(); err != nil {
			return nil, err
		}
	}

	parts := splitKeysequence(keys)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty key sequence")
	}

	out := make([]byte, 0, len(parts))
	for _, part := range parts {
		sym, ok := lookupKeysym(part)
		if !ok {
			return nil, fmt.Errorf("unknown keysym %q", part)
		}
		kc, err := x.keycodeForKeysym(xproto.Keysym(sym))
		if err != nil {
			return nil, fmt.Errorf("keysym %q (0x%x): %w", part, sym, err)
		}
		out = append(out, byte(kc))
	}
	return out, nil
}

// KeyBinding injects a named key sequence for hold-style use (press on Down,
// release on Up). Down re-resolves names against the current server map; Up
// releases the keycodes that were actually pressed so a mid-hold remap cannot
// leave the wrong keys stuck or release unrelated ones.
type KeyBinding struct {
	x    *Xdo
	keys string
	held []byte
}

// BindKeys validates keys against the current map and returns a [KeyBinding].
func (x *Xdo) BindKeys(keys string) (*KeyBinding, error) {
	if x == nil {
		return nil, fmt.Errorf("xdo connection closed")
	}
	if _, err := x.Keycodes(keys); err != nil {
		return nil, err
	}
	return &KeyBinding{x: x, keys: keys}, nil
}

// Down resolves the binding's key names to keycodes and sends presses.
func (b *KeyBinding) Down() error {
	if b == nil || b.x == nil {
		return fmt.Errorf("xdo connection closed")
	}
	kcs, err := b.x.Keycodes(b.keys)
	if err != nil {
		return err
	}
	if err := b.x.KeyDown(kcs); err != nil {
		return err
	}
	b.held = kcs
	return nil
}

// Up releases the keycodes from the last successful Down. If there is no held
// press (for example Down never succeeded), it resolves and releases using the
// current map as a best-effort fallback.
func (b *KeyBinding) Up() error {
	if b == nil || b.x == nil {
		return fmt.Errorf("xdo connection closed")
	}
	kcs := b.held
	if kcs == nil {
		var err error
		kcs, err = b.x.Keycodes(b.keys)
		if err != nil {
			return err
		}
	}
	err := b.x.KeyUp(kcs)
	b.held = nil
	return err
}

// refreshKeyboardMap loads min/max keycodes and the full keysym table from the
// connected X server. No-op for tests that construct an *Xdo without a conn.
func (x *Xdo) refreshKeyboardMap() error {
	if x.conn == nil {
		return fmt.Errorf("xdo connection closed")
	}
	setup := xproto.Setup(x.conn)
	if setup == nil {
		//lint:ignore ST1005 "X" is a proper noun (X Window System)
		return fmt.Errorf("X setup info unavailable")
	}
	min, max := setup.MinKeycode, setup.MaxKeycode
	// X keycodes are bytes; max-min+1 fits in a byte on real servers (min ≥ 8).
	count := byte(max - min + 1)
	reply, err := xproto.GetKeyboardMapping(x.conn, min, count).Reply()
	if err != nil {
		return fmt.Errorf("get keyboard mapping: %w", err)
	}
	x.min = min
	x.max = max
	x.keysymsPerKeycode = reply.KeysymsPerKeycode
	x.keyMap = reply.Keysyms
	return nil
}

// KeyDown sends XTest key presses for pre-resolved keycodes.
// If a multi-key sequence fails after some keys were pressed, those already
// pressed keys are best-effort released in reverse order before the press
// error is returned.
func (x *Xdo) KeyDown(keycodes []byte) error {
	if err := x.ready(); err != nil {
		return err
	}
	for i, kc := range keycodes {
		if err := x.fakeInput(xproto.KeyPress, kc); err != nil {
			for j := i - 1; j >= 0; j-- {
				// Best-effort; preserve the original press error.
				x.fakeInput(xproto.KeyRelease, keycodes[j])
			}
			return err
		}
	}
	return nil
}

// KeyUp sends XTest key releases for pre-resolved keycodes (reverse order).
// If a release fails, remaining keys are still best-effort released and the
// first failure is returned (so a mid-chord X error does not leave other
// modifiers stuck down).
func (x *Xdo) KeyUp(keycodes []byte) error {
	if err := x.ready(); err != nil {
		return err
	}
	var first error
	for _, keycode := range slices.Backward(keycodes) {
		if err := x.fakeInput(xproto.KeyRelease, keycode); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// ValidButton reports whether button is a valid X button number (1–255).
func ValidButton(button int) error {
	if button < 1 || button > 255 {
		return fmt.Errorf("invalid mouse button: %d", button)
	}
	return nil
}

// ButtonDown sends an XTest mouse button press (X button numbers, 1-based).
func (x *Xdo) ButtonDown(button int) error {
	if err := ValidButton(button); err != nil {
		return err
	}
	if err := x.ready(); err != nil {
		return err
	}
	return x.fakeInput(xproto.ButtonPress, byte(button))
}

// ButtonUp sends an XTest mouse button release.
func (x *Xdo) ButtonUp(button int) error {
	if err := ValidButton(button); err != nil {
		return err
	}
	if err := x.ready(); err != nil {
		return err
	}
	return x.fakeInput(xproto.ButtonRelease, byte(button))
}

func (x *Xdo) ready() error {
	if x == nil || (x.conn == nil && x.input == nil) {
		return fmt.Errorf("xdo connection closed")
	}
	return nil
}

func (x *Xdo) fakeInput(evType byte, detail byte) error {
	if x.input != nil {
		return x.input(evType, detail)
	}
	return xtest.FakeInputChecked(x.conn, evType, detail, 0, 0, 0, 0, 0).Check()
}

// keycodeForKeysym finds a keycode whose base-column (index 0) keysym equals
// sym. If the keysym only appears on a non-base level, an error is returned.
func (x *Xdo) keycodeForKeysym(sym xproto.Keysym) (xproto.Keycode, error) {
	per := int(x.keysymsPerKeycode)
	if per == 0 {
		return 0, fmt.Errorf("keyboard map has no keysyms per keycode")
	}
	n := int(x.max-x.min) + 1
	var nonBase bool
	for i := range n {
		base := i * per
		if base >= len(x.keyMap) {
			break
		}
		if x.keyMap[base] == sym {
			return x.min + xproto.Keycode(i), nil
		}
		for c := 1; c < per; c++ {
			if base+c >= len(x.keyMap) {
				break
			}
			if x.keyMap[base+c] == sym {
				nonBase = true
			}
		}
	}
	if nonBase {
		return 0, fmt.Errorf("only available with modifiers (base-level keysyms only; no auto Shift/AltGr)")
	}
	return 0, fmt.Errorf("no keycode mapped")
}

func lookupKeysym(name string) (uint32, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, false
	}
	name = stripKeysymPrefix(name)
	v, ok := keysyms[name]
	return v, ok
}

// stripKeysymPrefix removes at most one known header-style prefix, matching the
// longest applicable prefix first.
func stripKeysymPrefix(name string) string {
	for _, p := range keysymPrefixes {
		if strings.HasPrefix(name, p) {
			return name[len(p):]
		}
	}
	return name
}

// splitKeysequence splits libxdo/xdotool-style sequences ("Control_L+Alt_L").
// A single bare name has one part.
func splitKeysequence(keys string) []string {
	keys = strings.TrimSpace(keys)
	if keys == "" {
		return nil
	}
	parts := strings.Split(keys, "+")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
