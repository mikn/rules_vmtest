package vm

import (
	"bytes"
	"testing"
)

func TestProcessEscapeSequences_PlainText(t *testing.T) {
	input := []byte("Hello, World!\n")
	output, incomplete := ProcessEscapeSequences(input)

	if !bytes.Equal(output, input) {
		t.Errorf("plain text should pass through unchanged, got %q", output)
	}
	if incomplete != nil {
		t.Errorf("no incomplete data expected, got %q", incomplete)
	}
}

func TestProcessEscapeSequences_StripsClearScreen(t *testing.T) {
	// ESC[2J = clear screen
	input := []byte("before\x1b[2Jafter")
	output, incomplete := ProcessEscapeSequences(input)

	if !bytes.Equal(output, []byte("beforeafter")) {
		t.Errorf("expected clear screen stripped, got %q", output)
	}
	if incomplete != nil {
		t.Errorf("no incomplete expected, got %q", incomplete)
	}
}

func TestProcessEscapeSequences_StripsCursorHome(t *testing.T) {
	// ESC[H = cursor home
	input := []byte("text\x1b[Hmore")
	output, _ := ProcessEscapeSequences(input)

	if !bytes.Equal(output, []byte("textmore")) {
		t.Errorf("expected cursor home stripped, got %q", output)
	}
}

func TestProcessEscapeSequences_PreservesColorCodes(t *testing.T) {
	// ESC[31m = red foreground (should be preserved)
	input := []byte("\x1b[31mred text\x1b[0m")
	output, _ := ProcessEscapeSequences(input)

	if !bytes.Equal(output, input) {
		t.Errorf("color codes should be preserved, got %q", output)
	}
}

func TestProcessEscapeSequences_StripsSingleChar(t *testing.T) {
	// ESC c = reset terminal, ESC 7 = save cursor, ESC 8 = restore cursor
	input := []byte("a\x1bcb\x1b7c\x1b8d")
	output, _ := ProcessEscapeSequences(input)

	if !bytes.Equal(output, []byte("abcd")) {
		t.Errorf("single-char escapes should be stripped, got %q", output)
	}
}

func TestProcessEscapeSequences_IncompleteSequence(t *testing.T) {
	// Incomplete CSI sequence at end of buffer
	input := []byte("text\x1b[2")
	output, incomplete := ProcessEscapeSequences(input)

	if !bytes.Equal(output, []byte("text")) {
		t.Errorf("output should have text before incomplete, got %q", output)
	}
	if !bytes.Equal(incomplete, []byte("\x1b[2")) {
		t.Errorf("incomplete should contain the partial sequence, got %q", incomplete)
	}
}

func TestProcessEscapeSequences_IncompleteEscOnly(t *testing.T) {
	input := []byte("text\x1b")
	output, incomplete := ProcessEscapeSequences(input)

	if !bytes.Equal(output, []byte("text")) {
		t.Errorf("expected text, got %q", output)
	}
	if !bytes.Equal(incomplete, []byte("\x1b")) {
		t.Errorf("expected ESC as incomplete, got %q", incomplete)
	}
}

func TestProcessEscapeSequences_MultipleSequences(t *testing.T) {
	// Mix of stripped and preserved sequences
	input := []byte("\x1b[2Jhello\x1b[31m world\x1b[H\x1b[0m")
	output, _ := ProcessEscapeSequences(input)

	expected := []byte("hello\x1b[31m world\x1b[0m")
	if !bytes.Equal(output, expected) {
		t.Errorf("expected %q, got %q", expected, output)
	}
}
