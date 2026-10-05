package modes

import (
	"context"
	"image"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/y3owk1n/neru/internal/app/components"
	hintscomponent "github.com/y3owk1n/neru/internal/app/components/hints"
	scrollcomponent "github.com/y3owk1n/neru/internal/app/components/scroll"
	"github.com/y3owk1n/neru/internal/app/services"
	"github.com/y3owk1n/neru/internal/config"
	"github.com/y3owk1n/neru/internal/domain"
	"github.com/y3owk1n/neru/internal/domain/element"
	domainhint "github.com/y3owk1n/neru/internal/domain/hint"
	"github.com/y3owk1n/neru/internal/domain/modecmd"
	"github.com/y3owk1n/neru/internal/domain/state"
	"github.com/y3owk1n/neru/internal/ports"
	portmocks "github.com/y3owk1n/neru/internal/ports/mocks"
)

// newHintSearchTestHandler builds a handler sitting in hints mode with one hint
// found, which is everything starting a search needs, over the overlay and text
// input given.
func newHintSearchTestHandler(
	t *testing.T,
	overlayPort ports.OverlayPort,
	textInput ports.TextInputPort,
) *Handler {
	t.Helper()

	appState := state.NewAppState()
	appState.SetMode(domain.ModeHints)

	handler := newHandlerWithState(handlerState{
		ctx:         context.Background(),
		logger:      zap.NewNop(),
		appState:    appState,
		cursorState: state.NewCursorState(),
		scroll:      &components.ScrollComponent{Context: &scrollcomponent.Context{}},
		hints: &components.HintsComponent{
			Context: &hintscomponent.Context{},
		},
		overlayPort: overlayPort,
		textInput:   textInput,
		modes:       map[domain.Mode]Mode{},
	})

	elem, elemErr := element.NewElement("search", image.Rect(0, 0, 20, 20), element.RoleButton)
	if elemErr != nil {
		t.Fatalf("NewElement() error = %v", elemErr)
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()

	handler.hints.Context.SetManager(domainhint.NewManager(handler.logger, &handler.mu))

	setErr := handler.hints.Context.SetHints(
		domainhint.NewCollection([]*domainhint.Interface{mustNewModeHint("AA", elem)}),
	)
	if setErr != nil {
		t.Fatalf("SetHints() error = %v", setErr)
	}

	return handler
}

// TestStartHintSearch_PlacesTheTextInputOverTheDrawnBox is the ordinary case:
// the overlay says where it put the search box and the platform's input field
// is placed over exactly that rectangle, rather than deriving the placement a
// second time here.
func TestStartHintSearch_PlacesTheTextInputOverTheDrawnBox(t *testing.T) {
	t.Parallel()

	box := image.Rect(410, 740, 610, 780)
	overlayPort := &portmocks.MockOverlayPort{
		HintSearchBoundsFunc: func(image.Rectangle) image.Rectangle { return box },
	}
	textInput := &portmocks.MockTextInputPort{Started: true}

	handler := newHintSearchTestHandler(t, overlayPort, textInput)

	handler.mu.Lock()
	err := handler.startHintSearch()
	handler.mu.Unlock()

	if err != nil {
		t.Fatalf("startHintSearch() error = %v", err)
	}

	if textInput.StartCount() != 1 {
		t.Fatalf("text input started %d times, want 1", textInput.StartCount())
	}

	want := ports.TextInputFrame{X: box.Min.X, Y: box.Min.Y, Width: box.Dx(), Height: box.Dy()}
	if got := textInput.Frame(); got != want {
		t.Errorf("text input frame = %+v, want %+v", got, want)
	}
}

// TestStartHintSearch_NoBoxOnScreenStartsNoTextInput pins the degradation that
// comes with the overlay being able to refuse a search input it cannot place
// (#1329). An empty rectangle means nothing was drawn, and the platform's field
// is not sized from the box it sits over — macOS substitutes a 16x16 one at the
// screen origin — so starting the session anyway would hand the keyboard to an
// invisible input in a corner while the event tap is switched off. Search still
// works: the query arrives through the key stream, the way it does on every
// overlay that draws no search box at all.
func TestStartHintSearch_NoBoxOnScreenStartsNoTextInput(t *testing.T) {
	t.Parallel()

	overlayPort := &portmocks.MockOverlayPort{
		HintSearchBoundsFunc: func(image.Rectangle) image.Rectangle { return image.Rectangle{} },
	}
	textInput := &portmocks.MockTextInputPort{Started: true}

	handler := newHintSearchTestHandler(t, overlayPort, textInput)

	handler.mu.Lock()
	err := handler.startHintSearch()
	searchActive := handler.hints.Context.SearchActive()
	textInputActive := handler.hintSearchTextInputActive
	handler.mu.Unlock()

	if err != nil {
		t.Fatalf("startHintSearch() error = %v", err)
	}

	if textInput.StartCount() != 0 {
		t.Errorf(
			"text input started %d times with no search box on screen, want 0",
			textInput.StartCount(),
		)
	}

	if textInputActive {
		t.Error("the handler recorded an active text input session that was never started")
	}

	// The search itself still opened — only the native field was skipped.
	if !searchActive {
		t.Error("hint search is not active; the key stream has nothing to filter")
	}
}

// TestStartHintSearch_NoBoxOnScreenGivesTheKeyboardBack pins the half of that
// degradation the user would notice. Starting a search stops any live session
// while deliberately leaving the event tap off, because the session about to
// start wants it off. When no session starts, the tap has to come back on: it
// is the only thing left that can deliver a key, and hints mode with the
// keyboard switched off and nothing listening is indistinguishable from a hang.
func TestStartHintSearch_NoBoxOnScreenGivesTheKeyboardBack(t *testing.T) {
	t.Parallel()

	enables := 0
	eventTap := &portmocks.MockEventTapPort{
		EnableFunc: func(context.Context) error {
			enables++

			return nil
		},
	}

	overlayPort := &portmocks.MockOverlayPort{
		HintSearchBoundsFunc: func(image.Rectangle) image.Rectangle { return image.Rectangle{} },
	}

	handler := newHintSearchTestHandler(
		t,
		overlayPort,
		&portmocks.MockTextInputPort{Started: true},
	)

	handler.mu.Lock()
	// The state a live session leaves behind: the tap is off and the handler
	// knows it turned it off.
	handler.eventTap = eventTap
	handler.hintSearchTextInputActive = true
	handler.hintSearchEventTapDisabled = true

	err := handler.startHintSearch()
	stillDisabled := handler.hintSearchEventTapDisabled
	handler.mu.Unlock()

	if err != nil {
		t.Fatalf("startHintSearch() error = %v", err)
	}

	if enables != 1 {
		t.Errorf("event tap enabled %d times, want 1: the keyboard is still switched off", enables)
	}

	if stillDisabled {
		t.Error("the handler still believes it owes the event tap a re-enable")
	}
}

// TestHintsModeRefreshForThemeChange_RedrawsAnOpenSearchBox pins the half of a
// theme refresh a redraw of the labels alone would lose. On the backends that
// paint the search box onto the same surface as the labels — Linux and Windows
// — that redraw clears it, so a theme change mid-search would take the box off
// the screen while the search is still running and still taking keys.
func TestHintsModeRefreshForThemeChange_RedrawsAnOpenSearchBox(t *testing.T) {
	t.Parallel()

	searchDraws := 0
	overlayPort := &portmocks.MockOverlayPort{
		DrawHintSearchFunc: func(ports.HintSearch) error {
			searchDraws++

			return nil
		},
		HintSearchBoundsFunc: func(image.Rectangle) image.Rectangle { return image.Rectangle{} },
	}

	handler := newHintSearchTestHandler(t, overlayPort, &portmocks.MockTextInputPort{})
	mode := &HintsMode{baseMode: baseMode{handler: &handler.handlerState}}

	handler.mu.Lock()
	defer handler.mu.Unlock()

	if !mode.RefreshForThemeChange() {
		t.Fatal("RefreshForThemeChange() = false with hints on screen")
	}

	if searchDraws != 0 {
		t.Errorf("the search box was drawn %d times with no search open, want 0", searchDraws)
	}

	handler.hints.Context.SetSearchActive(true)

	if !mode.RefreshForThemeChange() {
		t.Fatal("RefreshForThemeChange() = false with hints on screen")
	}

	if searchDraws != 1 {
		t.Errorf(
			"the search box was drawn %d times after a theme change, want 1: "+
				"the label redraw cleared it",
			searchDraws,
		)
	}
}

func TestStartHintSearchScan_EditingAndCancelDoNotWaitForScan(t *testing.T) {
	t.Parallel()

	started, canceled := make(chan struct{}), make(chan struct{})
	textInput := &portmocks.MockTextInputPort{Started: true}
	handler := newHintSearchTestHandler(t, &portmocks.MockOverlayPort{
		HintSearchBoundsFunc: func(image.Rectangle) image.Rectangle { return image.Rect(0, 0, 200, 30) },
	}, textInput)
	cfg := config.DefaultConfig()

	gen, err := domainhint.NewAlphabetGenerator("asdf", domainhint.LabelDirectionNormal)
	if err != nil {
		t.Fatal(err)
	}

	handler.hintService = services.NewHintService(&portmocks.MockAccessibilityPort{
		ClickableElementsFunc: func(ctx context.Context, _ ports.ElementFilter) ([]*element.Element, error) {
			close(started)
			<-ctx.Done()
			close(canceled)

			return nil, ctx.Err()
		},
	}, nil, nil, gen, cfg.Hints, nil, nil)
	handler.modes[domain.ModeHints] = NewHintsMode(&handler.handlerState)

	handler.mu.Lock()
	handler.hints.Context.SetStartWithSearch(true)

	err = handler.startHintSearch()
	if err != nil {
		t.Fatal(err)
	}

	handler.startHintSearchScan(
		context.Background(),
		modecmd.Activation{},
		"test.app",
		hintOverrides{},
		image.Rect(0, 0, 1000, 1000),
	)
	handler.mu.Unlock()

	awaitHintSearchSignal(t, started)

	done := make(chan struct{})
	go func() {
		textInput.EmitQueryChanged("typed during scan")
		textInput.EmitCancel()
		close(done)
	}()

	awaitHintSearchSignal(t, done)
	awaitHintSearchSignal(t, canceled)

	if handler.appState.CurrentMode() != domain.ModeIdle {
		t.Fatal("Escape did not leave search while its scan was blocked")
	}
}

func TestCompleteHintSearchScan_StaleScanDoesNotReplaceNewSearch(t *testing.T) {
	t.Parallel()

	handler := newHintSearchTestHandler(t, &portmocks.MockOverlayPort{}, nil)
	original := handler.hints.Context.SourceHints()
	handler.hintScanGeneration = 2
	handler.completeHintSearchScan(handler.modeSession, 1, nil, nil)

	if handler.hints.Context.SourceHints() != original {
		t.Fatal("old scan replaced the newer search's hints")
	}
}

func TestHintSearchFilter_DebouncesAndFlushesBeforeConfirm(t *testing.T) {
	t.Parallel()

	handler := newHintSearchTestHandler(t, &portmocks.MockOverlayPort{}, nil)
	handler.mu.Lock()
	defer handler.mu.Unlock()

	handler.hints.Context.SetSearchActive(true)
	original := handler.hints.Context.Hints()
	handler.hints.Context.SetSearchQuery("no match")
	handler.scheduleHintSearchFilter()

	if handler.hints.Context.Hints() != original {
		t.Fatal("typing filtered synchronously instead of debouncing")
	}

	handler.confirmHintSearch()

	if handler.hints.Context.Hints().Count() != 0 || handler.hintSearchTimer != nil {
		t.Fatal("Return did not flush the current query")
	}

	if !handler.hints.Context.SearchActive() {
		t.Fatal("Return on no matches closed the input")
	}
}

func TestHintSearchFilter_TimerAppliesLatestQuery(t *testing.T) {
	t.Parallel()

	applied := make(chan struct{}, 1)
	handler := newHintSearchTestHandler(t, &portmocks.MockOverlayPort{}, nil)
	handler.mu.Lock()
	handler.hints.Context.Manager().SetUpdateCallback(func([]*domainhint.Interface) {
		select {
		case applied <- struct{}{}:
		default:
		}
	})
	handler.hints.Context.SetSearchActive(true)
	handler.hints.Context.SetSearchQuery("search")
	handler.scheduleHintSearchFilter()
	handler.hints.Context.SetSearchQuery("no match")
	handler.scheduleHintSearchFilter()
	handler.mu.Unlock()
	awaitHintSearchSignal(t, applied)
	handler.mu.Lock()
	defer handler.mu.Unlock()

	if handler.hints.Context.Hints().Count() != 0 {
		t.Fatal("debounce applied an outdated query")
	}
}

func TestConfirmHintSearch_ScanPendingKeepsInputOpen(t *testing.T) {
	t.Parallel()

	handler := newHintSearchTestHandler(t, &portmocks.MockOverlayPort{}, nil)
	handler.mu.Lock()
	handler.hints.Context.SetSearchActive(true)
	handler.hintScanCancel = func() {}
	handler.confirmHintSearch()
	pending := handler.hintSearchConfirmPending
	handler.mu.Unlock()

	if !pending {
		t.Fatal("Return during scanning did not defer confirmation")
	}

	handler.completeHintSearchScan(handler.modeSession, handler.hintScanGeneration, nil, nil)
	handler.mu.Lock()
	defer handler.mu.Unlock()

	if handler.hintSearchConfirmPending || !handler.hints.Context.SearchActive() {
		t.Fatal("empty scan did not settle pending Return while preserving input")
	}
}

func awaitHintSearchSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("hint search operation blocked")
	}
}

