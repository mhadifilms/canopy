package parser

import (
	"sync"
	"testing"
	"time"
)

func collectLines(t *testing.T, count int, timeout time.Duration, feed func(cb LineCallback)) []Line {
	t.Helper()
	var mu sync.Mutex
	var lines []Line
	done := make(chan struct{})

	cb := func(line Line) {
		mu.Lock()
		lines = append(lines, line)
		if len(lines) >= count {
			select {
			case <-done:
			default:
				close(done)
			}
		}
		mu.Unlock()
	}

	feed(cb)

	select {
	case <-done:
	case <-time.After(timeout):
	}

	mu.Lock()
	defer mu.Unlock()
	return lines
}

func TestAccumulator_SimpleLine(t *testing.T) {
	lines := collectLines(t, 1, time.Second, func(cb LineCallback) {
		a := NewAccumulator(500*time.Millisecond, cb)
		defer a.Close()
		a.Feed([]byte("hello world\n"), nil)
	})

	if len(lines) < 1 {
		t.Fatal("expected at least 1 line")
	}
	if lines[0].Text != "hello world" {
		t.Errorf("expected 'hello world', got %q", lines[0].Text)
	}
}

func TestAccumulator_MultipleLines(t *testing.T) {
	lines := collectLines(t, 3, time.Second, func(cb LineCallback) {
		a := NewAccumulator(500*time.Millisecond, cb)
		defer a.Close()
		a.Feed([]byte("line1\nline2\nline3\n"), nil)
	})

	if len(lines) < 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	expected := []string{"line1", "line2", "line3"}
	for i, exp := range expected {
		if lines[i].Text != exp {
			t.Errorf("line %d: expected %q, got %q", i, exp, lines[i].Text)
		}
	}
}

func TestAccumulator_CarriageReturn(t *testing.T) {
	// Progress bar: "50%\r75%\n" should produce "75%"
	lines := collectLines(t, 1, time.Second, func(cb LineCallback) {
		a := NewAccumulator(500*time.Millisecond, cb)
		defer a.Close()
		a.Feed([]byte("50%\r75%\n"), nil)
	})

	if len(lines) < 1 {
		t.Fatal("expected at least 1 line")
	}
	if lines[0].Text != "75%" {
		t.Errorf("expected '75%%', got %q", lines[0].Text)
	}
}

func TestAccumulator_CarriageReturnPartialOverwrite(t *testing.T) {
	// "abcdef\rXY\n" should produce "XYcdef"
	lines := collectLines(t, 1, time.Second, func(cb LineCallback) {
		a := NewAccumulator(500*time.Millisecond, cb)
		defer a.Close()
		a.Feed([]byte("abcdef\rXY\n"), nil)
	})

	if len(lines) < 1 {
		t.Fatal("expected at least 1 line")
	}
	if lines[0].Text != "XYcdef" {
		t.Errorf("expected 'XYcdef', got %q", lines[0].Text)
	}
}

func TestAccumulator_Backspace(t *testing.T) {
	// "abc\bd\n" — cursor moves back, then 'd' overwrites 'c' → "abd"
	lines := collectLines(t, 1, time.Second, func(cb LineCallback) {
		a := NewAccumulator(500*time.Millisecond, cb)
		defer a.Close()
		a.Feed([]byte("abc\bd\n"), nil)
	})

	if len(lines) < 1 {
		t.Fatal("expected at least 1 line")
	}
	if lines[0].Text != "abd" {
		t.Errorf("expected 'abd', got %q", lines[0].Text)
	}
}

func TestAccumulator_Tab(t *testing.T) {
	// "a\tb\n" — tab expands to spaces (next 8-col stop)
	lines := collectLines(t, 1, time.Second, func(cb LineCallback) {
		a := NewAccumulator(500*time.Millisecond, cb)
		defer a.Close()
		a.Feed([]byte("a\tb\n"), nil)
	})

	if len(lines) < 1 {
		t.Fatal("expected at least 1 line")
	}
	// 'a' is at position 0, tab goes to position 8, then 'b' at 8
	expected := "a       b"
	if lines[0].Text != expected {
		t.Errorf("expected %q, got %q", expected, lines[0].Text)
	}
}

func TestAccumulator_FlushTimeout(t *testing.T) {
	// Data without \n should flush after timeout
	lines := collectLines(t, 1, 2*time.Second, func(cb LineCallback) {
		a := NewAccumulator(100*time.Millisecond, cb)
		defer a.Close()
		a.Feed([]byte("prompt> "), nil)
		// Wait for flush timeout
		time.Sleep(300 * time.Millisecond)
	})

	if len(lines) < 1 {
		t.Fatal("expected at least 1 line from timeout flush")
	}
	if lines[0].Text != "prompt> " {
		t.Errorf("expected 'prompt> ', got %q", lines[0].Text)
	}
}

func TestAccumulator_MarkersPassedThrough(t *testing.T) {
	markers := []OSCMarker{{Code: 'A', Position: 0}}

	lines := collectLines(t, 2, time.Second, func(cb LineCallback) {
		a := NewAccumulator(500*time.Millisecond, cb)
		defer a.Close()
		a.Feed([]byte("prompt\n"), markers)
	})

	// Markers are emitted as a separate line before the text
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines (marker + text), got %d", len(lines))
	}
	// First line should be the marker
	if len(lines[0].Markers) != 1 {
		t.Fatalf("expected 1 marker in first line, got %d", len(lines[0].Markers))
	}
	if lines[0].Markers[0].Code != 'A' {
		t.Errorf("expected marker 'A', got %c", lines[0].Markers[0].Code)
	}
	// Second line should be the text
	if lines[1].Text != "prompt" {
		t.Errorf("expected text 'prompt', got %q", lines[1].Text)
	}
}

func TestAccumulator_PausedDuringAltScreen(t *testing.T) {
	var mu sync.Mutex
	var lines []Line

	a := NewAccumulator(500*time.Millisecond, func(line Line) {
		mu.Lock()
		lines = append(lines, line)
		mu.Unlock()
	})
	defer a.Close()

	a.SetPaused(true)
	a.Feed([]byte("vim content\n"), nil)

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	count := len(lines)
	mu.Unlock()

	if count != 0 {
		t.Errorf("expected 0 lines while paused, got %d", count)
	}
}

func TestAccumulator_PausedMarkersStillEmitted(t *testing.T) {
	markers := []OSCMarker{{Code: 'A', Position: 0}}

	lines := collectLines(t, 1, time.Second, func(cb LineCallback) {
		a := NewAccumulator(500*time.Millisecond, cb)
		defer a.Close()
		a.SetPaused(true)
		a.Feed([]byte("ignored text\n"), markers)
	})

	if len(lines) < 1 {
		t.Fatal("expected marker line even when paused")
	}
	if len(lines[0].Markers) != 1 {
		t.Errorf("expected 1 marker, got %d", len(lines[0].Markers))
	}
	if lines[0].Text != "" {
		t.Errorf("expected empty text while paused, got %q", lines[0].Text)
	}
}

func TestAccumulator_CloseFlushes(t *testing.T) {
	var mu sync.Mutex
	var lines []Line

	a := NewAccumulator(10*time.Second, func(line Line) {
		mu.Lock()
		lines = append(lines, line)
		mu.Unlock()
	})

	a.Feed([]byte("pending data"), nil)
	a.Close()

	mu.Lock()
	defer mu.Unlock()

	if len(lines) < 1 {
		t.Fatal("expected Close to flush pending data")
	}
	if lines[0].Text != "pending data" {
		t.Errorf("expected 'pending data', got %q", lines[0].Text)
	}
}
