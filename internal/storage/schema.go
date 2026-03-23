package storage

// Key builders — all return []byte for Pebble.

func keyMetaGitHead() []byte {
	return []byte("meta:git_head")
}

func keyMetaWorkspaceID() []byte {
	return []byte("meta:workspace_id")
}

func keyFileHash(path string) []byte {
	return []byte("file:" + path + ":hash")
}

func keyFileParserVersion(path string) []byte {
	return []byte("file:" + path + ":parser_version")
}

func keyFileSymbols(path string) []byte {
	return []byte("file:" + path + ":symbols")
}

func keyFileChunks(path string) []byte {
	return []byte("file:" + path + ":chunks")
}

func keyFileIndexedAt(path string) []byte {
	return []byte("file:" + path + ":indexed_at")
}

func keyEdge(from, to string) []byte {
	return []byte("edge:" + from + ":" + to)
}

func keyWSSummary() []byte {
	return []byte("ws:summary")
}

// keyEdgePrefix returns the prefix for all edges from a given path.
func keyEdgePrefix(path string) []byte {
	return []byte("edge:" + path + ":")
}

// keyFilePrefix returns the prefix for all keys related to a file.
func keyFilePrefix(path string) []byte {
	return []byte("file:" + path + ":")
}

// keyEdgeTo returns the reverse-index key for an edge (to → from lookup).
func keyEdgeTo(to, from string) []byte {
	return []byte("edgeto:" + to + ":" + from)
}

// keyEdgeToPrefix returns the prefix for all reverse-index entries pointing to 'to'.
func keyEdgeToPrefix(to string) []byte {
	return []byte("edgeto:" + to + ":")
}
