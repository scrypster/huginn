package repo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/scrypster/huginn/internal/storage"
)

// skipDirNames is the set of directory names that should never be indexed.
// Checked by name only (not full path) so it applies at any depth.
var skipDirNames = map[string]bool{
	// VCS / deps
	".git": true, "node_modules": true, "vendor": true,
	// macOS system dirs that appear under $HOME
	"Library": true, "Applications": true, ".Trash": true,
	// Common cache / build output dirs
	".cache": true, "cache": true, "Cache": true,
	".next": true, "dist": true, "build": true, "out": true,
	"target": true, ".gradle": true, ".m2": true,
	// Python / Ruby
	"__pycache__": true, ".venv": true, "venv": true, ".bundle": true,
	// Misc
	"tmp": true, "temp": true, "Temp": true, ".DS_Store": true,
	"coverage": true, ".nyc_output": true,
}

// shouldSkipDir reports whether a directory entry should be skipped during
// plain-directory indexing. Hidden directories (names starting with ".") are
// always skipped except for common project-level dotfiles.
func shouldSkipDir(name string) bool {
	if skipDirNames[name] {
		return true
	}
	// Skip hidden dirs generically — but allow .github, .huginn, .vscode etc.
	// We only skip hidden dirs that look like OS/tool caches, not project config.
	if len(name) > 1 && name[0] == '.' {
		// Allow common project-level hidden directories.
		allowed := map[string]bool{
			".github": true, ".huginn": true, ".vscode": true,
			".idea": true, ".circleci": true, ".husky": true,
		}
		return !allowed[name]
	}
	return false
}

// FileChunk is a slice of a file's content for context building.
type FileChunk struct {
	Path      string
	Content   string
	StartLine int
}

// Index holds all chunks from the current repo.
type Index struct {
	Chunks []FileChunk
	Root   string
}

// IncrementalResult is returned by BuildIncrementalWithStats and includes
// scan/skip counts for observability.
type IncrementalResult struct {
	Index        *Index
	FilesScanned int
	FilesSkipped int
}

var binaryExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".bmp": true,
	".ico": true, ".webp": true,
	".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true,
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".a": true,
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	".mp3": true, ".mp4": true, ".wav": true, ".avi": true, ".mov": true,
	".wasm": true, ".bin": true, ".dat": true, ".db": true, ".sqlite": true,
	".lock": true, ".pb": true, ".parquet": true, ".whl": true, ".class": true,
}

func isBinaryByExtension(name string) bool {
	return binaryExtensions[strings.ToLower(filepath.Ext(name))]
}

func isBinaryContent(data []byte) bool {
	sniff := data
	if len(sniff) > 512 {
		sniff = sniff[:512]
	}
	return bytes.IndexByte(sniff, 0) >= 0
}

const chunkSize = 200 * 1024
const maxFileBytes = 10 * 1024 * 1024 // 10 MB max single file read

// chunkContent splits file content into chunks <= maxBytes, splitting on blank lines.
func chunkContent(path string, data []byte, maxBytes int) []FileChunk {
	if len(data) <= maxBytes {
		return []FileChunk{{Path: path, Content: string(data), StartLine: 1}}
	}

	var chunks []FileChunk
	lines := strings.Split(string(data), "\n")
	var buf strings.Builder
	startLine := 1
	currentLine := 1

	for _, line := range lines {
		buf.WriteString(line)
		buf.WriteByte('\n')
		currentLine++

		if buf.Len() >= maxBytes {
			chunks = append(chunks, FileChunk{
				Path:      path,
				Content:   buf.String(),
				StartLine: startLine,
			})
			buf.Reset()
			startLine = currentLine
		}
	}
	if buf.Len() > 0 {
		chunks = append(chunks, FileChunk{
			Path:      path,
			Content:   buf.String(),
			StartLine: startLine,
		})
	}
	return chunks
}

// ProgressFunc is called during indexing with (done, total, currentPath).
// total may be 0 if not yet known (directory walk mode).
type ProgressFunc func(done, total int, path string)

