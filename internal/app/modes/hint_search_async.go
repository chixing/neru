package modes

import (
	"context"
	"image"
	"slices"
	"time"

	"go.uber.org/zap"

	"github.com/y3owk1n/neru/internal/derrors"
	"github.com/y3owk1n/neru/internal/domain"
	domainHint "github.com/y3owk1n/neru/internal/domain/hint"
	"github.com/y3owk1n/neru/internal/domain/modecmd"
	"github.com/y3owk1n/neru/internal/ports"
)

const hintSearchDebounce = 60 * time.Millisecond

// startHintSearchScan snapshots the request under h.mu. The worker touches no
// handler state until it re-enters through the outer locked completion method.
func (h *handlerState) startHintSearchScan(
	ctx context.Context,
	activation modecmd.Activation,
	bundleID string,
	overrides hintOverrides,
	screen image.Rectangle,
) {
	base := h.ctx

	h.hintScanWindow = image.Rectangle{}
	if bounds, ok := ports.DetectionWindow(ctx); ok {
		base = ports.WithDetectionWindow(base, bounds)
		h.hintScanWindow = bounds
	}

	scanCtx, cancel := context.WithTimeout(base, HintTimeout)
	h.hintScanCancel = cancel
	h.hintScanBundleID = bundleID
	h.hintScanGeneration++

	generation, session := h.hintScanGeneration, h.modeSession
	if h.hintScanGate == nil {
		h.hintScanGate = make(chan struct{}, 1)
	}

	gate := h.hintScanGate
	service, outer := h.hintService, h.outer
	roles := slices.Clone(activation.FilterRoles)
	texts := slices.Clone(activation.FilterTextContains)

	go func() {
		defer cancel()

		// Only background workers wait here. Native OCR may outlive its
		// canceled context, so repeated searches share one recognition slot.
		select {
		case gate <- struct{}{}:
			defer func() { <-gate }()
		case <-scanCtx.Done():
			outer.completeHintSearchScan(
				session,
				generation,
				nil,
				derrors.Wrap(
					scanCtx.Err(),
					derrors.CodeContextCanceled,
					"hint search scan canceled",
				),
			)

			return
		}

		if scanCtx.Err() != nil {
			outer.completeHintSearchScan(
				session,
				generation,
				nil,
				derrors.Wrap(
					scanCtx.Err(),
					derrors.CodeContextCanceled,
					"hint search scan canceled",
				),
			)

			return
		}

		hints, err := service.GenerateHints(
			scanCtx,
			roles,
			texts,
			bundleID,
			overrides.strategy,
			overrides.captureScope,
			overrides.labelDirection,
			overrides.splitWord,
		)
		hints = filterHintsForScreen(hints, screen)
		outer.completeHintSearchScan(session, generation, hints, err)
	}()
}

func (h *Handler) completeHintSearchScan(
	session, generation uint64,
	hints []*domainHint.Interface,
	err error,
) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.ctx.Err() != nil || h.modeSession != session || h.hintScanGeneration != generation ||
		h.appState.CurrentMode() != domain.ModeHints {
		return
	}

	h.hintScanCancel = nil
	if err != nil {
		h.logger.Warn("Hint search scan failed", zap.Error(err))
		h.exitMode()

		return
	}

	// Keep the input open even on an empty scan, so Escape still works and a
	// temporarily empty screen does not steal focus back during typing.
	h.hints.Context.SetSourceHints(domainHint.NewCollection(hints))
	h.cancelHintSearchFilter()

	if h.hints.Context.SearchActive() {
		h.applyHintSearchFilter()
	} else {
		matches := h.hints.Context.SourceHints().FilterByText(h.hints.Context.SearchQuery())

		labeled, labelErr := h.labelMatchesWith(matches, h.config.Hints.HintCharacters)
		if labelErr == nil {
			labelErr = h.hints.Context.SetVisibleHints(labeled)
		}

		h.cycleHintIndex = -1

		if labelErr != nil {
			h.logger.Warn("Failed to label refreshed search results", zap.Error(labelErr))
			h.exitMode()

			return
		}
	}

	h.startIndicatorPolling(domain.ModeHints)
	h.logger.Info("Hint search scan completed", zap.Int("hint_count", len(hints)))

	if h.hintSearchConfirmPending {
		h.hintSearchConfirmPending = false
		h.confirmHintSearch()
	}
}

