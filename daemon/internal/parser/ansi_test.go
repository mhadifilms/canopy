package parser

import (
	"testing"
)

func TestANSIStripper_PlainText(t *testing.T) {
	s := NewANSIStripper()
	stripped, markers := s.Feed([]byte("hello world"))
	if string(stripped) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(stripped))
	}
	if len(markers) != 0 {
		t.Errorf("expected no markers, got %d", len(markers))
	}
}

func TestANSIStripper_SGRColor(t *testing.T) {
	s := NewANSIStripper()
	// Bold red text: ESC[1;31m Hello ESC[0m
	input := "\x1b[1;31mHello\x1b[0m"
	stripped, markers := s.Feed([]byte(input))
	if string(stripped) != "Hello" {
		t.Errorf("expected 'Hello', got %q", string(stripped))
	}
	if len(markers) != 0 {
		t.Errorf("expected no markers, got %d", len(markers))
	}
}

func TestANSIStripper_CursorMovement(t *testing.T) {
	s := NewANSIStripper()
	// Move cursor up 2 lines: ESC[2A, then some text
	input := "\x1b[2AHello"
	stripped, _ := s.Feed([]byte(input))
	if string(stripped) != "Hello" {
		t.Errorf("expected 'Hello', got %q", string(stripped))
	}
}

func TestANSIStripper_OSC133_PromptStart(t *testing.T) {
	s := NewANSIStripper()
	// OSC 133;A BEL
	input := "\x1b]133;A\x07"
	stripped, markers := s.Feed([]byte(input))
	if string(stripped) != "" {
		t.Errorf("expected empty stripped, got %q", string(stripped))
	}
	if len(markers) != 1 {
		t.Fatalf("expected 1 marker, got %d", len(markers))
	}
	if markers[0].Code != 'A' {
		t.Errorf("expected marker code 'A', got %c", markers[0].Code)
	}
}

func TestANSIStripper_OSC133_CommandStart(t *testing.T) {
	s := NewANSIStripper()
	// OSC 133;C BEL
	input := "\x1b]133;C\x07"
	stripped, markers := s.Feed([]byte(input))
	if string(stripped) != "" {
		t.Errorf("expected empty stripped, got %q", string(stripped))
	}
	if len(markers) != 1 {
		t.Fatalf("expected 1 marker, got %d", len(markers))
	}
	if markers[0].Code != 'C' {
		t.Errorf("expected marker code 'C', got %c", markers[0].Code)
	}
}

func TestANSIStripper_OSC133_CommandDone(t *testing.T) {
	s := NewANSIStripper()
	// OSC 133;D;0 BEL (exit code 0)
	input := "\x1b]133;D;0\x07"
	stripped, markers := s.Feed([]byte(input))
	if string(stripped) != "" {
		t.Errorf("expected empty stripped, got %q", string(stripped))
	}
	if len(markers) != 1 {
		t.Fatalf("expected 1 marker, got %d", len(markers))
	}
	if markers[0].Code != 'D' {
		t.Errorf("expected marker code 'D', got %c", markers[0].Code)
	}
	if markers[0].Param != "0" {
		t.Errorf("expected param '0', got %q", markers[0].Param)
	}
}

func TestANSIStripper_OSC133_ExitCode127(t *testing.T) {
	s := NewANSIStripper()
	input := "\x1b]133;D;127\x07"
	_, markers := s.Feed([]byte(input))
	if len(markers) != 1 {
		t.Fatalf("expected 1 marker, got %d", len(markers))
	}
	if markers[0].Param != "127" {
		t.Errorf("expected param '127', got %q", markers[0].Param)
	}
}

func TestANSIStripper_OSC133_STTerminator(t *testing.T) {
	s := NewANSIStripper()
	// OSC 133;A terminated by ST (ESC \)
	input := "\x1b]133;A\x1b\\"
	stripped, markers := s.Feed([]byte(input))
	if string(stripped) != "" {
		t.Errorf("expected empty stripped, got %q", string(stripped))
	}
	if len(markers) != 1 {
		t.Fatalf("expected 1 marker, got %d", len(markers))
	}
	if markers[0].Code != 'A' {
		t.Errorf("expected marker code 'A', got %c", markers[0].Code)
	}
}

func TestANSIStripper_AltScreenEnter(t *testing.T) {
	s := NewANSIStripper()
	if s.AltScreen {
		t.Error("expected AltScreen false initially")
	}
	// Enter alternate screen: ESC[?1049h
	s.Feed([]byte("\x1b[?1049h"))
	if !s.AltScreen {
		t.Error("expected AltScreen true after entering")
	}
}

func TestANSIStripper_AltScreenLeave(t *testing.T) {
	s := NewANSIStripper()
	s.Feed([]byte("\x1b[?1049h"))
	if !s.AltScreen {
		t.Fatal("expected AltScreen true")
	}
	s.Feed([]byte("\x1b[?1049l"))
	if s.AltScreen {
		t.Error("expected AltScreen false after leaving")
	}
}

func TestANSIStripper_MixedContent(t *testing.T) {
	s := NewANSIStripper()
	// Simulates: colored prompt, OSC 133;A, then "$ " prompt text
	input := "\x1b[32m\x1b]133;A\x07$ \x1b[0m"
	stripped, markers := s.Feed([]byte(input))
	if string(stripped) != "$ " {
		t.Errorf("expected '$ ', got %q", string(stripped))
	}
	if len(markers) != 1 {
		t.Fatalf("expected 1 marker, got %d", len(markers))
	}
	if markers[0].Code != 'A' {
		t.Errorf("expected marker code 'A', got %c", markers[0].Code)
	}
}

