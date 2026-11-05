//go:build integration && darwin

package vision

import (
	"image"
	"image/color"
	"image/draw"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/y3owk1n/neru/internal/adapter/platform/darwin"
)

func init() { runtime.LockOSThread() }

// TestMain services native dispatches without starting Neru or opening a UI.
func TestMain(m *testing.M) { os.Exit(darwin.RunMainLoopForTesting(m.Run)) }

func TestRecognizeFrameWords_KeepsTextAcrossTilesAndScale(t *testing.T) {
	if testing.Short() {
		t.Skip("native OCR model initialization is excluded from short runs")
	}

	parsed, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}

	face, err := opentype.NewFace(
		parsed,
		&opentype.FaceOptions{Size: 40, DPI: 72, Hinting: font.HintingFull},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = face.Close() }()

	warmImage := image.NewRGBA(image.Rect(0, 0, 800, 300))
	draw.Draw(warmImage, warmImage.Bounds(), image.White, image.Point{}, draw.Src)
	warmDrawer := font.Drawer{Dst: warmImage, Src: image.Black, Face: face, Dot: fixed.P(80, 110)}
	warmDrawer.DrawString("BatchMode")

	warmStarted := time.Now()
	warmWords := recognizeFrameWords(
		detectionFrame{img: warmImage, region: warmImage.Bounds(), scale: 1},
		90000,
	)
	t.Logf("native warmup: %d words in %v", len(warmWords), time.Since(warmStarted))

	if len(warmWords) == 0 {
		t.Fatal("native OCR model failed to initialize within its warmup budget")
	}

	for _, testCase := range []struct {
		name  string
		size  image.Point
		at    image.Point
		scale float64
	}{
		{"single_frame", image.Pt(800, 300), image.Pt(80, 110), 1},
		{"retina_scaled_coordinates", image.Pt(1600, 600), image.Pt(200, 220), 2},
		{"large_frame", image.Pt(3840, 800), image.Pt(2100, 110), 1},
		{"tile_seam", image.Pt(3840, 800), image.Pt(1820, 110), 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			img := image.NewRGBA(image.Rectangle{Max: testCase.size})
			draw.Draw(img, img.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
			drawer := font.Drawer{
				Dst:  img,
				Src:  image.Black,
				Face: face,
				Dot:  fixed.P(testCase.at.X, testCase.at.Y),
			}
			drawer.DrawString("BatchMode")

			logical := image.Rect(
				0,
				0,
				int(float64(testCase.size.X)/testCase.scale),
				int(float64(testCase.size.Y)/testCase.scale),
			)
			start := time.Now()
			words := recognizeFrameWords(detectionFrame{
				img: img, origin: image.Pt(200, 100),
				region: logical.Add(image.Pt(200, 100)), scale: testCase.scale,
			}, 5000)

			found := false
			for _, word := range words {
				t.Logf("synthetic word %q at %v", word.Text, word.Rect)

				if strings.EqualFold(word.Text, "BatchMode") {
					found = true

					target := image.Pt(
						int(float64(testCase.at.X+100)/testCase.scale),
						int(float64(testCase.at.Y-15)/testCase.scale),
					)
					if !target.In(word.Rect.Inset(-20)) {
						t.Errorf(
							"recognized bounds %v do not cover synthetic text near %v",
							word.Rect,
							target,
						)
					}
				}
			}

			if !found {
				t.Errorf(
					"synthetic BatchMode not recognized (%d words, %v)",
					len(words),
					time.Since(start),
				)
			}
		})
	}

	t.Run("timeout_owns_native_pixels", func(t *testing.T) {
		for range 5 {
			img := image.NewRGBA(image.Rect(0, 0, 3840, 2160))
			draw.Draw(img, img.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
			drawer := font.Drawer{Dst: img, Src: image.Black, Face: face, Dot: fixed.P(100, 110)}
			drawer.DrawString("BatchMode")

			start := time.Now()
			_ = recognizeFrameWords(detectionFrame{img: img, region: img.Bounds(), scale: 1}, 1)

			if time.Since(start) > time.Second {
				t.Fatal("native timeout did not return promptly")
			}

			runtime.GC()
		}
		// Let canceled native work finish after Go pixels have been collected.
		time.Sleep(200 * time.Millisecond)
	})
}
