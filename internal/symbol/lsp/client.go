package lsp

import (
	"encoding/json"
	"fmt"
	"io"
	"sync/atomic"
)

type Client struct {
	tr       *Transport
	lang     string
	nextID   atomic.Int64
	initDone bool
}

func NewClient(rw io.ReadWriter, lang string) *Client {
	return &Client{tr: NewTransport(rw), lang: lang}
}

func (c *Client) Initialize(rootURI string) error {
	id := c.nextID.Add(1)
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "initialize",
		Params: map[string]any{
			"processId": nil,
			"rootUri":   rootURI,
			"capabilities": map[string]any{
				"textDocument": map[string]any{
					"definition":  map[string]any{},
					"references":  map[string]any{},
				},
				"workspace": map[string]any{
					"symbol": map[string]any{},
				},
			},
		},
	}
	if err := c.tr.Send(req); err != nil {
		return err
	}
	var resp rpcResponse
	if err := c.tr.Receive(&resp); err != nil {
		return err
	}
	if resp.Error != nil {
		return resp.Error
	}
	notif := rpcNotification{
		JSONRPC: "2.0",
		Method:  "initialized",
		Params:  map[string]any{},
	}
	if err := c.tr.Send(notif); err != nil {
		return err
	}
	c.initDone = true
	return nil
}

func (c *Client) TextDocumentDefinition(fileURI string, line, column int) ([]Location, error) {
	if !c.initDone {
		return nil, fmt.Errorf("lsp: not initialized")
	}
	id := c.nextID.Add(1)
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "textDocument/definition",
		Params: map[string]any{
			"textDocument": map[string]string{"uri": fileURI},
			"position": map[string]int{
				"line":      line - 1,
				"character": column - 1,
			},
		},
	}
	if err := c.tr.Send(req); err != nil {
		return nil, err
	}
	var resp rpcResponse
	if err := c.tr.Receive(&resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	if resp.Result == nil {
		return nil, nil
	}
	var locs []Location
	if err := json.Unmarshal(resp.Result, &locs); err != nil {
		var single Location
		if err2 := json.Unmarshal(resp.Result, &single); err2 == nil && single.URI != "" {
			return []Location{single}, nil
		}
		return nil, err
	}
	return locs, nil
}

func (c *Client) WorkspaceSymbol(query string) ([]SymbolInformation, error) {
	if !c.initDone {
		return nil, fmt.Errorf("lsp: not initialized")
	}
	id := c.nextID.Add(1)
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "workspace/symbol",
		Params:  map[string]string{"query": query},
	}
	if err := c.tr.Send(req); err != nil {
		return nil, err
	}
	var resp rpcResponse
	if err := c.tr.Receive(&resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	if resp.Result == nil {
		return nil, nil
	}
	var syms []SymbolInformation
	if err := json.Unmarshal(resp.Result, &syms); err != nil {
		return nil, err
	}
	return syms, nil
}
