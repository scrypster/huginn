package storage

import "time"

// FileRecord holds all indexed data for one file.
type FileRecord struct {
	Path          string    `json:"path"`
	Hash          string    `json:"hash"`            // SHA-256 hex of file content
	ParserVersion int       `json:"parser_version"`
	IndexedAt     time.Time `json:"indexed_at"`
}

// Symbol represents a code symbol extracted from a file.
type Symbol struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`     // "function","class","interface","type","variable","import","export"
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Exported bool   `json:"exported"`
}

// Edge represents a relationship between two files in the call/import graph.
type Edge struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Symbol     string `json:"symbol"`
	Confidence string `json:"confidence"` // "HIGH","MEDIUM","LOW"
	Kind       string `json:"kind"`       // "Import","Call","Instantiation","Extends","Implements"
}

// FileChunk is a slice of a file's content for RAG context building.
type FileChunk struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	StartLine int    `json:"start_line"`
}

// WorkspaceSummary is a cached summary of workspace intelligence.
type WorkspaceSummary struct {
	TopFilesByRefCount []string          `json:"top_files_by_ref_count"`
	CrossRepoHints     []string          `json:"cross_repo_hints"`
	InferredRepoRoles  map[string]string `json:"inferred_repo_roles"` // path → role
	UpdatedAt          time.Time         `json:"updated_at"`
}
