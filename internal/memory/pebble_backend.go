package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/pebble/v2"
)

// pebbleRecord is stored as JSON under key "m:<vault>:<key>".
type pebbleRecord struct {
	Content   string    `json:"content"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
}

// PebbleBackend is a simple keyword+recency fallback memory store.
type PebbleBackend struct {
	db *pebble.DB
}

// NewPebbleBackend opens (or creates) a Pebble DB at path.
func NewPebbleBackend(path string) (*PebbleBackend, error) {
	db, err := pebble.Open(path, &pebble.Options{})
	if err != nil {
		return nil, fmt.Errorf("pebble backend: open %s: %w", path, err)
	}
	return &PebbleBackend{db: db}, nil
}

func (p *PebbleBackend) Available() bool { return p.db != nil }

func (p *PebbleBackend) EnsureVault(_ context.Context, _ string) error {
	return nil // vaults are implicit (key namespaced)
}

func (p *PebbleBackend) Write(_ context.Context, vault, key, content string, tags []string) error {
	rec := pebbleRecord{
		Content:   content,
		Tags:      tags,
		CreatedAt: time.Now(),
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return p.db.Set([]byte("m:"+escapeComponent(vault)+":"+escapeComponent(key)), b, pebble.Sync)
}

func (p *PebbleBackend) Recall(_ context.Context, vault string, queryParts []string, topK int) ([]MemoryRecord, error) {
	prefix := []byte("m:" + escapeComponent(vault) + ":")
	upperBound := incrementBytes(prefix)

	iter, err := p.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: upperBound,
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	type scored struct {
		rec   MemoryRecord
		score float64
	}
	var candidates []scored

	queryLower := strings.ToLower(strings.Join(queryParts, " "))
	queryTokens := strings.Fields(queryLower)

	now := time.Now()
	for iter.First(); iter.Valid(); iter.Next() {
		val, err := iter.ValueAndErr()
		if err != nil {
			continue
		}
		var pr pebbleRecord
		if err := json.Unmarshal(val, &pr); err != nil {
			continue
		}

		// BM25-lite: token overlap between query and content+tags
		text := strings.ToLower(pr.Content + " " + strings.Join(pr.Tags, " "))
		termScore := 0.0
		for _, tok := range queryTokens {
			if strings.Contains(text, tok) {
				termScore++
			}
		}
		if len(queryTokens) > 0 {
			termScore /= float64(len(queryTokens))
		}

		// Recency decay: half-life 30 days
		ageDays := now.Sub(pr.CreatedAt).Hours() / 24
		recency := math.Exp(-ageDays / 30.0)

		score := 0.7*termScore + 0.3*recency
		if score > 0 {
			candidates = append(candidates, scored{
				rec: MemoryRecord{
					Content:     pr.Content,
					Tags:        pr.Tags,
					Score:       score,
					SourceVault: vault,
				},
				score: score,
			})
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("pebble backend: recall scan: %w", err)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	if topK < len(candidates) {
		candidates = candidates[:topK]
	}
	out := make([]MemoryRecord, len(candidates))
	for i, c := range candidates {
		out[i] = c.rec
	}
	return out, nil
}

// incrementBytes returns the exclusive upper bound for a prefix scan.
func incrementBytes(b []byte) []byte {
	end := make([]byte, len(b))
	copy(end, b)
	for i := len(end) - 1; i >= 0; i-- {
		end[i]++
		if end[i] != 0 {
			return end
		}
	}
	return append(b, 0x00)
}

// escapeComponent replaces colons with %3A to prevent key collisions.
func escapeComponent(s string) string {
	return strings.ReplaceAll(s, ":", "%3A")
}

// Close releases the Pebble DB.
func (p *PebbleBackend) Close() error {
	if p.db != nil {
		return p.db.Close()
	}
	return nil
}

var _ MemoryBackend = (*PebbleBackend)(nil)
var _ io.Closer = (*PebbleBackend)(nil)
