package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// TestAppend_ConcurrentRace_NoDataRace verifies that concurrent Append calls on
// the same session do not race on Manifest fields or interleave JSONL lines.
//
// Bug: Append() does atomic.AddInt64 for the sequence number but then mutates
// sess.Manifest.MessageCount and sess.Manifest.LastMessageID without any lock.
// Concurrent callers race on these two plain struct fields.
//
// Fix: add a sync.Mutex to Session and hold it while updating Manifest fields.
func TestAppend_ConcurrentRace_NoDataRace(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	sess := store.New("race test", "/ws", "model")

	const goroutines = 10
	const msgsPerGoroutine = 20

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < msgsPerGoroutine; j++ {
				msg := SessionMessage{
					Role:    "user",
					Content: fmt.Sprintf("goroutine %d message %d", id, j),
				}
				if err := store.Append(sess, msg); err != nil {
					t.Errorf("Append: %v", err)
				}
			}
		}(i)
	}
	wg.Wait()

	// Verify every line in the JSONL file is valid JSON.
	path := filepath.Join(dir, sess.ID, "messages.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open jsonl: %v", err)
	}
	defer f.Close()

	lineNum := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg SessionMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Errorf("line %d is not valid JSON: %v\nLine: %s", lineNum+1, err, line)
		}
		lineNum++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}

	want := goroutines * msgsPerGoroutine
	if lineNum != want {
		t.Errorf("expected %d lines, got %d", want, lineNum)
	}

	// MessageCount must equal want (races can corrupt it without the fix).
	if sess.Manifest.MessageCount != want {
		t.Errorf("expected MessageCount %d, got %d", want, sess.Manifest.MessageCount)
	}
}

// TestStore_TailMessages_ConcurrentAppend_NoTear verifies that concurrent
// Append and TailMessages calls on the same session never produce torn reads.
//
// 20 goroutines append distinct messages while 5 goroutines repeatedly call
// TailMessages(100). The race detector must report no data races, and every
// TailMessages result must be a valid (non-torn) set of JSON messages.
func TestStore_TailMessages_ConcurrentAppend_NoTear(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	sess := store.New("concurrent-tail-test", "/ws", "model")

	const appendGoroutines = 20
	const msgsPerAppender = 10
	const tailGoroutines = 5

	// appendedCount tracks how many Append calls have completed so far.
	var appendedCount atomic.Int64

	var wg sync.WaitGroup

	// Launch appenders.
	for i := 0; i < appendGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < msgsPerAppender; j++ {
				msg := SessionMessage{
					Role:    "user",
					Content: fmt.Sprintf("appender-%d-msg-%d", goroutineID, j),
				}
				if err := store.Append(sess, msg); err != nil {
					t.Errorf("Append (goroutine %d): %v", goroutineID, err)
					return
				}
				appendedCount.Add(1)
			}
		}(i)
	}

	// Launch readers that run concurrently with the appenders.
	done := make(chan struct{})
	for i := 0; i < tailGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				msgs, err := store.TailMessages(sess.ID, 100)
				if err != nil {
					t.Errorf("TailMessages (reader %d): %v", goroutineID, err)
					return
				}
				// Every returned message must be valid (non-zero ID assigned by Append).
				for idx, m := range msgs {
					if m.ID == "" {
						t.Errorf("reader %d: msg[%d] has empty ID — torn read detected", goroutineID, idx)
					}
				}
			}
		}(i)
	}

	// Wait for all appenders to finish, then signal readers to stop.
	// (Appenders are tracked in wg above; we rely on wg and done instead.)
	// The appender goroutines are already tracked in wg. Wait for all goroutines.
	// Signal readers only after wg minus readers is done — simpler: just close done
	// after a small delay to let appenders finish.
	go func() {
		// Wait until all messages are appended.
		total := int64(appendGoroutines * msgsPerAppender)
		for appendedCount.Load() < total {
			// spin — this loop exits very quickly in practice
		}
		close(done)
	}()

	wg.Wait()

	// Final assertion: TailMessages returns exactly the full set of messages.
	finalMsgs, err := store.TailMessages(sess.ID, appendGoroutines*msgsPerAppender+1)
	if err != nil {
		t.Fatalf("final TailMessages: %v", err)
	}
	want := appendGoroutines * msgsPerAppender
	if len(finalMsgs) != want {
		t.Errorf("expected %d messages in total, got %d", want, len(finalMsgs))
	}

	// Verify the JSONL file has no torn lines.
	path := filepath.Join(dir, sess.ID, "messages.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open jsonl: %v", err)
	}
	defer f.Close()

	lineNum := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg SessionMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Errorf("line %d is not valid JSON (torn write): %v\nLine: %s", lineNum+1, err, line)
		}
		lineNum++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	if lineNum != want {
		t.Errorf("JSONL line count: got %d, want %d", lineNum, want)
	}
}
