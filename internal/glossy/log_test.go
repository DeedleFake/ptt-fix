package glossy_test

import (
	"testing"
	"testing/slogtest"

	"deedles.dev/ptt-fix/internal/glossy"
)

func TestHandler(t *testing.T) {
	err := slogtest.TestHandler(glossy.Handler{}, func() []map[string]any { return nil })
	if err != nil {
		t.Fatal(err)
	}
}
