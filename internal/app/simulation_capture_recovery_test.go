package app_test

import (
	"context"
	"testing"

	"github.com/y3owk1n/neru/internal/domain"
)

// A native timeout can re-enable capture behind the application's back while
// the mode still says idle. Pause must always release it, not just exit a mode.
func TestSimulation_PauseWhileIdleReleasesTheKeyboard(t *testing.T) {
	sim := newSimHarness(t, simConfig(), nil)
	sim.waitMode(domain.ModeIdle)

	for _, name := range []string{"first pause", "already paused"} {
		t.Run(name, func(t *testing.T) {
			err := sim.tap.Enable(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			sim.app.SetEnabled(false)

			if sim.tap.IsEnabled() {
				t.Fatal("pause left the keyboard captured while the mode was already idle")
			}
		})
	}

	sim.app.SetEnabled(true)

	if sim.tap.IsEnabled() {
		t.Fatal("resume enabled full capture without entering a mode")
	}
}
