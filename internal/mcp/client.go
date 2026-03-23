package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// pendingCall holds the response channel for an in-flight RPC request.
type pendingCall struct {
	ch chan []byte // buffered with capacity 1; receives raw JSON response bytes
}

// MCPClient is a goroutine-safe JSON-RPC client for the MCP protocol.
//
// Concurrent CallTool, ListTools, and Ping invocations are safe. Each request
// is assigned a unique integer ID and registered in a pending-call map. A single
// background recvLoop goroutine reads from the transport and dispatches each
// response to the waiting goroutine whose request ID matches the response ID.
//
// The recvLoop uses a sync.Cond to wait when no requests are pending, preventing
// it from consuming responses before their corresponding callers have registered
// their pending entries. This is critical for correctness with sequential callers.
//
// Initialize is intentionally NOT routed through recvLoop because it runs
// exactly once, sequentially, before any concurrent use begins.
type MCPClient struct {
	transport Transport
	nextID    int64

	mu       sync.Mutex
	cond     *sync.Cond        // signalled when pending map changes
	pending  map[int]*pendingCall // requestID → waiting goroutine channel
	recvOnce sync.Once            // ensures recvLoop starts exactly once
	recvErr  error                // set when recvLoop terminates with an error; protected by mu
}

// wrapReceiveErr converts io.EOF and io.ErrUnexpectedEOF to a descriptive
// "server disconnected" error so callers get an actionable message rather than
// a bare "EOF" string.
func wrapReceiveErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("mcp: server disconnected: %w", err)
	}
	return err
}

func NewMCPClient(tr Transport) *MCPClient {
	c := &MCPClient{
		transport: tr,
		pending:   make(map[int]*pendingCall),
	}
	c.cond = sync.NewCond(&c.mu)
	return c
}

// startRecvLoop starts the background receive loop exactly once (using sync.Once).
// Must be called before any request that expects a response via the pending map.
func (c *MCPClient) startRecvLoop() {
	c.recvOnce.Do(func() {
		go c.recvLoop()
	})
}

