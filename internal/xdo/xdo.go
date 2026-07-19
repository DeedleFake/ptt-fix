// Package xdo synthesizes keyboard and mouse input through the X protocol
// (XTest), so X11/XWayland clients can receive push-to-talk events.
//
// This package is pure Go (no cgo). It replaces the former libxdo wrapper
// while accepting the same keysym name strings used in config.
package xdo

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

// Xdo is a connection to an X display used to inject input via XTest.
type Xdo struct {
	conn *xgb.Conn
	min  xproto.Keycode
	max  xproto.Keycode
	// keyMap is keysyms for each keycode: length (max-min+1)*keysymsPerKeycode
	keysymsPerKeycode byte
	keyMap            []xproto.Keysym
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
	runtime.AddCleanup(x, (*xgb.Conn).Close, conn)
	return x, nil
}

// Close closes the underlying X connection. Optional; finalizers also close.
func (x *Xdo) Close() {
	if x == nil || x.conn == nil {
		return
	}
	x.conn.Close()
	x.conn = nil
}

// KeysymByName looks up an X11/xkb-style keysym name (e.g. "Alt_L") without
// needing a display connection. Names match those accepted by the former
// libxdo path (XK_ / XKB_KEY_ prefix removed).
func KeysymByName(name string) (uint32, bool) {
	return lookupKeysym(name)
}

// Keycodes resolves a keysym name (or libxdo-style sequence of names joined
// by '+') to one or more X keycodes using the server keyboard map.
func (x *Xdo) Keycodes(keys string) ([]byte, error) {
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
		kc, ok := x.keycodeForKeysym(xproto.Keysym(sym))
		if !ok {
			return nil, fmt.Errorf("no keycode mapped for keysym %q (0x%x)", part, sym)
		}
		out = append(out, byte(kc))
	}
	return out, nil
}

// SendKeysequenceWindowDown synthesizes key presses for the key sequence.
// The window argument is retained for API compatibility with the libxdo
// wrapper; XTest injects into the X server input stream (current focus /
// grabs), matching CURRENTWINDOW behavior.
func (x *Xdo) SendKeysequenceWindowDown(_ Window, keys string, _ time.Duration) bool {
	kcs, err := x.Keycodes(keys)
	if err != nil {
		return true // non-zero return meant failure in libxdo
	}
	for _, kc := range kcs {
		if err := x.fakeKey(xproto.KeyPress, kc); err != nil {
			return true
		}
	}
	return false
}

// SendKeysequenceWindowUp synthesizes key releases for the key sequence.
func (x *Xdo) SendKeysequenceWindowUp(_ Window, keys string, _ time.Duration) bool {
	kcs, err := x.Keycodes(keys)
	if err != nil {
		return true
	}
	// Release in reverse order (modifiers last), matching common xdotool behavior.
	for i := len(kcs) - 1; i >= 0; i-- {
		if err := x.fakeKey(xproto.KeyRelease, kcs[i]); err != nil {
			return true
		}
	}
	return false
}

// MouseDown synthesizes a mouse button press (X button numbers, 1-based).
func (x *Xdo) MouseDown(_ Window, button int) bool {
	if button < 1 || button > 255 {
		return true
	}
	return x.fakeButton(xproto.ButtonPress, byte(button)) != nil
}

// MouseUp synthesizes a mouse button release.
func (x *Xdo) MouseUp(_ Window, button int) bool {
	if button < 1 || button > 255 {
		return true
	}
	return x.fakeButton(xproto.ButtonRelease, byte(button)) != nil
}

// KeyDown sends XTest key presses for pre-resolved keycodes (setup-time map).
func (x *Xdo) KeyDown(keycodes []byte) error {
	for _, kc := range keycodes {
		if err := x.fakeKey(xproto.KeyPress, kc); err != nil {
			return err
		}
	}
	return nil
}

// KeyUp sends XTest key releases for pre-resolved keycodes (reverse order).
func (x *Xdo) KeyUp(keycodes []byte) error {
	for i := len(keycodes) - 1; i >= 0; i-- {
		if err := x.fakeKey(xproto.KeyRelease, keycodes[i]); err != nil {
			return err
		}
	}
	return nil
}

// ButtonDown sends an XTest mouse button press.
func (x *Xdo) ButtonDown(button int) error {
	if button < 1 || button > 255 {
		return fmt.Errorf("invalid mouse button: %d", button)
	}
	return x.fakeButton(xproto.ButtonPress, byte(button))
}

// ButtonUp sends an XTest mouse button release.
func (x *Xdo) ButtonUp(button int) error {
	if button < 1 || button > 255 {
		return fmt.Errorf("invalid mouse button: %d", button)
	}
	return x.fakeButton(xproto.ButtonRelease, byte(button))
}

func (x *Xdo) fakeKey(evType byte, keycode byte) error {
	return xtest.FakeInputChecked(x.conn, evType, keycode, 0, 0, 0, 0, 0).Check()
}

func (x *Xdo) fakeButton(evType byte, button byte) error {
	return xtest.FakeInputChecked(x.conn, evType, button, 0, 0, 0, 0, 0).Check()
}

func (x *Xdo) keycodeForKeysym(sym xproto.Keysym) (xproto.Keycode, bool) {
	per := int(x.keysymsPerKeycode)
	if per == 0 {
		return 0, false
	}
	n := int(x.max-x.min) + 1
	for i := range n {
		base := i * per
		for c := range per {
			if base+c >= len(x.keyMap) {
				break
			}
			if x.keyMap[base+c] == sym {
				return x.min + xproto.Keycode(i), true
			}
		}
	}
	return 0, false
}

func lookupKeysym(name string) (uint32, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, false
	}
	// Accept optional historical prefixes users might paste from headers.
	for _, p := range []string{"XKB_KEY_", "XK_", "XF86XK_"} {
		name = strings.TrimPrefix(name, p)
	}
	if v, ok := keysyms[name]; ok {
		return v, true
	}
	// Case-insensitive fallback for common mistakes (e.g. alt_l).
	if v, ok := keysyms[strings.ToLower(name)]; ok {
		return v, true
	}
	// Title-case single-segment names: "return" -> try "Return" already lower map miss
	if len(name) > 0 {
		titled := strings.ToUpper(name[:1]) + strings.ToLower(name[1:])
		if v, ok := keysyms[titled]; ok {
			return v, true
		}
	}
	return 0, false
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

// Window is retained for API compatibility with the previous wrapper.
type Window uint32

// CurrentWindow matches libxdo's CURRENTWINDOW (XTest / current focus path).
const CurrentWindow Window = 0
