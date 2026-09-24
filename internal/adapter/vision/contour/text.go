package contour

import (
	"image"
	"strings"
)

// Word is one recognized word, in the same frame space as the detector's
// rectangles.
type Word struct {
	Rect image.Rectangle
	Text string
}

// Label gives each rectangle the words whose centers fall inside it, in the
// order recognition returned them. A word lands in the smallest rectangle
// holding it, so a button nested in a toolbar strip gets the button's text.
// Words no rectangle holds become rectangles of their own: text the detector
// missed is still a target. The returned texts run parallel to the returned
// rectangles.
func Label(rects []image.Rectangle, words []Word) ([]image.Rectangle, []string) {
	out := append([]image.Rectangle(nil), rects...)
	parts := make([][]string, len(out))

	// ponytail: O(words × rects) scan; a few hundred thousand checks on a
	// busy 4K window, index by row if it ever shows up in the timing log.
	for _, word := range words {
		center := word.Rect.Min.Add(word.Rect.Max).Div(2)
		best := -1

		for i, rect := range rects {
			if center.In(rect) && (best < 0 || area(rect) < area(rects[best])) {
				best = i
			}
		}

		if best < 0 {
			out = append(out, word.Rect)
			parts = append(parts, []string{word.Text})

			continue
		}

		parts[best] = append(parts[best], word.Text)
	}

	texts := make([]string, len(out))
	for i, p := range parts {
		texts[i] = strings.Join(p, " ")
	}

	return out, texts
}

func area(r image.Rectangle) int { return r.Dx() * r.Dy() }
