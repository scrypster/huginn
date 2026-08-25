package claudecode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// TailState is the resume point for one transcript file. It is persisted per
// Claude Code session so ingestion survives restarts.
type TailState struct {
	Path       string
	Size       int64
	ByteOffset int64
	// LastUUID is the transcript uuid of the most recently consumed line. It
	// is the re-sync anchor after a truncation: uuids are unordered, so this
	// is used for positional matching, never comparison.
	LastUUID string
}

// ReadNew returns the complete lines appended to path since st, along with
// the state to pass on the next call.
//
// Three rules make this safe against a file Claude Code is actively writing:
//
//  1. A trailing fragment with no newline is never consumed, and ByteOffset
//     never advances past the last '\n'. Consuming it would corrupt the
//     stream; advancing past it would drop a message.
//  2. If the file shrank below ByteOffset it was truncated or replaced.
//     Replay from zero and discard lines up to and including LastUUID.
//  3. If LastUUID is not found during that replay, the path holds different
//     content entirely. Emit the whole file.
func ReadNew(path string, st TailState) ([][]byte, TailState, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, st, fmt.Errorf("claudecode: stat %s: %w", path, err)
	}
	size := fi.Size()

	next := st
	next.Path = path
	next.Size = size

	replaying := false
	start := st.ByteOffset
	if size < st.ByteOffset {
		// Rule 2: truncated or replaced.
		replaying = true
		start = 0
	}
	if size == st.ByteOffset {
		return nil, next, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, st, fmt.Errorf("claudecode: open %s: %w", path, err)
	}
	defer f.Close()

	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, st, fmt.Errorf("claudecode: seek %s: %w", path, err)
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		return nil, st, fmt.Errorf("claudecode: read %s: %w", path, err)
	}

	// Rule 1: keep only whole lines.
	nl := bytes.LastIndexByte(buf, '\n')
	if nl < 0 {
		return nil, next, nil
	}
	whole := buf[:nl+1]
	next.ByteOffset = start + int64(len(whole))

	raw := bytes.Split(bytes.TrimSuffix(whole, []byte("\n")), []byte("\n"))

	var out [][]byte
	if replaying && st.LastUUID != "" {
		out = afterUUID(raw, st.LastUUID)
	} else {
		out = raw
	}

	for _, l := range out {
		if u := uuidOf(l); u != "" {
			next.LastUUID = u
		}
	}
	return out, next, nil
}

// afterUUID returns the lines following the one whose uuid is target. If the
// target is not present the whole slice is returned (rule 3).
func afterUUID(lines [][]byte, target string) [][]byte {
	for i, l := range lines {
		if uuidOf(l) == target {
			return lines[i+1:]
		}
	}
	return lines
}

func uuidOf(line []byte) string {
	var probe struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		return ""
	}
	return probe.UUID
}
