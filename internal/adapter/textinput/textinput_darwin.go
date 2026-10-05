//go:build darwin

package textinput

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#include "../platform/darwin/textinput.h"
#include <stdlib.h>

extern void textInputQueryBridge(char* query, void* userData);
extern void textInputConfirmBridge(void* userData);
extern void textInputCancelBridge(void* userData);
extern void textInputCycleNextBridge(void* userData);
extern void textInputCyclePreviousBridge(void* userData);
*/
import "C"

import (
	"context"
	"sync"
	"sync/atomic"
	"unsafe"

	"go.uber.org/zap"

	// Blank import links the Objective-C bridge that defines
	// NeruStartHintSearchTextInput / NeruStopHintSearchTextInput. Without it the
	// package compiles but fails to link on its own — which is why this package
	// had no tests: `go test ./internal/adapter/textinput/` could not build a
	// test binary. keyfeed_darwin.go carries the same import for the same reason.
	_ "github.com/y3owk1n/neru/internal/adapter/platform/darwin"
	"github.com/y3owk1n/neru/internal/ports"
)

// TextInput manages the macOS native text input session.
type TextInput struct {
	logger    *zap.Logger
	query     string
	callbacks ports.TextInputCallbacks
	mu        sync.RWMutex

	querySeq        uint64
	lastExecutedSeq uint64
	callbackMu      sync.Mutex
}

var (
	globalTextInput   *TextInput
	globalTextInputMu sync.RWMutex
)

// NewTextInput creates a new TextInput.
func NewTextInput(logger *zap.Logger) *TextInput {
	textInput := &TextInput{logger: logger}

	globalTextInputMu.Lock()
	globalTextInput = textInput
	globalTextInputMu.Unlock()

	C.NeruSetHintSearchCycleCallbacks(
		C.TextInputControlCallback(C.textInputCycleNextBridge),
		C.TextInputControlCallback(C.textInputCyclePreviousBridge),
	)

	return textInput
}

// StartHintSearchSession starts the native hint search session.
func (t *TextInput) StartHintSearchSession(
	_ context.Context,
	callbacks ports.TextInputCallbacks,
	frame ports.TextInputFrame,
) (bool, error) {
	t.mu.Lock()
	t.callbacks = callbacks
	t.query = ""
	t.mu.Unlock()

	started := C.NeruStartHintSearchTextInput(
		C.TextInputQueryCallback(C.textInputQueryBridge),
		C.TextInputControlCallback(C.textInputConfirmBridge),
		C.TextInputControlCallback(C.textInputCancelBridge),
		C.int(frame.X),
		C.int(frame.Y),
		C.int(frame.Width),
		C.int(frame.Height),
		nil,
	)

	if started == 0 {
		return false, nil
	}

	return true, nil
}

// StopHintSearchSession stops the native hint search session.
func (t *TextInput) StopHintSearchSession(_ context.Context) error {
	t.mu.Lock()
	t.callbacks = ports.TextInputCallbacks{}
	t.mu.Unlock()

	C.NeruStopHintSearchTextInput()

	return nil
}

//export textInputQueryBridge
func textInputQueryBridge(query *C.char, _ unsafe.Pointer) {
	globalTextInputMu.RLock()
	textInput := globalTextInput
	globalTextInputMu.RUnlock()

	if textInput == nil {
		return
	}

	seq := atomic.AddUint64(&textInput.querySeq, 1)

	queryStr := ""
	if query != nil {
		queryStr = C.GoString(query)
	}

	textInput.mu.Lock()
	textInput.query = queryStr
	callback := textInput.callbacks.OnQueryChanged
	textInput.mu.Unlock()

	if callback == nil {
		return
	}

	go func(seq uint64, query string) {
		textInput.callbackMu.Lock()
		defer textInput.callbackMu.Unlock()

		if seq < textInput.lastExecutedSeq {
			return
		}
		textInput.lastExecutedSeq = seq

		callback(query)
	}(seq, queryStr)
}

//export textInputConfirmBridge
func textInputConfirmBridge(_ unsafe.Pointer) {
	globalTextInputMu.RLock()
	textInput := globalTextInput
	globalTextInputMu.RUnlock()

	if textInput == nil {
		return
	}

	textInput.mu.RLock()
	callback := textInput.callbacks.OnConfirm
	queryCallback, query := textInput.callbacks.OnQueryChanged, textInput.query
	seq := atomic.LoadUint64(&textInput.querySeq)
	textInput.mu.RUnlock()

	if callback == nil {
		return
	}

	go func() {
		textInput.callbackMu.Lock()
		defer textInput.callbackMu.Unlock()

		// The main thread has already published the full query. Deliver it
		// before the control even if its query goroutine has not run yet.
		if seq >= textInput.lastExecutedSeq {
			textInput.lastExecutedSeq = seq
			if queryCallback != nil {
				queryCallback(query)
			}
		}

		callback()
	}()
}

//export textInputCancelBridge
func textInputCancelBridge(_ unsafe.Pointer) {
	globalTextInputMu.RLock()
	textInput := globalTextInput
	globalTextInputMu.RUnlock()

	if textInput == nil {
		return
	}

	textInput.mu.RLock()
	callback := textInput.callbacks.OnCancel
	textInput.mu.RUnlock()

	if callback == nil {
		return
	}

	go func() {
		textInput.callbackMu.Lock()
		defer textInput.callbackMu.Unlock()

		callback()
	}()
}

//export textInputCycleNextBridge
func textInputCycleNextBridge(_ unsafe.Pointer) { dispatchCycle(false) }

//export textInputCyclePreviousBridge
func textInputCyclePreviousBridge(_ unsafe.Pointer) { dispatchCycle(true) }

func dispatchCycle(backward bool) {
	globalTextInputMu.RLock()
	textInput := globalTextInput
	globalTextInputMu.RUnlock()

	if textInput == nil {
		return
	}

	textInput.mu.RLock()
	callback := textInput.callbacks.OnCycle
	queryCallback, query := textInput.callbacks.OnQueryChanged, textInput.query
	seq := atomic.LoadUint64(&textInput.querySeq)
	textInput.mu.RUnlock()

	if callback == nil {
		return
	}

	go func() {
		textInput.callbackMu.Lock()
		defer textInput.callbackMu.Unlock()

		// The main thread has already published the full query. Deliver it
		// before the control even if its query goroutine has not run yet.
		if seq >= textInput.lastExecutedSeq {
			textInput.lastExecutedSeq = seq
			if queryCallback != nil {
				queryCallback(query)
			}
		}

		callback(backward)
	}()
}