func (h *handlerState) cancelHintScan() {
	h.hintScanGeneration++
	if h.hintScanCancel != nil {
		h.hintScanCancel()
		h.hintScanCancel = nil
	}

	h.hintSearchConfirmPending = false
}

// scheduleHintSearchFilter keeps text echo immediate and coalesces expensive
// marker/overlay updates. A timer that already fired is invalidated as well.
func (h *handlerState) scheduleHintSearchFilter() {
	h.cancelHintSearchFilter()
	h.hintSearchConfirmPending = false
	h.drawHintSearchInput()
	generation, session := h.hintSearchGeneration, h.hintSearchSession
	outer := h.outer
	h.hintSearchTimer = time.AfterFunc(hintSearchDebounce, func() {
		outer.mu.Lock()
		defer outer.mu.Unlock()

		if outer.hintSearchGeneration != generation || outer.hintSearchSession != session ||
			outer.appState.CurrentMode() != domain.ModeHints || !outer.hints.Context.SearchActive() {
			return
		}

		outer.hintSearchTimer = nil
		outer.applyHintSearchFilter()
	})
}

func (h *handlerState) cancelHintSearchFilter() {
	h.hintSearchGeneration++
	if h.hintSearchTimer != nil {
		h.hintSearchTimer.Stop()
		h.hintSearchTimer = nil
	}
}

func (h *handlerState) flushHintSearchFilter() {
	if h.hintSearchTimer != nil {
		h.cancelHintSearchFilter()
		h.applyHintSearchFilter()
	}
}

// cycleHintSearch validates and cycles under one lock hold, so an old native
// input callback cannot act on a newer search.
func (h *Handler) cycleHintSearch(ctx context.Context, backward bool, session uint64) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.hintSearchSession != session || h.appState.CurrentMode() != domain.ModeHints ||
		!h.hints.Context.SearchActive() || h.hintScanCancel != nil {
		return nil
	}

	h.flushHintSearchFilter()

	return h.cycleHint(ctx, backward, false)
}

// restartHintSearch keeps the mode, input session and query across a display
// refresh. Clearing only the visible collection prevents selecting stale points.
func (h *handlerState) restartHintSearch(ctx context.Context, screen image.Rectangle) bool {
	if h.hints == nil || h.hints.Context == nil || !h.hints.Context.StartWithSearch() {
		return false
	}

	h.cancelHintScan()
	h.cancelHintSearchFilter()
	h.setScreenBounds(screen)
	h.hintsFrameOnScreen = false

	clearErr := h.hints.Context.ClearVisibleHints()
	if clearErr != nil {
		h.logger.Warn("Failed to clear hints before search refresh", zap.Error(clearErr))
		h.exitMode()

		return true
	}

	if h.hints.Context.SearchActive() {
		h.drawHintSearchInput()
	}

	modeContext := h.hints.Context
	if !h.hintScanWindow.Empty() {
		ctx = ports.WithDetectionWindow(ctx, h.hintScanWindow)
	}

	h.startHintSearchScan(ctx, modecmd.Activation{
		FilterRoles:        slices.Clone(modeContext.FilterRoles()),
		FilterTextContains: slices.Clone(modeContext.FilterTextContains()),
	}, h.hintScanBundleID, hintOverrides{
		strategy: modeContext.ActiveStrategy(), captureScope: modeContext.ActiveCaptureScope(),
		labelDirection: modeContext.LabelDirectionOverride(), splitWord: modeContext.SplitWord(),
	}, screen)

	return true
}