func TestANSIStripper_ProgressBar(t *testing.T) {
	s := NewANSIStripper()
	// Progress bar: "Downloading... 50%\rDownloading... 75%"
	input := "Downloading... 50%\rDownloading... 75%"
	stripped, _ := s.Feed([]byte(input))
	// \r is preserved for the accumulator to handle
	if string(stripped) != "Downloading... 50%\rDownloading... 75%" {
		t.Errorf("expected CR preserved, got %q", string(stripped))
	}
}

func TestANSIStripper_Backspace(t *testing.T) {
	s := NewANSIStripper()
	// "abc\b" — backspace is preserved for accumulator
	input := "abc\b"
	stripped, _ := s.Feed([]byte(input))
	if string(stripped) != "abc\b" {
		t.Errorf("expected backspace preserved, got %q", string(stripped))
	}
}

func TestANSIStripper_Tab(t *testing.T) {
	s := NewANSIStripper()
	input := "a\tb"
	stripped, _ := s.Feed([]byte(input))
	if string(stripped) != "a\tb" {
		t.Errorf("expected tab preserved, got %q", string(stripped))
	}
}

func TestANSIStripper_MultipleMarkers(t *testing.T) {
	s := NewANSIStripper()
	// Full command cycle: 133;A (prompt) then 133;C (exec start)
	input := "\x1b]133;A\x07$ ls\x1b]133;C\x07"
	stripped, markers := s.Feed([]byte(input))
	if string(stripped) != "$ ls" {
		t.Errorf("expected '$ ls', got %q", string(stripped))
	}
	if len(markers) != 2 {
		t.Fatalf("expected 2 markers, got %d", len(markers))
	}
	if markers[0].Code != 'A' {
		t.Errorf("expected first marker 'A', got %c", markers[0].Code)
	}
	if markers[1].Code != 'C' {
		t.Errorf("expected second marker 'C', got %c", markers[1].Code)
	}
}

func TestANSIStripper_OtherOSCIgnored(t *testing.T) {
	s := NewANSIStripper()
	// OSC 0 (set title): ESC ] 0;My Title BEL
	input := "\x1b]0;My Title\x07some text"
	stripped, markers := s.Feed([]byte(input))
	if string(stripped) != "some text" {
		t.Errorf("expected 'some text', got %q", string(stripped))
	}
	if len(markers) != 0 {
		t.Errorf("expected no markers for OSC 0, got %d", len(markers))
	}
}

func TestANSIStripper_NullBytesStripped(t *testing.T) {
	s := NewANSIStripper()
	input := "hel\x00lo"
	stripped, _ := s.Feed([]byte(input))
	if string(stripped) != "hello" {
		t.Errorf("expected 'hello', got %q", string(stripped))
	}
}

func TestANSIStripper_EmptyInput(t *testing.T) {
	s := NewANSIStripper()
	stripped, markers := s.Feed([]byte{})
	if len(stripped) != 0 {
		t.Errorf("expected empty output, got %q", string(stripped))
	}
	if len(markers) != 0 {
		t.Errorf("expected no markers, got %d", len(markers))
	}
}

func TestANSIStripper_RealTerminalOutput(t *testing.T) {
	s := NewANSIStripper()
	// Simulates real `ls --color` output with SGR codes
	input := "\x1b[1;34mdir1\x1b[0m  \x1b[1;34mdir2\x1b[0m  \x1b[0mfile.txt\x1b[0m\n"
	stripped, _ := s.Feed([]byte(input))
	if string(stripped) != "dir1  dir2  file.txt\n" {
		t.Errorf("expected plain ls output, got %q", string(stripped))
	}
}

func TestANSIStripper_IncrementalFeeding(t *testing.T) {
	s := NewANSIStripper()

	// Feed an ESC sequence across two calls
	s1, m1 := s.Feed([]byte("\x1b"))
	s2, m2 := s.Feed([]byte("]133;A\x07hello"))

	if len(s1) != 0 {
		t.Errorf("expected no output from first feed, got %q", string(s1))
	}
	if len(m1) != 0 {
		t.Errorf("expected no markers from first feed, got %d", len(m1))
	}
	if string(s2) != "hello" {
		t.Errorf("expected 'hello' from second feed, got %q", string(s2))
	}
	if len(m2) != 1 {
		t.Fatalf("expected 1 marker from second feed, got %d", len(m2))
	}
	if m2[0].Code != 'A' {
		t.Errorf("expected marker 'A', got %c", m2[0].Code)
	}
}

// FuzzANSIStripper ensures the ANSI stripper never panics on arbitrary input.
func FuzzANSIStripper(f *testing.F) {
	f.Add([]byte("hello world"))
	f.Add([]byte("\x1b[31mred\x1b[0m"))
	f.Add([]byte("\x1b]133;A\x07"))
	f.Add([]byte("\x1b]133;D;0\x07"))
	f.Add([]byte("\x1b[?1049h"))
	f.Add([]byte("\x1b"))
	f.Add([]byte{0x1b, 0x5b})
	f.Add([]byte{0x00, 0x01, 0x02, 0xff})

	f.Fuzz(func(t *testing.T, data []byte) {
		s := NewANSIStripper()
		// Must not panic
		s.Feed(data)
	})
}
