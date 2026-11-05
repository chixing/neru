package vision

import (
	"image"
	"testing"

	"github.com/y3owk1n/neru/internal/domain/element"
)

func TestTargetElements_ClipsToRegionAndDropsOutside(t *testing.T) {
	t.Parallel()

	frame := detectionFrame{
		origin: image.Pt(1920, 0),
		region: image.Rect(2000, 100, 2800, 700),
	}
	rects := []image.Rectangle{
		image.Rect(100, 150, 200, 190), // inside once offset
		image.Rect(10, 10, 50, 30),     // outside region
		image.Rect(40, 300, 120, 340),  // straddles the region's left edge
	}

	got := targetElements(frame, rects, element.SourceContour)
	if len(got) != 2 {
		t.Fatalf("len(targetElements) = %d, want 2", len(got))
	}

	want := image.Rect(2020, 150, 2120, 190)
	if got[0].Bounds() != want {
		t.Errorf("Bounds() = %v, want %v", got[0].Bounds(), want)
	}

	clipped := image.Rect(2000, 300, 2040, 340)
	if got[1].Bounds() != clipped {
		t.Errorf("straddling Bounds() = %v, want clipped to %v", got[1].Bounds(), clipped)
	}

	if !got[0].IsClickable() || !got[0].IsVisionOnly() {
		t.Error("element must be clickable and vision-only")
	}

	if got[0].Source() != element.SourceContour {
		t.Errorf("Source() = %q, want %q", got[0].Source(), element.SourceContour)
	}
}

func TestTextTargetElements_KeepsTextAlignedAfterClipping(t *testing.T) {
	t.Parallel()

	const targetText = "Save As"

	frame := detectionFrame{
		origin: image.Pt(1920, 0),
		region: image.Rect(2000, 100, 2800, 700),
	}
	rects := []image.Rectangle{
		image.Rect(10, 10, 50, 30),    // dropped
		image.Rect(40, 300, 120, 340), // clipped
		image.Rect(100, 150, 200, 190),
	}

	got := textTargetElements(frame, rects, element.SourceContour,
		[]string{"outside", targetText, "Title"})
	if len(got) != 2 {
		t.Fatalf("target count = %d, want 2", len(got))
	}

	if got[0].Bounds() != image.Rect(2000, 300, 2040, 340) ||
		got[0].Title() != targetText || got[0].SearchText() != targetText {
		t.Fatalf("clipped target lost its bounds or OCR text: bounds %v, title %q, search text %q",
			got[0].Bounds(), got[0].Title(), got[0].SearchText())
	}

	if got[1].SearchText() != "Title" || got[1].Source() != element.SourceContour {
		t.Fatal("remaining target lost its OCR text or contour source")
	}
}
