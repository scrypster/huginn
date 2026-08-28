package validation

import (
	"strings"
	"testing"
)

func TestNormalizeMode(t *testing.T) {
	cases := map[string]Mode{
		"":        ModeBlock,
		"block":   ModeBlock,
		"BLOCK":   ModeBlock,
		" block ": ModeBlock,
		"warn":    ModeWarn,
		"WARN":    ModeWarn,
		"off":     ModeOff,
		"OFF":     ModeOff,
		"bogus":   ModeBlock, // unrecognized defaults to the safe choice
	}
	for in, want := range cases {
		if got := NormalizeMode(in); got != want {
			t.Errorf("NormalizeMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidate_Go_Valid(t *testing.T) {
	res := Validate("ok.go", []byte("package main\n\nfunc main() {}\n"))
	if !res.Checked {
		t.Fatal("expected Checked=true for .go")
	}
	if !res.OK {
		t.Errorf("expected OK=true, got Message=%q", res.Message)
	}
	if res.Lang != "go" {
		t.Errorf("Lang = %q, want go", res.Lang)
	}
}

func TestValidate_Go_Broken(t *testing.T) {
	res := Validate("bad.go", []byte("package main\n\nfunc main( {\n"))
	if !res.Checked {
		t.Fatal("expected Checked=true for .go")
	}
	if res.OK {
		t.Fatal("expected OK=false for broken Go source")
	}
	if res.Message == "" {
		t.Error("expected a non-empty parser error message")
	}
}

func TestValidate_UnsupportedExtension_Skips(t *testing.T) {
	res := Validate("data.json", []byte("{not json"))
	if res.Checked {
		t.Error("expected Checked=false for an extension with no validator")
	}
}

func TestValidate_TSJS_DocumentedGap(t *testing.T) {
	for _, ext := range []string{"broken.ts", "broken.tsx", "broken.js", "broken.jsx"} {
		res := Validate(ext, []byte("function broken( {"))
		if res.Checked {
			t.Errorf("%s: expected Checked=false (documented v1 gap)", ext)
		}
		if res.Lang != "ts/js" {
			t.Errorf("%s: Lang = %q, want ts/js", ext, res.Lang)
		}
		if !strings.Contains(res.SkipReason, "not implemented") {
			t.Errorf("%s: SkipReason should explain the gap, got %q", ext, res.SkipReason)
		}
	}
}

func TestValidate_Python_ValidAndBroken(t *testing.T) {
	ok := Validate("ok.py", []byte("def f():\n    return 1\n"))
	if !ok.Checked {
		t.Skip("python3 not available on PATH in this environment")
	}
	if !ok.OK {
		t.Errorf("expected valid Python to pass, got Message=%q", ok.Message)
	}

	bad := Validate("bad.py", []byte("def broken(:\n    pass\n"))
	if !bad.Checked {
		t.Skip("python3 not available on PATH in this environment")
	}
	if bad.OK {
		t.Fatal("expected OK=false for broken Python source")
	}
	if bad.Message == "" {
		t.Error("expected a non-empty py_compile error message")
	}
}

func TestValidate_Python_MissingInterpreterSkipsGracefully(t *testing.T) {
	t.Setenv("PATH", "")
	res := Validate("bad.py", []byte("def broken(:\n    pass\n"))
	if res.Checked {
		t.Fatal("expected Checked=false when python3 is not on PATH")
	}
	if res.SkipReason == "" {
		t.Error("expected a SkipReason explaining why validation was skipped")
	}
}
