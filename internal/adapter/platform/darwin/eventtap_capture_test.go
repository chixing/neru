//go:build darwin

package darwin

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Compile the real bridge with only its WindowServer tap creation/enable calls
// replaced. The fixture owns an ordinary Mach port and never captures input.
func TestEventTap_CaptureIntent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	binary := filepath.Join(t.TempDir(), "eventtap-capture")
	compile := exec.CommandContext(ctx, "xcrun", "clang",
		"-fobjc-arc", "-fblocks", "-mmacosx-version-min=14.0",
		"-framework", "Foundation", "-framework", "AppKit",
		"-framework", "Carbon", "-framework", "CoreGraphics",
		"-framework", "ApplicationServices",
		"testdata/eventtap_capture.m", "-o", binary)

	output, err := compile.CombinedOutput()
	if err != nil {
		t.Fatalf("compile native capture fixture: %v\n%s", err, output)
	}

	for _, scenario := range []string{
		"creation", "active-timeout", "stale-timeout", "disabled-key",
		"pending-disable", "last-command", "destroy-timeout", "user-disabled",
	} {
		t.Run(scenario, func(t *testing.T) {
			runCtx, runCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer runCancel()

			output, err := exec.CommandContext(runCtx, binary, scenario).CombinedOutput()
			if err != nil {
				t.Fatalf("native capture scenario: %v\n%s", err, output)
			}
		})
	}
}
