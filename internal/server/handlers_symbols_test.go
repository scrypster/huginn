package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/symbol"
)

type fakeSymbolStore struct {
	edges []symbol.Edge
}

func (f *fakeSymbolStore) GetAllSymbolEdges() []symbol.Edge {
	return f.edges
}

func TestHandleSymbolSearch_WhitespaceQueryReturns400(t *testing.T) {
	srv := &Server{}
	srv.SetSymbolStore(&fakeSymbolStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/symbols/search?q=%20%20%20", nil)
	w := httptest.NewRecorder()
	srv.handleSymbolSearch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for whitespace query, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHandleSymbolSearch_InvalidLimitReturns400(t *testing.T) {
	srv := &Server{}
	srv.SetSymbolStore(&fakeSymbolStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/symbols/search?q=foo&limit=abc", nil)
	w := httptest.NewRecorder()
	srv.handleSymbolSearch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid limit, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHandleSymbolSearch_CaseInsensitiveSortedAndTruncated(t *testing.T) {
	srv := &Server{}
	srv.SetSymbolStore(&fakeSymbolStore{
		edges: []symbol.Edge{
			{From: "a.go", To: "b.go", Symbol: "myFunc", Confidence: symbol.ConfHigh, Kind: symbol.EdgeCall},
			{From: "c.go", To: "d.go", Symbol: "MyFuncExtra", Confidence: symbol.ConfMedium, Kind: symbol.EdgeCall},
			{From: "e.go", To: "f.go", Symbol: "anotherMyfunc", Confidence: symbol.ConfLow, Kind: symbol.EdgeImport},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/symbols/search?q=myfunc&limit=2", nil)
	w := httptest.NewRecorder()
	srv.handleSymbolSearch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var got struct {
		Symbols   []string `json:"symbols"`
		Truncated bool     `json:"truncated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(got.Symbols) != 2 {
		t.Fatalf("expected 2 symbols due to limit, got %d (%v)", len(got.Symbols), got.Symbols)
	}
	if !got.Truncated {
		t.Fatalf("expected truncated=true when results exceed limit")
	}
	if got.Symbols[0] != "MyFuncExtra" || got.Symbols[1] != "anotherMyfunc" {
		t.Fatalf("expected sorted symbols [MyFuncExtra anotherMyfunc], got %v", got.Symbols)
	}
}

func TestHandleSymbolImpact_ReturnsGroupedImpact(t *testing.T) {
	srv := &Server{}
	srv.SetSymbolStore(&fakeSymbolStore{
		edges: []symbol.Edge{
			{From: "a.go", To: "b.go", Symbol: "Compute", Confidence: symbol.ConfHigh, Kind: symbol.EdgeCall},
			{From: "c.go", To: "b.go", Symbol: "Compute", Confidence: symbol.ConfLow, Kind: symbol.EdgeImport},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/symbols/impact/Compute", nil)
	req.SetPathValue("symbol", "Compute")
	w := httptest.NewRecorder()
	srv.handleSymbolImpact(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var got symbol.ImpactReport
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal impact response: %v", err)
	}
	if got.Symbol != "Compute" {
		t.Fatalf("expected symbol Compute, got %q", got.Symbol)
	}
	if len(got.High) != 1 || got.High[0].Path != "a.go" {
		t.Fatalf("expected one high-confidence path a.go, got %+v", got.High)
	}
	if len(got.Low) != 1 || got.Low[0].Path != "c.go" {
		t.Fatalf("expected one low-confidence path c.go, got %+v", got.Low)
	}
}
