package search_test

import (
	"testing"
	"github.com/scrypster/huginn/internal/search"
)

func TestChunkFile_GoFile_FunctionBoundaries(t *testing.T) {
	src := `package main

func FooBar() {
    println("hello")
}

func BazQux() {
    println("world")
}
`
	chunks := search.ChunkFile("main.go", []byte(src))
	if len(chunks) < 2 {
		t.Errorf("expected >= 2 chunks for 2 functions, got %d", len(chunks))
	}
}

func TestChunkFile_NonGo_LineWindows(t *testing.T) {
	lines := make([]byte, 0)
	for i := 0; i < 120; i++ {
		lines = append(lines, []byte("line\n")...)
	}
	chunks := search.ChunkFile("readme.txt", lines)
	if len(chunks) < 3 {
		t.Errorf("expected >= 3 windows for 120 lines, got %d", len(chunks))
	}
}

func TestChunkFile_EmptyContent(t *testing.T) {
	chunks := search.ChunkFile("empty.go", nil)
	if chunks == nil {
		chunks = []search.Chunk{}
	}
	// No panic is the key requirement
}
