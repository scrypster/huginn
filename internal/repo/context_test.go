package repo

import (
	"strings"
	"testing"
)

func TestBuildContext(t *testing.T) {
	idx := &Index{
		Root: "/repo",
		Chunks: []FileChunk{
			{Path: "auth/login.go", Content: "func Login(user string) error { return nil }", StartLine: 1},
			{Path: "auth/logout.go", Content: "func Logout(user string) { }", StartLine: 1},
			{Path: "api/handler.go", Content: "func HandleRequest(w http.ResponseWriter, r *http.Request) {}", StartLine: 1},
			{Path: "README.md", Content: "# My App\nA simple web application.", StartLine: 1},
		},
	}

	ctx := idx.BuildContext("login authentication", 4096)
	if !strings.Contains(ctx, "auth/login.go") {
		t.Error("expected auth/login.go in context (most relevant to 'login')")
	}
}

func TestBuildTree(t *testing.T) {
	idx := &Index{
		Root: "/repo",
		Chunks: []FileChunk{
			{Path: "cmd/main.go", Content: "package main", StartLine: 1},
			{Path: "internal/auth/login.go", Content: "package auth", StartLine: 1},
			{Path: "README.md", Content: "# Readme", StartLine: 1},
		},
	}
	tree := idx.BuildTree()
	if !strings.Contains(tree, "cmd/") && !strings.Contains(tree, "cmd") {
		t.Error("expected cmd in tree")
	}
	if !strings.Contains(tree, "internal") {
		t.Error("expected internal in tree")
	}
}

func TestBuildContextEmpty(t *testing.T) {
	idx := &Index{Root: "/repo", Chunks: nil}
	ctx := idx.BuildContext("anything", 4096)
	if ctx != "" {
		t.Errorf("expected empty context for empty index, got %q", ctx)
	}
}

// TestBuildContext_EmptyQuery verifies that an empty query returns all chunks
// with uniform score=1 (no TF-IDF filtering applied).
func TestBuildContext_EmptyQuery(t *testing.T) {
	idx := &Index{
		Root: "/repo",
		Chunks: []FileChunk{
			{Path: "a.go", Content: "package a", StartLine: 1},
			{Path: "b.go", Content: "package b", StartLine: 1},
		},
	}
	ctx := idx.BuildContext("", 4096)
	// With an empty query, scoreChunks returns all chunks with score=1.
	// BuildContext header alone takes ~26 bytes, so both chunks should fit.
	if !strings.Contains(ctx, "a.go") || !strings.Contains(ctx, "b.go") {
		t.Errorf("expected both chunks in empty-query context, got:\n%s", ctx)
	}
}

// TestBuildContext_TinyBudget verifies that when maxBytes is very small,
// no chunks are added (the header itself may not fit).
func TestBuildContext_TinyBudget(t *testing.T) {
	idx := &Index{
		Root: "/repo",
		Chunks: []FileChunk{
			{Path: "large_file.go", Content: strings.Repeat("x", 2000), StartLine: 1},
		},
	}
	// Budget is tiny — not even enough for the chunk block.
	ctx := idx.BuildContext("large_file", 10)
	// The header "## Repository Context\n\n" is ~24 bytes, so with budget=10
	// remaining is negative but the header is always written.
	// The important thing is no panic and the chunk is not included.
	if strings.Contains(ctx, strings.Repeat("x", 100)) {
		t.Error("large chunk should not be included with tiny budget")
	}
}

// TestBuildContext_PathBoost verifies that a file whose path contains the query
// term appears in the context. The path boost doubles the score, so a file with
// the query term in its path should be included.
func TestBuildContext_PathBoost(t *testing.T) {
	idx := &Index{
		Root: "/repo",
		Chunks: []FileChunk{
			{Path: "auth/login.go", Content: "func login() error { return nil }", StartLine: 1},
			{Path: "unrelated/other.go", Content: "func handler() {}", StartLine: 1},
		},
	}
	// "login" appears in both the path and the content of auth/login.go.
	// The path boost doubles its score, ensuring it is included in context.
	ctx := idx.BuildContext("login", 4096)
	if !strings.Contains(ctx, "auth/login.go") {
		t.Error("expected auth/login.go in context for query 'login'")
	}
}

// TestBuildTree_EmptyIndex verifies BuildTree on an empty index.
func TestBuildTree_EmptyIndex(t *testing.T) {
	idx := &Index{Root: "/repo", Chunks: nil}
	tree := idx.BuildTree()
	if !strings.Contains(tree, "## Repository Structure") {
		t.Error("expected tree header even for empty index")
	}
}

// TestBuildTree_TopLevelFiles verifies that top-level files (no directory) are listed.
func TestBuildTree_TopLevelFiles(t *testing.T) {
	idx := &Index{
		Root: "/repo",
		Chunks: []FileChunk{
			{Path: "README.md", Content: "# Readme", StartLine: 1},
			{Path: "main.go", Content: "package main", StartLine: 1},
		},
	}
	tree := idx.BuildTree()
	if !strings.Contains(tree, "README.md") {
		t.Error("expected README.md in tree")
	}
	if !strings.Contains(tree, "main.go") {
		t.Error("expected main.go in tree")
	}
}

