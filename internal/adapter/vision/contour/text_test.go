package contour_test

import (
	"image"
	"reflect"
	"testing"

	"github.com/y3owk1n/neru/internal/adapter/vision/contour"
)

func TestLabel_SmallestHolderGetsWordsAndStraysBecomeTargets(t *testing.T) {
	t.Parallel()

	toolbar := image.Rect(0, 0, 400, 40)
	button := image.Rect(10, 5, 90, 35)
	rects := []image.Rectangle{toolbar, button}
	words := []contour.Word{
		{Rect: image.Rect(15, 10, 45, 30), Text: "Save"},
		{Rect: image.Rect(50, 10, 85, 30), Text: "As"},
		{Rect: image.Rect(200, 10, 240, 30), Text: "Title"},
		{Rect: image.Rect(10, 100, 60, 120), Text: "stray"},
	}

	gotRects, gotTexts := contour.Label(rects, words)

	wantRects := []image.Rectangle{toolbar, button, image.Rect(10, 100, 60, 120)}
	wantTexts := []string{"Title", "Save As", "stray"}

	if !reflect.DeepEqual(gotRects, wantRects) || !reflect.DeepEqual(gotTexts, wantTexts) {
		t.Fatalf("Label = %v %q, want %v %q", gotRects, gotTexts, wantRects, wantTexts)
	}

	if len(rects) != 2 {
		t.Error("Label must not grow the caller's slice")
	}

	els := contour.Elements(image.Point{}, image.Rect(0, 0, 500, 500), gotRects, gotTexts)
	if els[1].SearchText() != "Save As" {
		t.Errorf("SearchText() = %q, want %q", els[1].SearchText(), "Save As")
	}
}
