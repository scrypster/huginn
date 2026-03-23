package tools

import (
	"strings"
	"testing"
)

func TestMergeEnv(t *testing.T) {
	cases := []struct {
		name      string
		base      []string
		overrides []string
		wantKey   string
		wantVal   string
	}{
		{"base-only key preserved", []string{"FOO=bar"}, nil, "FOO", "bar"},
		{"override wins on collision", []string{"FOO=bar"}, []string{"FOO=baz"}, "FOO", "baz"},
		{"override-only key added", nil, []string{"NEW=value"}, "NEW", "value"},
		{"empty-value override preserved", []string{"BASH_ENV=/some/file"}, []string{"BASH_ENV="}, "BASH_ENV", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := mergeEnv(tc.base, tc.overrides)
			for _, e := range result {
				k, v, _ := strings.Cut(e, "=")
				if k == tc.wantKey {
					if v != tc.wantVal {
						t.Errorf("key %s: got %q, want %q", tc.wantKey, v, tc.wantVal)
					}
					return
				}
			}
			if tc.wantKey != "" {
				t.Errorf("key %s not found in result", tc.wantKey)
			}
		})
	}
}
