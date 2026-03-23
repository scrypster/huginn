package threadmgr

import (
	"testing"
)

func TestFinishTool_PopulatesFinishSummary(t *testing.T) {
	tt := &ThreadTools{}
	args := map[string]any{
		"summary":        "did the work",
		"files_modified": []any{"main.go", "foo.go"},
		"key_decisions":  []any{"used sync.Mutex"},
		"artifacts":      []any{"build/output.bin"},
		"status":         "completed",
	}

	var caught *ErrFinish
	func() {
		defer func() {
			if r := recover(); r != nil {
				if e, ok := r.(*ErrFinish); ok {
					caught = e
				}
			}
		}()
		tt.Finish(args)
	}()

	if caught == nil {
		t.Fatal("expected ErrFinish panic, got none")
	}
	if caught.Summary.Summary != "did the work" {
		t.Errorf("summary mismatch: got %q", caught.Summary.Summary)
	}
	if len(caught.Summary.FilesModified) != 2 {
		t.Errorf("expected 2 files_modified, got %d", len(caught.Summary.FilesModified))
	}
	if caught.Summary.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", caught.Summary.Status)
	}
}

func TestFinishTool_DefaultStatus(t *testing.T) {
	tt := &ThreadTools{}
	args := map[string]any{"summary": "done"}

	var caught *ErrFinish
	func() {
		defer func() {
			if r := recover(); r != nil {
				if e, ok := r.(*ErrFinish); ok {
					caught = e
				}
			}
		}()
		tt.Finish(args)
	}()

	if caught == nil {
		t.Fatal("expected ErrFinish panic")
	}
	if caught.Summary.Status != "completed" {
		t.Errorf("default status should be 'completed', got %q", caught.Summary.Status)
	}
}

func TestRequestHelpTool_PanicsWithErrHelp(t *testing.T) {
	tt := &ThreadTools{}
	args := map[string]any{"message": "stuck on auth"}

	var caught *ErrHelp
	func() {
		defer func() {
			if r := recover(); r != nil {
				if e, ok := r.(*ErrHelp); ok {
					caught = e
				}
			}
		}()
		tt.RequestHelp(args)
	}()

	if caught == nil {
		t.Fatal("expected ErrHelp panic, got none")
	}
	if caught.Message != "stuck on auth" {
		t.Errorf("message mismatch: got %q", caught.Message)
	}
}

func TestRequestHelpTool_EmptyMessage(t *testing.T) {
	tt := &ThreadTools{}
	args := map[string]any{}

	var caught *ErrHelp
	func() {
		defer func() {
			if r := recover(); r != nil {
				if e, ok := r.(*ErrHelp); ok {
					caught = e
				}
			}
		}()
		tt.RequestHelp(args)
	}()

	if caught == nil {
		t.Fatal("expected ErrHelp panic with empty message")
	}
}