// Build indexes the git repo rooted at dir and returns an Index.
// onProgress is called for each file processed; pass nil to skip.
func Build(dir string, onProgress ProgressFunc) (*Index, error) {
	repo, err := git.PlainOpenWithOptions(dir, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return buildFromDir(dir, onProgress)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return buildFromDir(dir, onProgress)
	}

	root := wt.Filesystem.Root()
	idx := &Index{Root: root}

	files, err := listGitFiles(repo)
	if err != nil || len(files) == 0 {
		return buildFromDir(dir, onProgress)
	}

	total := len(files)
	done := 0
	for _, relPath := range files {
		done++
		if onProgress != nil {
			onProgress(done, total, relPath)
		}
		if isBinaryByExtension(relPath) {
			continue
		}
		absPath := filepath.Join(root, relPath)
		info, statErr := os.Stat(absPath)
		if statErr != nil || info.Size() > maxFileBytes {
			continue
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		if isBinaryContent(data) {
			continue
		}
		chunks := chunkContent(relPath, data, chunkSize)
		idx.Chunks = append(idx.Chunks, chunks...)
	}

	return idx, nil
}

// listGitFiles returns all tracked file paths relative to the repo root.
func listGitFiles(r *git.Repository) ([]string, error) {
	ref, err := r.Head()
	if err != nil {
		return nil, nil
	}
	commit, err := r.CommitObject(ref.Hash())
	if err != nil {
		return nil, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}

	var paths []string
	tree.Files().ForEach(func(f *object.File) error {
		paths = append(paths, f.Name)
		return nil
	})
	return paths, nil
}

// buildFromDir indexes all text files in a plain directory (no git).
func buildFromDir(dir string, onProgress ProgressFunc) (*Index, error) {
	idx := &Index{Root: dir}
	done := 0
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		done++
		rel, _ := filepath.Rel(dir, path)
		if onProgress != nil {
			onProgress(done, 0, rel) // total unknown in walk mode
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxFileBytes {
			return nil
		}
		if isBinaryByExtension(path) {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil || isBinaryContent(data) {
			return nil
		}
		chunks := chunkContent(rel, data, chunkSize)
		idx.Chunks = append(idx.Chunks, chunks...)
		return nil
	})
	return idx, err
}

// BuildIncremental indexes the repo, skipping files whose SHA-256 hash
// matches what's already stored in the store. This enables fast re-runs.
// Returns the Index (for in-memory use) and updates the store.
func BuildIncremental(dir string, store *storage.Store, onProgress ProgressFunc) (*Index, error) {
	repo, err := git.PlainOpenWithOptions(dir, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return buildFromDirIncremental(dir, store, onProgress)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return buildFromDirIncremental(dir, store, onProgress)
	}

	root := wt.Filesystem.Root()
	idx := &Index{Root: root}

	files, err := listGitFiles(repo)
	if err != nil || len(files) == 0 {
		return buildFromDirIncremental(dir, store, onProgress)
	}

	total := len(files)
	done := 0
	for _, relPath := range files {
		done++
		if onProgress != nil {
			onProgress(done, total, relPath)
		}
		if isBinaryByExtension(relPath) {
			continue
		}
		absPath := filepath.Join(root, relPath)
		info, statErr := os.Stat(absPath)
		if statErr != nil || info.Size() > maxFileBytes {
			continue
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		if isBinaryContent(data) {
			continue
		}

		// Compute SHA-256 and check if we can skip
		hash := sha256hex(data)
		if store != nil {
			stored := store.GetFileRecord(relPath)
			if stored.Hash == hash {
				// File unchanged — load chunks from store for in-memory index
				chunks := store.GetChunks(relPath)
				for _, sc := range chunks {
					idx.Chunks = append(idx.Chunks, FileChunk{
						Path:      sc.Path,
						Content:   sc.Content,
						StartLine: sc.StartLine,
					})
				}
				continue
			}
		}

		// File changed — re-chunk and store
		chunks := chunkContent(relPath, data, chunkSize)
		idx.Chunks = append(idx.Chunks, chunks...)

		// Persist to store
		if store != nil {
			storeChunks := make([]storage.FileChunk, len(chunks))
			for i, c := range chunks {
				storeChunks[i] = storage.FileChunk{
					Path:      c.Path,
					Content:   c.Content,
					StartLine: c.StartLine,
				}
			}
			if err := store.SetChunks(relPath, storeChunks); err != nil {
				return nil, fmt.Errorf("store chunks for %s: %w", relPath, err)
			}
			if err := store.SetFileRecord(storage.FileRecord{
				Path:          relPath,
				Hash:          hash,
				ParserVersion: 1,
				IndexedAt:     time.Now(),
			}); err != nil {
				return nil, fmt.Errorf("store file record for %s: %w", relPath, err)
			}
		}
	}

	return idx, nil
}

// buildFromDirIncremental is the directory-walk version of BuildIncremental.
func buildFromDirIncremental(dir string, store *storage.Store, onProgress ProgressFunc) (*Index, error) {
	idx := &Index{Root: dir}
	done := 0
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		done++
		rel, _ := filepath.Rel(dir, path)
		if onProgress != nil {
			onProgress(done, 0, rel)
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxFileBytes {
			return nil
		}
		if isBinaryByExtension(path) {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil || isBinaryContent(data) {
			return nil
		}

		hash := sha256hex(data)
		if store != nil {
			stored := store.GetFileRecord(rel)
			if stored.Hash == hash {
				chunks := store.GetChunks(rel)
				for _, sc := range chunks {
					idx.Chunks = append(idx.Chunks, FileChunk{
						Path:      sc.Path,
						Content:   sc.Content,
						StartLine: sc.StartLine,
					})
				}
				return nil
			}
		}

		chunks := chunkContent(rel, data, chunkSize)
		idx.Chunks = append(idx.Chunks, chunks...)

		if store != nil {
			storeChunks := make([]storage.FileChunk, len(chunks))
			for i, c := range chunks {
				storeChunks[i] = storage.FileChunk{
					Path:      c.Path,
					Content:   c.Content,
					StartLine: c.StartLine,
				}
			}
			if err := store.SetChunks(rel, storeChunks); err != nil {
				return fmt.Errorf("store chunks for %s: %w", rel, err)
			}
			if err := store.SetFileRecord(storage.FileRecord{
				Path:          rel,
				Hash:          hash,
				ParserVersion: 1,
				IndexedAt:     time.Now(),
			}); err != nil {
				return fmt.Errorf("store file record for %s: %w", rel, err)
			}
		}
		return nil
	})
	return idx, err
}

// BuildIncrementalWithStats is like BuildIncremental but returns scan/skip counts.
func BuildIncrementalWithStats(dir string, store *storage.Store, onProgress ProgressFunc) (*IncrementalResult, error) {
	result := &IncrementalResult{}

	repo, err := git.PlainOpenWithOptions(dir, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return buildFromDirIncrementalWithStats(dir, store, onProgress)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return buildFromDirIncrementalWithStats(dir, store, onProgress)
	}

	root := wt.Filesystem.Root()
	idx := &Index{Root: root}

	files, err := listGitFiles(repo)
	if err != nil || len(files) == 0 {
		return buildFromDirIncrementalWithStats(dir, store, onProgress)
	}

	total := len(files)
	done := 0
	for _, relPath := range files {
		done++
		if onProgress != nil {
			onProgress(done, total, relPath)
		}
		if isBinaryByExtension(relPath) {
			continue
		}
		absPath := filepath.Join(root, relPath)
		info, statErr := os.Stat(absPath)
		if statErr != nil || info.Size() > maxFileBytes {
			continue
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		if isBinaryContent(data) {
			continue
		}
		result.FilesScanned++

		hash := sha256hex(data)
		if store != nil {
			stored := store.GetFileRecord(relPath)
			if stored.Hash == hash {
				result.FilesSkipped++
				chunks := store.GetChunks(relPath)
				for _, sc := range chunks {
					idx.Chunks = append(idx.Chunks, FileChunk{
						Path:      sc.Path,
						Content:   sc.Content,
						StartLine: sc.StartLine,
					})
				}
				continue
			}
		}

		chunks := chunkContent(relPath, data, chunkSize)
		idx.Chunks = append(idx.Chunks, chunks...)

		if store != nil {
			storeChunks := make([]storage.FileChunk, len(chunks))
			for i, c := range chunks {
				storeChunks[i] = storage.FileChunk{
					Path:      c.Path,
					Content:   c.Content,
					StartLine: c.StartLine,
				}
			}
			if err := store.SetChunks(relPath, storeChunks); err != nil {
				return nil, fmt.Errorf("store chunks for %s: %w", relPath, err)
			}
			if err := store.SetFileRecord(storage.FileRecord{
				Path:          relPath,
				Hash:          hash,
				ParserVersion: 1,
				IndexedAt:     time.Now(),
			}); err != nil {
				return nil, fmt.Errorf("store file record for %s: %w", relPath, err)
			}
		}
	}

	result.Index = idx
	return result, nil
}

// buildFromDirIncrementalWithStats is the directory-walk version of BuildIncrementalWithStats.
func buildFromDirIncrementalWithStats(dir string, store *storage.Store, onProgress ProgressFunc) (*IncrementalResult, error) {
	result := &IncrementalResult{}
	idx := &Index{Root: dir}
	done := 0
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		done++
		rel, _ := filepath.Rel(dir, path)
		if onProgress != nil {
			onProgress(done, 0, rel)
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxFileBytes {
			return nil
		}
		if isBinaryByExtension(path) {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil || isBinaryContent(data) {
			return nil
		}
		result.FilesScanned++

		hash := sha256hex(data)
		if store != nil {
			stored := store.GetFileRecord(rel)
			if stored.Hash == hash {
				result.FilesSkipped++
				chunks := store.GetChunks(rel)
				for _, sc := range chunks {
					idx.Chunks = append(idx.Chunks, FileChunk{
						Path:      sc.Path,
						Content:   sc.Content,
						StartLine: sc.StartLine,
					})
				}
				return nil
			}
		}

		chunks := chunkContent(rel, data, chunkSize)
		idx.Chunks = append(idx.Chunks, chunks...)

		if store != nil {
			storeChunks := make([]storage.FileChunk, len(chunks))
			for i, c := range chunks {
				storeChunks[i] = storage.FileChunk{
					Path:      c.Path,
					Content:   c.Content,
					StartLine: c.StartLine,
				}
			}
			if err := store.SetChunks(rel, storeChunks); err != nil {
				return fmt.Errorf("store chunks for %s: %w", rel, err)
			}
			if err := store.SetFileRecord(storage.FileRecord{
				Path:          rel,
				Hash:          hash,
				ParserVersion: 1,
				IndexedAt:     time.Now(),
			}); err != nil {
				return fmt.Errorf("store file record for %s: %w", rel, err)
			}
		}
		return nil
	})
	result.Index = idx
	return result, err
}

// sha256hex returns the hex-encoded SHA-256 of data.
func sha256hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
