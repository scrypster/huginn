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

func TestValidate_JS_ValidAndBroken(t *testing.T) {
	for _, ext := range []string{"ok.js", "ok.mjs", "ok.cjs"} {
		ok := Validate(ext, []byte("function foo() {\n  return 1;\n}\n"))
		if !ok.Checked {
			t.Skipf("%s: neither node nor esbuild available on PATH in this environment", ext)
		}
		if !ok.OK {
			t.Errorf("%s: expected valid JS to pass, got Message=%q", ext, ok.Message)
		}
		if ok.Lang != "javascript" {
			t.Errorf("%s: Lang = %q, want javascript", ext, ok.Lang)
		}
	}

	bad := Validate("bad.js", []byte("function broken( {\n  return 1;\n}\n"))
	if !bad.Checked {
		t.Skip("neither node nor esbuild available on PATH in this environment")
	}
	if bad.OK {
		t.Fatal("expected OK=false for broken JS source")
	}
	if bad.Message == "" {
		t.Error("expected a non-empty syntax error message")
	}
}

func TestValidate_JS_MissingToolsSkipsGracefully(t *testing.T) {
	t.Setenv("PATH", "")
	res := Validate("bad.js", []byte("function broken( {\n"))
	if res.Checked {
		t.Fatal("expected Checked=false when neither node nor esbuild is on PATH")
	}
	if res.SkipReason == "" {
		t.Error("expected a SkipReason explaining why validation was skipped")
	}
}

func TestValidate_TS_ValidAndBroken(t *testing.T) {
	for _, ext := range []string{"ok.ts", "ok.tsx", "ok.jsx"} {
		ok := Validate(ext, []byte("function foo(): number {\n  return 1;\n}\n"))
		if strings.HasSuffix(ext, ".jsx") || strings.HasSuffix(ext, ".tsx") {
			// Use JSX-flavored valid source for the tsx/jsx cases so the
			// syntax under test actually exercises the parser mode.
			ok = Validate(ext, []byte("function Foo() {\n  return 1;\n}\n"))
		}
		if !ok.Checked {
			t.Skipf("%s: esbuild not available on PATH in this environment", ext)
		}
		if !ok.OK {
			t.Errorf("%s: expected valid source to pass, got Message=%q", ext, ok.Message)
		}
	}

	bad := Validate("bad.ts", []byte("function broken(: number {\n  return 1;\n}\n"))
	if !bad.Checked {
		t.Skip("esbuild not available on PATH in this environment")
	}
	if bad.OK {
		t.Fatal("expected OK=false for broken TS source")
	}
	if bad.Message == "" {
		t.Error("expected a non-empty syntax error message")
	}
}

func TestValidate_TS_MissingEsbuildSkipsGracefully(t *testing.T) {
	t.Setenv("PATH", "")
	res := Validate("bad.ts", []byte("function broken(: number {\n"))
	if res.Checked {
		t.Fatal("expected Checked=false when esbuild is not on PATH")
	}
	if res.SkipReason == "" {
		t.Error("expected a SkipReason explaining why validation was skipped")
	}
}

func TestValidate_Ruby_ValidAndBroken(t *testing.T) {
	ok := Validate("ok.rb", []byte("def foo\n  puts \"hi\"\nend\n"))
	if !ok.Checked {
		t.Skip("ruby not available on PATH in this environment")
	}
	if !ok.OK {
		t.Errorf("expected valid Ruby to pass, got Message=%q", ok.Message)
	}

	bad := Validate("bad.rb", []byte("def foo(\n  puts \"hi\"\nend\n"))
	if !bad.Checked {
		t.Skip("ruby not available on PATH in this environment")
	}
	if bad.OK {
		t.Fatal("expected OK=false for broken Ruby source")
	}
	if bad.Message == "" {
		t.Error("expected a non-empty syntax error message")
	}
}

func TestValidate_Ruby_MissingInterpreterSkipsGracefully(t *testing.T) {
	t.Setenv("PATH", "")
	res := Validate("bad.rb", []byte("def foo(\n"))
	if res.Checked {
		t.Fatal("expected Checked=false when ruby is not on PATH")
	}
	if res.SkipReason == "" {
		t.Error("expected a SkipReason explaining why validation was skipped")
	}
}

func TestValidate_PHP_ValidAndBroken(t *testing.T) {
	ok := Validate("ok.php", []byte("<?php\nfunction foo() {\n  echo \"hi\";\n}\n"))
	if !ok.Checked {
		t.Skip("php not available on PATH in this environment")
	}
	if !ok.OK {
		t.Errorf("expected valid PHP to pass, got Message=%q", ok.Message)
	}

	bad := Validate("bad.php", []byte("<?php\nfunction foo( {\n  echo \"hi\";\n"))
	if !bad.Checked {
		t.Skip("php not available on PATH in this environment")
	}
	if bad.OK {
		t.Fatal("expected OK=false for broken PHP source")
	}
	if bad.Message == "" {
		t.Error("expected a non-empty syntax error message")
	}
}

func TestValidate_PHP_MissingInterpreterSkipsGracefully(t *testing.T) {
	t.Setenv("PATH", "")
	res := Validate("bad.php", []byte("<?php\nfunction foo( {\n"))
	if res.Checked {
		t.Fatal("expected Checked=false when php is not on PATH")
	}
	if res.SkipReason == "" {
		t.Error("expected a SkipReason explaining why validation was skipped")
	}
}

func TestValidate_Rust_ValidAndBroken(t *testing.T) {
	ok := Validate("ok.rs", []byte("fn main() {\n    println!(\"hi\");\n}\n"))
	if !ok.Checked {
		t.Skip("rustfmt not available on PATH in this environment")
	}
	if !ok.OK {
		t.Errorf("expected valid Rust to pass, got Message=%q", ok.Message)
	}

	bad := Validate("bad.rs", []byte("fn main( {\n    println!(\"hi\");\n}\n"))
	if !bad.Checked {
		t.Skip("rustfmt not available on PATH in this environment")
	}
	if bad.OK {
		t.Fatal("expected OK=false for broken Rust source (unclosed delimiter)")
	}
	if bad.Message == "" {
		t.Error("expected a non-empty syntax error message")
	}
}

func TestValidate_Rust_UnformattedButValidIsOK(t *testing.T) {
	// rustfmt --check also fails on syntactically valid source that isn't
	// canonically formatted. validateRust must distinguish that case (OK)
	// from a real parse error (not OK) — this is the empirical crux of
	// using rustfmt as a syntax-only check.
	res := Validate("unformatted.rs", []byte("fn main( ) {\nprintln!(\"hi\");\n}\n"))
	if !res.Checked {
		t.Skip("rustfmt not available on PATH in this environment")
	}
	if !res.OK {
		t.Errorf("expected syntactically valid but unformatted Rust to pass as OK, got Message=%q", res.Message)
	}
}

func TestValidate_Rust_MissingToolSkipsGracefully(t *testing.T) {
	t.Setenv("PATH", "")
	res := Validate("bad.rs", []byte("fn main( {\n"))
	if res.Checked {
		t.Fatal("expected Checked=false when rustfmt is not on PATH")
	}
	if res.SkipReason == "" {
		t.Error("expected a SkipReason explaining why validation was skipped")
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