func TestRestartHintSearch_PreservesQueryAndKeyboardDuringScan(t *testing.T) {
	t.Parallel()

	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)

	handler := newHintSearchTestHandler(t, &portmocks.MockOverlayPort{}, nil)
	cfg := config.DefaultConfig()

	gen, err := domainhint.NewAlphabetGenerator("asdf", domainhint.LabelDirectionNormal)
	if err != nil {
		t.Fatal(err)
	}

	handler.hintService = services.NewHintService(&portmocks.MockAccessibilityPort{
		ClickableElementsFunc: func(ctx context.Context, _ ports.ElementFilter) ([]*element.Element, error) {
			close(started)

			select {
			case <-release:
			case <-ctx.Done():
			}

			return nil, ctx.Err()
		},
	}, nil, nil, gen, cfg.Hints, nil, nil)
	handler.mu.Lock()
	handler.hints.Context.SetStartWithSearch(true)
	handler.hints.Context.SetSearchActive(true)
	handler.hints.Context.SetSearchQuery("keep this query")
	session := handler.modeSession
	searchSession := handler.hintSearchSession

	handler.hintScanBundleID = "test.app"
	if !handler.restartHintSearch(t.Context(), image.Rect(0, 0, 1000, 1000)) {
		t.Fatal("search refresh was not handled")
	}
	handler.mu.Unlock()
	awaitHintSearchSignal(t, started)
	handler.mu.Lock()
	defer handler.mu.Unlock()
	defer handler.cancelHintScan()

	if handler.modeSession != session || handler.hintSearchSession != searchSession ||
		handler.appState.CurrentMode() != domain.ModeHints || handler.hints.Context.SearchQuery() != "keep this query" {
		t.Fatal("display refresh reset the mode, keyboard session or query")
	}
}
