package xdo

import (
	"os"
	"testing"
)

// Live X smoke: only runs when DISPLAY is set and an X server is reachable.
// Does not inject keys (avoids stealing input); only connects and resolves.
func TestXTestSmokeConnectAndResolve(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("DISPLAY not set")
	}

	x, err := Open()
	if err != nil {
		t.Skipf("cannot open X display: %v", err)
	}
	defer x.Close()

	kcs, err := x.Keycodes("Alt_L")
	if err != nil {
		t.Fatalf("Keycodes(Alt_L): %v", err)
	}
	if len(kcs) == 0 {
		t.Fatal("Keycodes(Alt_L) returned empty")
	}

	// mouse path: valid button id is accepted by the validator; do not send.
	if err := (&Xdo{}).ButtonDown(0); err == nil {
		t.Fatal("expected invalid button")
	}
}
