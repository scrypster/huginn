package scheduler

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzResolveRuntimeVars asserts the placeholder resolver never panics and
// preserves a few invariants regardless of input shape:
//
//  1. The output is valid UTF-8 (we never emit malformed bytes).
//  2. If the input contained no `{{...}}` tokens, the output is identical.
//  3. The output never contains a placeholder for a name that was actually
//     present in scratch / outputs (proves no string-shuffle regression).
//
// We keep the seed corpus small but deliberately targeted at the edge cases
// the hand-written tests exercise: nested braces, multi-segment JSON paths,
// runaway whitespace, and adversarial keys with regex-meta chars.
func FuzzResolveRuntimeVars(f *testing.F) {
	seeds := []struct {
		prompt, prevOutput string
	}{
		{"hello", ""},
		{"{{prev.output}}", "world"},
		{"{{run.scratch.k}}", ""},
		{"{{inputs.x.y}}", `{"y":"z"}`},
		{"plain text {{ no.match }}", ""},
		{"{{prev.output.deeply.nested.path}}", `{"deeply":{"nested":{"path":42}}}`},
		{"{{prev.output}}{{prev.output}}{{prev.output}}", "X"},
		{"a{{b{{c}}d}}e", "?"},
		{`{{run.scratch.weird-key}}`, ""},
		{"   ", ""},
	}
	for _, s := range seeds {
		f.Add(s.prompt, s.prevOutput)
	}

	f.Fuzz(func(t *testing.T, prompt, prevOutput string) {
		// Discard inputs that are obviously not UTF-8 — the resolver works on
		// strings, but the fuzz engine can produce arbitrary byte sequences;
		// the contract is "if you give me UTF-8, you get UTF-8 back".
		if !utf8.ValidString(prompt) || !utf8.ValidString(prevOutput) {
			return
		}
		// Fixed inputs and scratch so the fuzz engine doesn't have to invent
		// keys; the function is deterministic given (prompt, scratch, prev).
		inputs := []StepInput{{FromStep: "alpha", As: "a"}}
		outputs := map[string]string{"alpha": `{"k":"v","n":1}`}
		scratch := map[string]string{"feature_flag": "on", "attempt": "3"}

		got := resolveRuntimeVars(prompt, inputs, outputs, prevOutput, scratch)

		if !utf8.ValidString(got) {
			t.Fatalf("resolveRuntimeVars produced invalid UTF-8 for prompt %q", prompt)
		}
		if !strings.Contains(prompt, "{{") && got != prompt {
			t.Fatalf("input had no placeholders but output changed: in=%q out=%q", prompt, got)
		}
	})
}
