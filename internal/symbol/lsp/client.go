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

// maxUnsolicitedMessages bounds how many server-initiated messages
// (notifications, or requests we don't handle) receiveResponse will skip
// while looking for the response matching id, before giving up. Real
// servers (gopls) interleave window/showMessage and window/logMessage
// notifications — e.g. "Loading packages..." — with in-flight requests,
// especially during the initial workspace load.
const maxUnsolicitedMessages = 500

// receiveResponse reads messages off the transport until it finds the
// response whose id matches the given request id, discarding any
// notifications or unrelated messages in between. A naive single Receive
// call would misparse the first unsolicited notification as the response,
// silently returning an empty/wrong result instead of erroring.
func (c *Client) receiveResponse(id int64) (rpcResponse, error) {
	for i := 0; i < maxUnsolicitedMessages; i++ {
		var resp rpcResponse
		if err := c.tr.Receive(&resp); err != nil {
			return rpcResponse{}, err
		}
		if resp.ID == id {
			return resp, nil
		}
		// Notification (no id) or a response to a different in-flight
		// request/server-initiated request: not what we're waiting for.
	}
	return rpcResponse{}, fmt.Errorf("lsp: no response for request %d after %d messages", id, maxUnsolicitedMessages)
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
					"definition": map[string]any{},
					"references": map[string]any{},
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
	resp, err := c.receiveResponse(id)
	if err != nil {
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
	resp, err := c.receiveResponse(id)
	if err != nil {
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
	resp, err := c.receiveResponse(id)
	if err != nil {
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

// DidOpen notifies the server that fileURI is open with the given content,
// as textDocument/didOpen. Servers such as gopls resolve position-based
// requests (e.g. textDocument/definition) more reliably — and in some cases
// only — against documents that have been explicitly opened; a request
// against a file that was never opened can silently return an empty result
// rather than an error.
func (c *Client) DidOpen(fileURI, languageID, text string) error {
	notif := rpcNotification{
		JSONRPC: "2.0",
		Method:  "textDocument/didOpen",
		Params: map[string]any{
			"textDocument": map[string]any{
				"uri":        fileURI,
				"languageId": languageID,
				"version":    1,
				"text":       text,
			},
		},
	}
	return c.tr.Send(notif)
}
