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
package xdo

import (
	"fmt"
	"runtime"
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

// Open connects to the default X display ($DISPLAY) and initializes the XTest extension.
func Open() (*Xdo, error) {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("connect to X display: %w", err)
	}

	if err := xtest.Init(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("init XTEST extension: %w", err)
	}

	setup := xproto.Setup(conn)
	if setup == nil {
		conn.Close()
		return nil, fmt.Errorf("X setup info unavailable")
	}

	min, max := setup.MinKeycode, setup.MaxKeycode
	// X keycodes are bytes; max-min+1 fits in a byte on real servers.
	count := byte(max - min + 1)
	reply, err := xproto.GetKeyboardMapping(conn, min, count).Reply()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("get keyboard mapping: %w", err)
	}

	x := &Xdo{
		conn:              conn,
		min:               min,
		max:               max,
		keysymsPerKeycode: reply.KeysymsPerKeycode,
		keyMap:            reply.Keysyms,
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
// by '+') to one or more X keycodes using the server keyboard map. Only base
// column mappings are used (see package docs).
func (x *Xdo) Keycodes(keys string) ([]byte, error) {
	if x == nil {
		return nil, fmt.Errorf("xdo connection closed")
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

// KeyDown sends XTest key presses for pre-resolved keycodes.
// If a multi-key sequence fails after some keys were pressed, those already
// pressed keys are best-effort released in reverse order before the press
// error is returned.
func (x *Xdo) KeyDown(keycodes []byte) error {
	if err := x.ready(); err != nil {
		return err
	}
	for i, kc := range keycodes {
		if err := x.fakeKey(xproto.KeyPress, kc); err != nil {
			for j := i - 1; j >= 0; j-- {
				// Best-effort; preserve the original press error.
				x.fakeKey(xproto.KeyRelease, keycodes[j])
			}
			return err
		}
	}
	return nil
}

// KeyUp sends XTest key releases for pre-resolved keycodes (reverse order).
func (x *Xdo) KeyUp(keycodes []byte) error {
	if err := x.ready(); err != nil {
		return err
	}
	for i := len(keycodes) - 1; i >= 0; i-- {
		if err := x.fakeKey(xproto.KeyRelease, keycodes[i]); err != nil {
			return err
		}
	}
	return nil
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
	return x.fakeButton(xproto.ButtonPress, byte(button))
}

// ButtonUp sends an XTest mouse button release.
func (x *Xdo) ButtonUp(button int) error {
	if err := ValidButton(button); err != nil {
		return err
	}
	if err := x.ready(); err != nil {
		return err
	}
	return x.fakeButton(xproto.ButtonRelease, byte(button))
}

func (x *Xdo) ready() error {
	if x == nil || (x.conn == nil && x.input == nil) {
		return fmt.Errorf("xdo connection closed")
	}
	return nil
}

func (x *Xdo) fakeKey(evType byte, keycode byte) error {
	return x.fakeInput(evType, keycode)
}

func (x *Xdo) fakeButton(evType byte, button byte) error {
	return x.fakeInput(evType, button)
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