// recvLoop reads JSON-RPC responses from the transport and dispatches them to
// the channel registered in c.pending by matching on response ID.
//
// When the pending map is empty, the loop waits on c.cond before issuing the
// next Receive call. This prevents reading ahead — a response is only read from
// the transport when at least one goroutine is waiting for it. This ensures
// correctness for both concurrent and sequential callers.
//
// The loop recovers from panics in the transport so they do not crash the
// entire process; a panic is converted to an error and all pending callers
// are unblocked.
//
// If the transport returns an error, all pending channels are closed so waiting
// goroutines unblock immediately. Notifications (no "id" field) and responses
// for unknown IDs are logged and discarded.
func (c *MCPClient) recvLoop() {
	defer func() {
		if r := recover(); r != nil {
			panicErr := fmt.Errorf("mcp: transport panic in recvLoop: %v", r)
			slog.Error("mcp: recvLoop recovered from panic", "panic", r)
			c.mu.Lock()
			if c.recvErr == nil {
				c.recvErr = panicErr
			}
			for id, pc := range c.pending {
				close(pc.ch)
				delete(c.pending, id)
			}
			c.mu.Unlock()
		}
	}()

	for {
		// Wait until at least one pending request exists.
		// This prevents reading ahead and discarding responses for callers
		// that haven't registered their pending entry yet.
		c.mu.Lock()
		for len(c.pending) == 0 && c.recvErr == nil {
			c.cond.Wait()
		}
		if c.recvErr != nil {
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()

		// Read outside the lock so callers can register new pending entries
		// concurrently while we block on I/O.
		// Use a background context so the loop is not tied to any single call's ctx.
		raw, err := c.transport.Receive(context.Background())
		if err != nil {
			wrappedErr := wrapReceiveErr(err)
			c.mu.Lock()
			c.recvErr = wrappedErr
			// Close all pending channels so waiting goroutines unblock.
			for id, pc := range c.pending {
				close(pc.ch)
				delete(c.pending, id)
			}
			c.mu.Unlock()
			return
		}

		// Peek at the ID field without fully unmarshalling.
		var peek struct {
			ID *int `json:"id"`
		}
		if jsonErr := json.Unmarshal(raw, &peek); jsonErr != nil || peek.ID == nil {
			// Notification (no id) or malformed frame — log and skip.
			slog.Debug("mcp: recvLoop: dropping frame without id",
				"frame", string(raw), "parse_err", jsonErr)
			continue
		}

		id := *peek.ID
		c.mu.Lock()
		pc, ok := c.pending[id]
		if ok {
			delete(c.pending, id)
		}
		c.mu.Unlock()

		if !ok {
			slog.Warn("mcp: recvLoop: received response for unknown request id",
				"id", id)
			continue
		}

		// Non-blocking send — the channel is buffered(1) and we just deleted the
		// entry from the map so no other goroutine can write to this channel.
		pc.ch <- raw
	}
}

// sendRequest registers a pending call, sends the request, and waits for the
// response. It is used by CallTool, ListTools, and Ping — all concurrent-safe
// operations that route through recvLoop.
func (c *MCPClient) sendRequest(ctx context.Context, id int, reqData []byte) ([]byte, error) {
	// Ensure the recv loop is running before we register the pending entry.
	c.startRecvLoop()

	pc := &pendingCall{ch: make(chan []byte, 1)}

	c.mu.Lock()
	// Surface any pre-existing transport error before attempting to send.
	if c.recvErr != nil {
		err := c.recvErr
		c.mu.Unlock()
		return nil, err
	}
	c.pending[id] = pc
	// Signal recvLoop that there is now at least one pending entry.
	c.cond.Signal()
	c.mu.Unlock()

	if err := c.transport.Send(ctx, reqData); err != nil {
		// Remove from pending map so recvLoop does not try to deliver to it.
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case raw, ok := <-pc.ch:
		if !ok {
			// Channel was closed by recvLoop due to a transport error.
			c.mu.Lock()
			err := c.recvErr
			c.mu.Unlock()
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("mcp: transport closed unexpectedly for request id %d", id)
		}
		return raw, nil
	case <-ctx.Done():
		// Request timed out or was cancelled. Remove from pending so recvLoop
		// discards any late-arriving response instead of blocking.
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

// Initialize performs the MCP handshake. It is sequential and does NOT use
// the recvLoop / pending-map mechanism, because no concurrent use can begin
// before Initialize returns successfully.
func (c *MCPClient) Initialize(ctx context.Context) error {
	id := int(atomic.AddInt64(&c.nextID, 1))
	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "initialize",
		Params: initializeParams{
			ProtocolVersion: ProtocolVersion,
			Capabilities:    map[string]any{"tools": map[string]any{}},
			ClientInfo:      ClientInfo,
		},
	}
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("initialize marshal: %w", err)
	}
	if err := c.transport.Send(ctx, data); err != nil {
		return fmt.Errorf("initialize send: %w", err)
	}
	// Apply a bounded timeout on Receive: if the server accepts the connection
	// but never sends initialize response, we should not hang forever.
	receiveCtx, receiveCancel := context.WithTimeout(ctx, 10*time.Second)
	defer receiveCancel()
	respData, err := c.transport.Receive(receiveCtx)
	if err != nil {
		return fmt.Errorf("initialize receive: %w", wrapReceiveErr(err))
	}
	var resp Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		return err
	}
	if resp.Error != nil {
		return resp.Error
	}
	// Send initialized notification (no ID)
	notif := map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}
	notifData, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("initialized notification marshal: %w", err)
	}
	if err := c.transport.Send(ctx, notifData); err != nil {
		return fmt.Errorf("initialized notification send: %w", err)
	}
	return nil
}

func (c *MCPClient) ListTools(ctx context.Context) ([]MCPTool, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	id := int(atomic.AddInt64(&c.nextID, 1))
	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/list",
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("list tools marshal: %w", err)
	}
	respData, err := c.sendRequest(ctx, id, data)
	if err != nil {
		return nil, wrapReceiveErr(err)
	}
	var resp Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	var result MCPToolsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

func (c *MCPClient) CallTool(ctx context.Context, name string, args map[string]any) (*MCPToolCallResult, error) {
	// Apply a 2-minute default timeout when the caller has no deadline set.
	// Tool calls can be long-running (file I/O, web fetch) so we use a generous
	// timeout — but unbounded calls risk blocking the manager indefinitely.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
	}
	id := int(atomic.AddInt64(&c.nextID, 1))
	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/call",
		Params: toolCallParams{
			Name:      name,
			Arguments: args,
		},
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("call tool marshal: %w", err)
	}
	respData, err := c.sendRequest(ctx, id, data)
	if err != nil {
		return nil, wrapReceiveErr(err)
	}
	var resp Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, err
	}
	if resp.ID != id {
		return nil, fmt.Errorf("mcp: response ID mismatch: got %d, want %d", resp.ID, id)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	var result MCPToolCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Ping sends a JSON-RPC "ping" request and waits for the response.
// Returns nil on success. Returns an *RPCError with code -32601 (MethodNotFound)
// when the server does not implement ping — callers should fall back to ListTools.
func (c *MCPClient) Ping(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	id := int(atomic.AddInt64(&c.nextID, 1))
	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "ping",
	}
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("ping marshal: %w", err)
	}
	respData, err := c.sendRequest(ctx, id, data)
	if err != nil {
		return wrapReceiveErr(err)
	}
	var resp Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		return err
	}
	if resp.Error != nil {
		return resp.Error
	}
	return nil
}

func (c *MCPClient) Close() error {
	return c.transport.Close()
}
