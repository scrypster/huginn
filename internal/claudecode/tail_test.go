package claudecode

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("append: %v", err)
	}
}

func TestReadNewSkipsPartialTrailingLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, "{\"a\":1}\n{\"b\":2}\n{\"c\":3")

	lines, st, err := ReadNew(p, TailState{Path: p})
	if err != nil {
		t.Fatalf("ReadNew: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (the third is incomplete)", len(lines))
	}
	if st.ByteOffset != 16 {
		t.Errorf("ByteOffset = %d, want 16 (two complete lines)", st.ByteOffset)
	}

	// Completing the line makes it available without re-emitting the first two.
	appendFile(t, p, "}\n")
	lines2, _, err := ReadNew(p, st)
	if err != nil {
		t.Fatalf("ReadNew(2): %v", err)
	}
	if len(lines2) != 1 || string(lines2[0]) != `{"c":3}` {
		t.Errorf("second read = %q, want [{\"c\":3}]", lines2)
	}
}

func TestReadNewByteAtATimeMatchesSingleShot(t *testing.T) {
	content := "{\"uuid\":\"1\"}\n{\"uuid\":\"2\"}\n{\"uuid\":\"3\"}\n{\"uuid\":\"4\"}\n"

	dir := t.TempDir()
	whole := filepath.Join(dir, "whole.jsonl")
	writeFile(t, whole, content)
	want, _, err := ReadNew(whole, TailState{Path: whole})
	if err != nil {
		t.Fatalf("single shot: %v", err)
	}

	drip := filepath.Join(dir, "drip.jsonl")
	writeFile(t, drip, "")
	st := TailState{Path: drip}
	var got [][]byte
	for i := 0; i < len(content); i++ {
		appendFile(t, drip, string(content[i]))
		lines, next, err := ReadNew(drip, st)
		if err != nil {
			t.Fatalf("drip read at %d: %v", i, err)
		}
		got = append(got, lines...)
		st = next
	}

	if len(got) != len(want) {
		t.Fatalf("drip produced %d lines, single shot produced %d", len(got), len(want))
	}
	for i := range want {
		if string(got[i]) != string(want[i]) {
			t.Errorf("line %d: drip %q != single %q", i, got[i], want[i])
		}
	}
}

func TestReadNewContinuesWhenFileGrew(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, "{\"uuid\":\"1\"}\n{\"uuid\":\"2\"}\n")

	_, st, err := ReadNew(p, TailState{Path: p})
	if err != nil {
		t.Fatalf("ReadNew: %v", err)
	}
	st.LastUUID = "2"

	// Same session, file rewritten from scratch with more content appended.
	writeFile(t, p, "{\"uuid\":\"1\"}\n{\"uuid\":\"2\"}\n{\"uuid\":\"3\"}\n")
	lines, _, err := ReadNew(p, st)
	if err != nil {
		t.Fatalf("ReadNew after truncation: %v", err)
	}
	if len(lines) != 1 || string(lines[0]) != `{"uuid":"3"}` {
		t.Fatalf("after truncation got %q, want only uuid 3", lines)
	}
}

func TestReadNewFullResetWhenLastUUIDAbsent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, "{\"uuid\":\"9\"}\n")

	// Offset is past the new (shorter) file and LastUUID is not in it:
	// this is different content at the same path.
	st := TailState{Path: p, Size: 999, ByteOffset: 500, LastUUID: "gone"}
	lines, next, err := ReadNew(p, st)
	if err != nil {
		t.Fatalf("ReadNew: %v", err)
	}
	if len(lines) != 1 || string(lines[0]) != `{"uuid":"9"}` {
		t.Fatalf("got %q, want the whole file replayed", lines)
	}
	if next.LastUUID != "9" {
		t.Errorf("LastUUID = %q, want 9", next.LastUUID)
	}
}

func TestReadNewResyncsAfterShrinkWhenLastUUIDFound(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")

	// Three lines: 16 + 16 + 13 = 45 bytes.
	writeFile(t, p, "{\"uuid\":\"aaaa\"}\n{\"uuid\":\"bbbb\"}\n{\"uuid\":\"c\"}\n")

	_, st, err := ReadNew(p, TailState{Path: p})
	if err != nil {
		t.Fatalf("ReadNew: %v", err)
	}
	if st.ByteOffset != 45 {
		t.Fatalf("ByteOffset = %d, want 45", st.ByteOffset)
	}
	if st.LastUUID != "c" {
		t.Fatalf("LastUUID = %q, want c", st.LastUUID)
	}

	// Rewrite SHORTER than the stored offset (26 < 45), with LastUUID still
	// present but no longer last. This is the rule-2 path: reset to zero,
	// replay, discard through LastUUID, resume after it.
	writeFile(t, p, "{\"uuid\":\"c\"}\n{\"uuid\":\"d\"}\n")

	lines, next, err := ReadNew(p, st)
	if err != nil {
		t.Fatalf("ReadNew after shrink: %v", err)
	}
	if len(lines) != 1 || string(lines[0]) != `{"uuid":"d"}` {
		t.Fatalf("got %q, want only uuid d — the replay must discard through LastUUID", lines)
	}
	if next.ByteOffset != 26 {
		t.Errorf("ByteOffset = %d, want 26 (offset must reset, not continue from 45)", next.ByteOffset)
	}
	if next.LastUUID != "d" {
		t.Errorf("LastUUID = %q, want d", next.LastUUID)
	}
}

func TestReadNewNoChangeReturnsNothing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, "{\"uuid\":\"1\"}\n")

	_, st, err := ReadNew(p, TailState{Path: p})
	if err != nil {
		t.Fatalf("ReadNew: %v", err)
	}
	lines, _, err := ReadNew(p, st)
	if err != nil {
		t.Fatalf("ReadNew(2): %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("got %d lines on an unchanged file, want 0", len(lines))
	}
}
