package parser

import (
	"sync"
	"time"
)

// Line represents a complete line of terminal output after ANSI stripping
// and \r-overwrite handling.
type Line struct {
	Text    string
	Markers []OSCMarker // Any OSC 133 markers that occurred on/before this line
}

// LineCallback is called when the accumulator produces a complete line.
type LineCallback func(line Line)

// Accumulator buffers stripped terminal output into complete lines.
// It handles \r (carriage return without newline) for progress bars,
// \b (backspace), \t (tab), flushes on timeout for prompts without \n,
// and pauses during alternate screen mode.
type Accumulator struct {
	mu       sync.Mutex
	buf      []byte
	pos      int // cursor position within buf (for \r handling)
	markers  []OSCMarker
	callback LineCallback

	flushTimeout time.Duration
	flushTimer   *time.Timer

	paused bool // true when in alternate screen mode

	// stopCh signals the accumulator to stop
	stopCh chan struct{}
	once   sync.Once
}

// NewAccumulator creates a line accumulator that calls cb for each complete line.
// flushTimeout is how long to wait for more data before flushing a partial line
// (e.g., 500ms for detecting prompts without \n).
func NewAccumulator(flushTimeout time.Duration, cb LineCallback) *Accumulator {
	a := &Accumulator{
		buf:          make([]byte, 0, 1024),
		callback:     cb,
		flushTimeout: flushTimeout,
		stopCh:       make(chan struct{}),
	}
	return a
}

// Feed processes stripped terminal output and any OSC markers.
// It breaks the data into lines and calls the callback for each.
func (a *Accumulator) Feed(data []byte, markers []OSCMarker) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.paused {
		// During alternate screen mode, still pass through markers
		// but don't accumulate lines.
		for _, m := range markers {
			a.callback(Line{Markers: []OSCMarker{m}})
		}
		return
	}

	// When markers arrive, flush any pending text first so that
	// markers are processed by the conversation parser at the correct
	// boundary (not mixed with subsequent text).
	if len(markers) > 0 {
		if len(a.buf) > 0 {
			a.flushPartial()
		}
		// Emit markers as their own line callback
		a.callback(Line{Markers: markers})
	}

	for _, b := range data {
		a.processByte(b)
	}

	// Reset flush timer if there's data in the buffer
	if len(a.buf) > 0 {
		a.resetFlushTimer()
	}
}

func (a *Accumulator) processByte(b byte) {
	switch b {
	case '\n':
		// Complete line
		a.flushLine()
	case '\r':
		// Carriage return: move cursor to beginning of line
		// This enables progress bar overwriting
		a.pos = 0
	case '\b':
		// Backspace: move cursor left one position
		if a.pos > 0 {
			a.pos--
		}
	case '\t':
		// Tab: expand to next 8-column stop
		nextStop := ((a.pos / 8) + 1) * 8
		for a.pos < nextStop {
			a.writeByte(' ')
		}
	default:
		a.writeByte(b)
	}
}

func (a *Accumulator) writeByte(b byte) {
	if a.pos < len(a.buf) {
		a.buf[a.pos] = b
	} else {
		a.buf = append(a.buf, b)
	}
	a.pos++
}

func (a *Accumulator) flushLine() {
	a.cancelFlushTimer()

	text := ""
	if a.pos <= len(a.buf) && len(a.buf) > 0 {
		// Trim the buffer to the max position we've written to
		end := len(a.buf)
		if a.pos < end {
			// If cursor is before end, \r moved us back and we overwrote partially.
			// Keep all content up to the length of the buffer.
		}
		text = string(a.buf[:end])
	}

	line := Line{
		Text:    text,
		Markers: a.markers,
	}

	a.buf = a.buf[:0]
	a.pos = 0
	a.markers = nil

	a.callback(line)
}

func (a *Accumulator) resetFlushTimer() {
	a.cancelFlushTimer()
	a.flushTimer = time.AfterFunc(a.flushTimeout, func() {
		a.mu.Lock()
		defer a.mu.Unlock()

		select {
		case <-a.stopCh:
			return
		default:
		}

		if len(a.buf) > 0 || len(a.markers) > 0 {
			a.flushPartial()
		}
	})
}

func (a *Accumulator) cancelFlushTimer() {
	if a.flushTimer != nil {
		a.flushTimer.Stop()
		a.flushTimer = nil
	}
}

// flushPartial flushes the current buffer as a partial line (no trailing \n).
// This handles prompts that don't end with a newline.
func (a *Accumulator) flushPartial() {
	text := ""
	if len(a.buf) > 0 {
		end := len(a.buf)
		text = string(a.buf[:end])
	}

	line := Line{
		Text:    text,
		Markers: a.markers,
	}

	a.buf = a.buf[:0]
	a.pos = 0
	a.markers = nil

	a.callback(line)
}

// SetPaused pauses or resumes line accumulation (for alternate screen mode).
func (a *Accumulator) SetPaused(paused bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.paused && !paused {
		// Resuming — flush any pending data
		if len(a.buf) > 0 {
			a.flushPartial()
		}
	}
	a.paused = paused
}

// Flush forces any buffered data to be emitted.
func (a *Accumulator) Flush() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.buf) > 0 || len(a.markers) > 0 {
		a.flushPartial()
	}
}

// Close stops the accumulator and flushes remaining data.
func (a *Accumulator) Close() {
	a.once.Do(func() {
		close(a.stopCh)
	})
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cancelFlushTimer()
	if len(a.buf) > 0 || len(a.markers) > 0 {
		a.flushPartial()
	}
}