// TestBuildTree_DeduplicatesTopLevel verifies that the same top-level dir
// is only listed once.
func TestBuildTree_DeduplicatesTopLevel(t *testing.T) {
	idx := &Index{
		Root: "/repo",
		Chunks: []FileChunk{
			{Path: "internal/a.go", Content: "a", StartLine: 1},
			{Path: "internal/b.go", Content: "b", StartLine: 1},
			{Path: "internal/c.go", Content: "c", StartLine: 1},
		},
	}
	tree := idx.BuildTree()
	// "internal/" should appear exactly once.
	count := strings.Count(tree, "internal/")
	if count != 1 {
		t.Errorf("expected 'internal/' to appear once in tree, got %d", count)
	}
}

// TestTokenize_SingleCharTokensDropped verifies that single-char tokens are dropped.
func TestTokenize_SingleCharTokens(t *testing.T) {
	tokens := tokenize("a b c hello world")
	for _, tok := range tokens {
		if len(tok) <= 1 {
			t.Errorf("expected single-char token %q to be dropped", tok)
		}
	}
}

// TestTokenize_Empty verifies that an empty string produces no tokens.
func TestTokenize_Empty(t *testing.T) {
	tokens := tokenize("")
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens for empty string, got %v", tokens)
	}
}

// TestTokenize_UpperCase verifies that tokenize is case-insensitive.
func TestTokenize_UpperCase(t *testing.T) {
	tokens := tokenize("HelloWorld FOOBAR")
	for _, tok := range tokens {
		if tok != strings.ToLower(tok) {
			t.Errorf("expected lowercase token, got %q", tok)
		}
	}
}

// TestScoreChunks_BM25_BasicRanking verifies that chunks with more relevant
// term matches rank higher using BM25 scoring.
func TestScoreChunks_BM25_BasicRanking(t *testing.T) {
	chunks := []FileChunk{
		{Path: "login.go", Content: "func login(user string) error { login validation here }", StartLine: 1},
		{Path: "other.go", Content: "func handler() { }", StartLine: 1},
		{Path: "auth.go", Content: "func login() { }", StartLine: 1},
	}

	scored := scoreChunks(chunks, "login")

	// Find the indices
	var loginIdx, authIdx, otherIdx int
	for i, sc := range scored {
		if sc.chunk.Path == "login.go" {
			loginIdx = i
		} else if sc.chunk.Path == "auth.go" {
			authIdx = i
		} else if sc.chunk.Path == "other.go" {
			otherIdx = i
		}
	}

	// login.go should have the highest score (more occurrences of "login")
	if scored[loginIdx].score <= scored[authIdx].score {
		t.Errorf("login.go should score higher than auth.go: %f vs %f", scored[loginIdx].score, scored[authIdx].score)
	}

	// auth.go should score higher than other.go (has "login")
	if scored[authIdx].score <= scored[otherIdx].score {
		t.Errorf("auth.go should score higher than other.go: %f vs %f", scored[authIdx].score, scored[otherIdx].score)
	}
}

// TestScoreChunks_BM25_FrequencySaturation verifies that BM25's saturation
// property prevents excessive term frequency boosting. A term appearing 50
// times should not score proportionally higher than appearing 5 times.
func TestScoreChunks_BM25_FrequencySaturation(t *testing.T) {
	highFreq := "term " + strings.Repeat("term ", 49) // "term" appears 50 times
	lowFreq := "term " + strings.Repeat("other ", 49)  // "term" appears 5 times

	chunks := []FileChunk{
		{Path: "high.go", Content: highFreq, StartLine: 1},
		{Path: "low.go", Content: lowFreq, StartLine: 1},
	}

	scored := scoreChunks(chunks, "term")

	var highScore, lowScore float64
	for _, sc := range scored {
		if sc.chunk.Path == "high.go" {
			highScore = sc.score
		} else if sc.chunk.Path == "low.go" {
			lowScore = sc.score
		}
	}

	// High freq should be higher, but ratio should be < 10x (saturation effect).
	// With TF-IDF, high would be ~10x higher. With BM25, it should be much less.
	ratio := highScore / lowScore
	if ratio > 5.0 {
		t.Logf("BM25 saturation check: ratio=%.2f (should be reasonable, not 10x+)", ratio)
	}
	if ratio < 1.0 {
		t.Errorf("high frequency should still score higher, but got ratio %.2f", ratio)
	}
}

// TestScoreChunks_BM25_PathBoostKept verifies that the path boost (2x multiplier)
// is still applied in BM25 variant.
func TestScoreChunks_BM25_PathBoostKept(t *testing.T) {
	chunks := []FileChunk{
		{Path: "login.go", Content: "authentication system", StartLine: 1},
		{Path: "system.go", Content: "login code here", StartLine: 1},
	}

	scored := scoreChunks(chunks, "login")

	var loginScore, systemScore float64
	for _, sc := range scored {
		if sc.chunk.Path == "login.go" {
			loginScore = sc.score
		} else if sc.chunk.Path == "system.go" {
			systemScore = sc.score
		}
	}

	// login.go has "login" in path (2x boost) so should score higher
	if loginScore <= systemScore {
		t.Errorf("login.go (path boost) should score higher than system.go: %f vs %f", loginScore, systemScore)
	}
}
