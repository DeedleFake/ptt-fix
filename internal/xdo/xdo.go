// Package xdo synthesizes keyboard and mouse input through the X protocol
// (XTest), so X11/XWayland clients can receive push-to-talk events.
//
// This package is pure Go (no cgo). Keysym names in config are resolved with a
// generated client-side name table (see keysyms.go and go:generate); the X
// protocol does not provide name→keysym lookup.
//
// An [Xdo] value is not safe for concurrent use by multiple goroutines.
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
// If a keysym has no base mapping at all, [Xdo] may install a process-lifetime
// scratch binding: it finds a fully empty keycode (all columns NoSymbol), maps
// the keysym onto that keycode via ChangeKeyboardMapping, and reuses the slot
// for the rest of the connection. Only empty keycodes are used; real keys are
// never overwritten. If no empty keycode is available, resolution fails with an
// error that mentions scratch binding.
//
// Call [Xdo.Close] when finished. Scratch bindings are restored there (best-
// effort: a failure restoring one slot does not block restoring the others, and
// errors are not surfaced). ChangeKeyboardMapping is server-global and survives
// client disconnect, so relying on GC alone is insufficient unless the GC
// cleanup runs (it best-effort restores then closes). Prefer an explicit Close.
//
// [Xdo.Keycodes] reloads the server keyboard map on every call (when connected
// to a live display), so layout and remapping changes are visible without
// reconnecting. Installed scratch bindings are re-detected after each reload;
// if a scratch cannot be re-bound, the reload fails closed (error returned).
// Prefer [Xdo.BindKeys] for hold-style injection: it re-resolves on Down but
// releases the same keycodes on Up if the map changed mid-hold.
package xdo

