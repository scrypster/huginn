package claudecode

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// backfillConcurrency bounds parallel transcript imports so a large history
// does not saturate the SQLite write connection.
const backfillConcurrency = 4

// BackfillResult summarises a historical import.
type BackfillResult struct {
	Files    int // transcripts read
	Messages int // messages appended
	Skipped  int // transcripts skipped for exceeding maxFileMB
	Failed   int // transcripts that could not be read at all
}

// Backfill imports every Claude Code transcript under root that has not
// already been fully consumed. It is idempotent: transcripts already at their
// full offset append nothing.
//
// A missing root is not an error — Claude Code may never have run here.
// Transcripts larger than maxFileMB are skipped and logged by name and size;
// truncation is never silent.
func Backfill(ctx context.Context, root string, ing *Ingester, maxFileMB int) (BackfillResult, error) {
	var res BackfillResult
	if root == "" {
		return res, nil
	}

	paths, err := transcriptPaths(root)
	if err != nil {
		return res, err
	}

	maxBytes := int64(maxFileMB) * 1024 * 1024

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, backfillConcurrency)
	)

	var cancelled bool
	for _, p := range paths {
		select {
		case <-ctx.Done():
			cancelled = true
		default:
		}
		if cancelled {
			break
		}

		fi, statErr := os.Stat(p)
		if statErr != nil {
			slog.Warn("claudecode: cannot stat transcript, skipping", "path", p, "err", statErr)
			mu.Lock()
			res.Failed++
			mu.Unlock()
			continue
		}
		if fi.Size() > maxBytes {
			slog.Warn("claudecode: skipping oversized transcript",
				"path", p, "size_bytes", fi.Size(), "limit_bytes", maxBytes)
			mu.Lock()
			res.Skipped++
			mu.Unlock()
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(path string) {
			defer wg.Done()
			defer func() { <-sem }()

			n, ingErr := ing.IngestFile(path)
			mu.Lock()
			defer mu.Unlock()
			if ingErr != nil {
				slog.Warn("claudecode: backfill ingest failed", "path", path, "err", ingErr)
				res.Failed++
				return
			}
			res.Files++
			res.Messages += n
		}(p)
	}

	wg.Wait()
	if cancelled {
		return res, ctx.Err()
	}
	return res, nil
}

// transcriptPaths lists every .jsonl file anywhere under root, walked
// recursively, EXCEPT those under a "subagents" directory.
//
// VERIFIED FACT: Claude Code writes sub-agent turns to
// <project>/<session-uuid>/subagents/agent-<hex>.jsonl — a nested directory,
// not isSidechain lines inside the parent transcript. On a real machine this
// was 936 subagent transcripts (458 MB) against 27 main transcripts.
//
// RULING: these are deliberately skipped, not ingested. sessionIDFromPath
// keys a transcript by its basename, so ingesting them produces ~936 junk
// top-level Huginn sessions named "agent-<hex>", fragmenting real threads.
// The watcher's addTree is also depth-1 only and structurally cannot tail
// them live, so backfill would be importing what live sync can never follow
// — worse than not importing them at all. Correctly attributing sub-agent
// turns to their parent session is a design change beyond this branch.
//
// A missing root yields an empty list, not an error.
func transcriptPaths(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			// An unreadable subdirectory should not abort the whole walk.
			slog.Debug("claudecode: skipping unreadable path", "path", path, "err", err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if d.Name() == "subagents" {
				// Do not even descend — cheaper than filtering afterwards.
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".jsonl") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return out, err
	}
	return out, nil
}
