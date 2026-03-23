package search

import (
	"go/parser"
	"go/token"
	"strings"
)

// ChunkFile splits a file into semantic chunks.
// .go files: function/method/type boundaries via go/ast
// Other files: 40-line windows
func ChunkFile(path string, content []byte) []Chunk {
	if len(content) == 0 {
		return nil
	}
	if strings.HasSuffix(path, ".go") {
		chunks := chunkGoFile(path, content)
		if chunks != nil {
			return chunks
		}
	}
	return chunkLineWindows(path, content)
}

// chunkGoFile parses a Go file and returns chunks at declaration boundaries.
func chunkGoFile(path string, content []byte) []Chunk {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, content, parser.ParseComments)
	if err != nil {
		return nil
	}

	var chunks []Chunk
	var id uint64

	for _, decl := range f.Decls {
		start := fset.Position(decl.Pos())
		end := fset.Position(decl.End())

		if end.Offset > start.Offset && start.Offset < len(content) {
			endOff := end.Offset
			if endOff > len(content) {
				endOff = len(content)
			}

			id++
			chunks = append(chunks, Chunk{
				ID:        id,
				Path:      path,
				Content:   string(content[start.Offset:endOff]),
				StartLine: start.Line,
			})
		}
	}

	return chunks
}

// chunkLineWindows splits non-Go files into fixed-size line windows (40 lines each).
func chunkLineWindows(path string, content []byte) []Chunk {
	lines := strings.Split(string(content), "\n")
	const windowSize = 40
	var chunks []Chunk
	var id uint64

	for i := 0; i < len(lines); i += windowSize {
		end := i + windowSize
		if end > len(lines) {
			end = len(lines)
		}

		id++
		chunks = append(chunks, Chunk{
			ID:        id,
			Path:      path,
			Content:   strings.Join(lines[i:end], "\n"),
			StartLine: i + 1,
		})
	}

	return chunks
}
