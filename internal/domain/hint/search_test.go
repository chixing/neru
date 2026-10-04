package hint_test

import (
	"image"
	"testing"

	"github.com/y3owk1n/neru/internal/domain/element"
	"github.com/y3owk1n/neru/internal/domain/hint"
)

func TestFilterByText_ContainsFirstThenFuzzy(t *testing.T) {
	t.Parallel()

	const batchTitle = "Batch Mode"

	hints := make([]*hint.Interface, 0, 4)

	for index, title := range []string{batchTitle, "Mode", "Other", "bm"} {
		elem, err := element.NewElement(
			element.ID(title),
			image.Rect(0, index*20, 50, index*20+10),
			element.RoleButton,
			element.WithTitle(title),
		)
		if err != nil {
			t.Fatal(err)
		}

		h, err := hint.NewHint("aa"+string(rune('a'+index)), elem, image.Pt(0, index*20))
		if err != nil {
			t.Fatal(err)
		}

		hints = append(hints, h)
	}

	collection := hint.NewCollection(hints)

	got := collection.FilterByText("mode").All()
	if len(got) != 2 || got[0].Element().Title() != batchTitle ||
		got[1].Element().Title() != "Mode" {
		t.Fatalf("contains matches = %v", titles(got))
	}

	got = collection.FilterByText("bmode").All()
	if len(got) != 1 || got[0].Element().Title() != batchTitle {
		t.Fatalf("fuzzy matches = %v", titles(got))
	}

	// Two-letter queries stay literal, or they would match nearly everything.
	if got = collection.FilterByText("bo").All(); len(got) != 0 {
		t.Fatalf("short fuzzy matches = %v", titles(got))
	}
}

func titles(hints []*hint.Interface) []string {
	out := make([]string, 0, len(hints))
	for _, h := range hints {
		out = append(out, h.Element().Title())
	}

	return out
}