import (
	"fmt"
	"maps"
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

// scratchBinding records a process-lifetime keyboard-map slot installed for an
// unmapped keysym, so it can be restored on Close (or GC cleanup).
type scratchBinding struct {
	keycode  xproto.Keycode
	previous []xproto.Keysym // keysyms for this keycode before the scratch bind
}

// connCleanup owns connection teardown state shared with runtime.AddCleanup.
// It must not retain *Xdo (AddCleanup value must not point into the object).
// Xdo.scratches is the same map reference when opened via [Open].
type connCleanup struct {
	conn      *xgb.Conn
	scratches map[xproto.Keysym]scratchBinding
}

// finalize restores scratch mappings (best-effort) then closes the connection.
// Used as the GC cleanup when Close was not called.
func (c *connCleanup) finalize() {
	if c == nil {
		return
	}
	for _, s := range c.scratches {
		if c.conn == nil {
			break
		}
		per := len(s.previous)
		if per == 0 {
			continue
		}
		// Best-effort; ignore errors so remaining slots still restore.
		if err := xproto.ChangeKeyboardMappingChecked(c.conn, 1, s.keycode, byte(per), s.previous).Check(); err != nil {
			continue
		}
	}
	clear(c.scratches)
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

// Xdo is a connection to an X display used to inject input via XTest.
// Not safe for concurrent use by multiple goroutines.
type Xdo struct {
	conn    *xgb.Conn
	cleanup runtime.Cleanup
	// cleanupState is non-nil when opened via [Open]; shared with GC cleanup.
	cleanupState *connCleanup
	min          xproto.Keycode
	max          xproto.Keycode
	// keyMap is keysyms for each keycode: length (max-min+1)*keysymsPerKeycode
	keysymsPerKeycode byte
	keyMap            []xproto.Keysym

	// scratches maps keysyms we bound onto empty keycodes for the life of this
	// connection. Restored in Close (and GC cleanup when using [Open]).
	scratches map[xproto.Keysym]scratchBinding

	// input, if non-nil, replaces XTest FakeInput. Used by tests to inject
	// failures without a live display.
	input func(evType, detail byte) error

	// changeMapping, if non-nil, replaces ChangeKeyboardMapping. Used by tests
	// without a live display. When both changeMapping and conn are nil, mapping
	// changes update only the local keyMap cache.
	changeMapping func(keycode xproto.Keycode, keysyms []xproto.Keysym) error
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

	cc := &connCleanup{
		conn:      conn,
		scratches: make(map[xproto.Keysym]scratchBinding),
	}
	x := &Xdo{conn: conn, cleanupState: cc, scratches: cc.scratches}
	if err := x.refreshKeyboardMap(); err != nil {
		conn.Close()
		return nil, err
	}
	x.cleanup = runtime.AddCleanup(x, (*connCleanup).finalize, cc)
	return x, nil
}

// Close restores any process-lifetime scratch keysym bindings (best-effort),
// then closes the underlying X connection. Always call Close when finished if
// scratch bindings may have been installed: ChangeKeyboardMapping is global and
// survives disconnect. A GC cleanup also best-effort restores then closes when
// the Xdo value becomes unreachable unless Close has already stopped that
// cleanup. Scratch restore runs even when conn is already nil (synthetic tests).
func (x *Xdo) Close() {
	if x == nil {
		return
	}
	x.cleanup.Stop()
	x.restoreScratches()
	if x.conn == nil {
		return
	}
	x.conn.Close()
	x.conn = nil
	if x.cleanupState != nil {
		x.cleanupState.conn = nil
	}
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
// connected X server. Returns an error if conn is nil (synthetic *Xdo values
// without a connection do not call this; [Xdo.Keycodes] skips the reload when
// conn is nil). After reloading, process-lifetime scratch bindings are
// re-detected and re-applied if needed; failure to re-bind fails closed.
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
	return x.ensureScratches()
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
// sym. If none exists, it may install a process-lifetime scratch binding on a
// fully empty keycode (see package docs). If the keysym only appears on a
// non-base level, an error is returned without scratch binding.
func (x *Xdo) keycodeForKeysym(sym xproto.Keysym) (xproto.Keycode, error) {
	per := int(x.keysymsPerKeycode)
	if per == 0 {
		return 0, fmt.Errorf("keyboard map has no keysyms per keycode")
	}
	if kc, ok := x.findBaseKeycode(sym); ok {
		return kc, nil
	}
	if x.keysymOnNonBase(sym) {
		return 0, fmt.Errorf("only available with modifiers (base-level keysyms only; no auto Shift/AltGr)")
	}
	// Recorded scratch but base lookup missed it (stale local map). Re-ensure
	// then resolve again; ensure may re-apply, move, or drop the scratch if a
	// native base mapping appeared.
	if _, ok := x.scratches[sym]; ok {
		if err := x.ensureScratches(); err != nil {
			return 0, err
		}
		if kc, ok := x.findBaseKeycode(sym); ok {
			return kc, nil
		}
		if s, ok := x.scratches[sym]; ok {
			return s.keycode, nil
		}
	}
	return x.bindScratch(sym)
}

// findBaseKeycode returns a keycode whose base-column keysym equals sym.
func (x *Xdo) findBaseKeycode(sym xproto.Keysym) (xproto.Keycode, bool) {
	per := int(x.keysymsPerKeycode)
	if per == 0 {
		return 0, false
	}
	n := int(x.max-x.min) + 1
	for i := range n {
		base := i * per
		if base >= len(x.keyMap) {
			break
		}
		if x.keyMap[base] == sym {
			return x.min + xproto.Keycode(i), true
		}
	}
	return 0, false
}

// keysymOnNonBase reports whether sym appears only on a non-base column.
func (x *Xdo) keysymOnNonBase(sym xproto.Keysym) bool {
	per := int(x.keysymsPerKeycode)
	if per <= 1 {
		return false
	}
	n := int(x.max-x.min) + 1
	for i := range n {
		base := i * per
		for c := 1; c < per; c++ {
			if base+c >= len(x.keyMap) {
				break
			}
			if x.keyMap[base+c] == sym {
				return true
			}
		}
	}
	return false
}

// bindScratch maps sym onto a fully empty keycode for the life of this *Xdo.
func (x *Xdo) bindScratch(sym xproto.Keysym) (xproto.Keycode, error) {
	kc, prev, ok := x.findEmptyKeycode()
	if !ok {
		return 0, fmt.Errorf("no empty keycode available for scratch binding of 0x%x", sym)
	}
	per := int(x.keysymsPerKeycode)
	newMap := make([]xproto.Keysym, per)
	newMap[0] = sym
	if err := x.setKeysyms(kc, newMap); err != nil {
		return 0, fmt.Errorf("bind unmapped keysym 0x%x on keycode %d: %w", sym, kc, err)
	}
	if x.scratches == nil {
		x.scratches = make(map[xproto.Keysym]scratchBinding)
		if x.cleanupState != nil {
			// Keep GC cleanup's undo table in sync for late first bind.
			x.cleanupState.scratches = x.scratches
		}
	}
	x.scratches[sym] = scratchBinding{keycode: kc, previous: prev}
	return kc, nil
}

// findEmptyKeycode returns a fully empty keycode (all columns NoSymbol) and a
// copy of its current keysyms. Prefers higher keycodes so physical keys
// (typically lower numbers) are left alone. Skips keycodes already reserved in
// the scratch undo table so a stale local map cannot double-book a slot.
// ok is false if none exist.
func (x *Xdo) findEmptyKeycode() (kc xproto.Keycode, previous []xproto.Keysym, ok bool) {
	per := int(x.keysymsPerKeycode)
	if per == 0 {
		return 0, nil, false
	}
	n := int(x.max-x.min) + 1
	for i := n - 1; i >= 0; i-- {
		cand := x.min + xproto.Keycode(i)
		if x.scratchKeycodeReserved(cand) {
			continue
		}
		base := i * per
		if base+per > len(x.keyMap) {
			continue
		}
		empty := true
		for c := range per {
			if x.keyMap[base+c] != 0 {
				empty = false
				break
			}
		}
		if !empty {
			continue
		}
		prev := make([]xproto.Keysym, per)
		copy(prev, x.keyMap[base:base+per])
		return cand, prev, true
	}
	return 0, nil, false
}

// scratchKeycodeReserved reports whether kc is already used by any scratch.
func (x *Xdo) scratchKeycodeReserved(kc xproto.Keycode) bool {
	for _, s := range x.scratches {
		if s.keycode == kc {
			return true
		}
	}
	return false
}

// baseKeysym returns the base-column keysym for kc, or 0 if out of range.
func (x *Xdo) baseKeysym(kc xproto.Keycode) xproto.Keysym {
	per := int(x.keysymsPerKeycode)
	if per == 0 || kc < x.min || kc > x.max {
		return 0
	}
	base := int(kc-x.min) * per
	if base >= len(x.keyMap) {
		return 0
	}
	return x.keyMap[base]
}

// keycodeFullyEmpty reports whether all columns for kc are NoSymbol.
func (x *Xdo) keycodeFullyEmpty(kc xproto.Keycode) bool {
	per := int(x.keysymsPerKeycode)
	if per == 0 || kc < x.min || kc > x.max {
		return false
	}
	base := int(kc-x.min) * per
	if base+per > len(x.keyMap) {
		return false
	}
	for c := range per {
		if x.keyMap[base+c] != 0 {
			return false
		}
	}
	return true
}

// setKeysyms applies keysyms for a single keycode via ChangeKeyboardMapping (or
// the test hook) and updates the local keyMap cache on success.
func (x *Xdo) setKeysyms(kc xproto.Keycode, keysyms []xproto.Keysym) error {
	per := int(x.keysymsPerKeycode)
	if per == 0 {
		return fmt.Errorf("keyboard map has no keysyms per keycode")
	}
	if len(keysyms) != per {
		// Normalize length: pad with NoSymbol or truncate to server column count.
		norm := make([]xproto.Keysym, per)
		copy(norm, keysyms)
		keysyms = norm
	}
	if x.changeMapping != nil {
		if err := x.changeMapping(kc, keysyms); err != nil {
			return err
		}
	} else if x.conn != nil {
		if err := xproto.ChangeKeyboardMappingChecked(x.conn, 1, kc, byte(per), keysyms).Check(); err != nil {
			return fmt.Errorf("change keyboard mapping: %w", err)
		}
	}
	return x.writeLocalKeysyms(kc, keysyms)
}

// writeLocalKeysyms updates the in-memory keyMap for kc.
func (x *Xdo) writeLocalKeysyms(kc xproto.Keycode, keysyms []xproto.Keysym) error {
	per := int(x.keysymsPerKeycode)
	if per == 0 || kc < x.min || kc > x.max {
		return fmt.Errorf("keycode %d out of range [%d, %d]", kc, x.min, x.max)
	}
	base := int(kc-x.min) * per
	if base+per > len(x.keyMap) {
		return fmt.Errorf("keycode %d outside keyMap", kc)
	}
	copy(x.keyMap[base:base+per], keysyms)
	return nil
}

// ensureScratches re-applies process-lifetime scratch bindings after a map
// reload so they are not forgotten if the server no longer shows them.
//
// Two passes so re-applies are not starved by rebind failures: (1) drop when a
// native base exists; re-apply when the scratch's own slot is still fully empty;
// (2) rebind any scratch still missing a base mapping onto a new empty keycode.
// Fail-closed: if a scratch cannot be re-bound after pass 1, an error is
// returned so refreshKeyboardMap / Keycodes do not proceed with a missing binding.
func (x *Xdo) ensureScratches() error {
	if len(x.scratches) == 0 {
		return nil
	}

	// Pass 1: drop obsolete scratches; re-apply when own slot is still empty.
	// Do not rebind here — a rebind that needs another scratch's reserved empty
	// slot would fail closed before that scratch could re-apply (map iteration order).
	for sym, s := range maps.Clone(x.scratches) {
		// Prefer a native (or any) base mapping over keeping a dual scratch.
		if baseKc, found := x.findBaseKeycode(sym); found {
			if baseKc == s.keycode {
				continue
			}
			// Base exists elsewhere: drop scratch and restore our old slot if
			// it still carries our keysym.
			if x.baseKeysym(s.keycode) == sym {
				if err := x.setKeysyms(s.keycode, s.previous); err != nil {
					return fmt.Errorf("release obsolete scratch keysym 0x%x: %w", sym, err)
				}
			}
			delete(x.scratches, sym)
			continue
		}
		if x.baseKeysym(s.keycode) == sym {
			continue
		}
		if !x.keycodeFullyEmpty(s.keycode) {
			// Slot taken or out of range — rebind in pass 2.
			continue
		}
		per := int(x.keysymsPerKeycode)
		newMap := make([]xproto.Keysym, per)
		newMap[0] = sym
		if err := x.setKeysyms(s.keycode, newMap); err != nil {
			return fmt.Errorf("re-apply scratch keysym 0x%x: %w", sym, err)
		}
		// Keep original previous for Close restore.
	}

	// Pass 2: rebind scratches that still have no base mapping.
	for sym, s := range maps.Clone(x.scratches) {
		if _, found := x.findBaseKeycode(sym); found {
			continue
		}
		// Temporarily drop so findEmptyKeycode does not skip this keycode if it
		// somehow became empty; other scratches remain reserved.
		delete(x.scratches, sym)
		kc, prev, ok := x.findEmptyKeycode()
		if !ok {
			// Put back so Close can still attempt undo of the old slot.
			x.scratches[sym] = s
			return fmt.Errorf("no empty keycode to re-bind scratch keysym 0x%x", sym)
		}
		per := int(x.keysymsPerKeycode)
		newMap := make([]xproto.Keysym, per)
		newMap[0] = sym
		if err := x.setKeysyms(kc, newMap); err != nil {
			x.scratches[sym] = s
			return fmt.Errorf("re-bind scratch keysym 0x%x: %w", sym, err)
		}
		x.scratches[sym] = scratchBinding{keycode: kc, previous: prev}
	}
	return nil
}

// restoreScratches undoes all process-lifetime scratch bindings (best-effort).
// Errors from individual slots are ignored so remaining slots still restore;
// the undo table is always cleared. Called from Close before the connection
// is closed. No logging (package has no logger); callers cannot observe failures.
func (x *Xdo) restoreScratches() {
	if len(x.scratches) == 0 {
		return
	}
	for _, s := range x.scratches {
		// Best-effort: continue so remaining slots are still restored.
		if err := x.setKeysyms(s.keycode, s.previous); err != nil {
			continue
		}
	}
	// Clear in place so Open's shared cleanupState map stays the same reference.
	clear(x.scratches)
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
