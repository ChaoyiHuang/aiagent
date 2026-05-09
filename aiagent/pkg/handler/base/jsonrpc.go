package base

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// JSONRPCClient handles stdin/stdout JSON-RPC communication.
type JSONRPCClient struct {
	stdin  io.WriteCloser
	stdout io.Reader

	requestID int
	pending   map[int]*pendingRequest
	mu        sync.Mutex

	// For stream handling
	streamHandlers map[string]chan []byte
	streamMu       sync.RWMutex
}

type pendingRequest struct {
	response chan *jsonRPCResponse
}

type jsonRPCRequest struct {
	Jsonrpc string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      int         `json:"id,omitempty"` // ID=0 means stream
}

type jsonRPCResponse struct {
	Jsonrpc string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
	ID      int             `json:"id"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewJSONRPCClient creates a new JSON-RPC client.
func NewJSONRPCClient(stdin io.WriteCloser, stdout io.Reader) *JSONRPCClient {
	return &JSONRPCClient{
		stdin:         stdin,
		stdout:        stdout,
		pending:       make(map[int]*pendingRequest),
		streamHandlers: make(map[string]chan []byte),
	}
}

// Call sends a request and waits for single response.
func (c *JSONRPCClient) Call(ctx context.Context, method string, params any) ([]byte, error) {
	c.mu.Lock()
	c.requestID++
	id := c.requestID
	pending := &pendingRequest{
		response: make(chan *jsonRPCResponse, 1),
	}
	c.pending[id] = pending
	c.mu.Unlock()

	// Send request
	req := jsonRPCRequest{
		Jsonrpc: "2.0",
		Method:  method,
		Params:  params,
		ID:      id,
	}

	data, err := json.Marshal(req)
	if err != nil {
		c.cleanup(id)
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	data = append(data, '\n')

	c.mu.Lock()
	_, err = c.stdin.Write(data)
	c.mu.Unlock()

	if err != nil {
		c.cleanup(id)
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	// Wait for response
	select {
	case resp := <-pending.response:
		c.cleanup(id)
		if resp.Error != nil {
			return nil, fmt.Errorf("RPC error [%d]: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-time.After(30 * time.Second):
		c.cleanup(id)
		return nil, fmt.Errorf("request timeout")
	case <-ctx.Done():
		c.cleanup(id)
		return nil, ctx.Err()
	}
}

// Stream sends a stream request (ID=0) and returns event channel.
func (c *JSONRPCClient) Stream(ctx context.Context, method string, params any) (chan []byte, error) {
	// Create event channel
	eventChan := make(chan []byte, 100)

	// Generate stream ID for tracking
	streamID := method + "-" + fmt.Sprintf("%d", time.Now().UnixNano())
	c.streamMu.Lock()
	c.streamHandlers[streamID] = eventChan
	c.streamMu.Unlock()

	// Send stream request (ID=0)
	req := jsonRPCRequest{
		Jsonrpc: "2.0",
		Method:  method,
		Params:  params,
		ID:      0, // Stream request
	}

	data, err := json.Marshal(req)
	if err != nil {
		c.streamMu.Lock()
		delete(c.streamHandlers, streamID)
		c.streamMu.Unlock()
		close(eventChan)
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	data = append(data, '\n')

	c.mu.Lock()
	_, err = c.stdin.Write(data)
	c.mu.Unlock()

	if err != nil {
		c.streamMu.Lock()
		delete(c.streamHandlers, streamID)
		c.streamMu.Unlock()
		close(eventChan)
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	// Start goroutine to handle stream completion
	go func() {
		<-ctx.Done()
		c.streamMu.Lock()
		if ch, exists := c.streamHandlers[streamID]; exists {
			delete(c.streamHandlers, streamID)
			close(ch)
		}
		c.streamMu.Unlock()
	}()

	return eventChan, nil
}

// ReadResponses continuously reads responses from stdout.
func (c *JSONRPCClient) ReadResponses() {
	decoder := json.NewDecoder(c.stdout)

	for {
		var resp jsonRPCResponse
		if err := decoder.Decode(&resp); err != nil {
			if err == io.EOF {
				// Process exited, close all pending
				c.mu.Lock()
				for _, pending := range c.pending {
					pending.response <- &jsonRPCResponse{
						Error: &jsonRPCError{Code: -1, Message: "process exited"},
					}
				}
				c.pending = make(map[int]*pendingRequest)
				c.mu.Unlock()

				// Close all stream handlers
				c.streamMu.Lock()
				for _, ch := range c.streamHandlers {
					close(ch)
				}
				c.streamHandlers = make(map[string]chan []byte)
				c.streamMu.Unlock()
				return
			}
			continue
		}

		// Handle response based on ID
		if resp.ID > 0 {
			// Non-stream response
			c.mu.Lock()
			pending := c.pending[resp.ID]
			c.mu.Unlock()

			if pending != nil {
				pending.response <- &resp
			}
		} else if resp.ID == 0 {
			// Stream event - broadcast to all stream handlers
			c.streamMu.RLock()
			for _, ch := range c.streamHandlers {
				select {
				case ch <- resp.Result:
				default:
					// Channel full, skip
				}
			}
			c.streamMu.RUnlock()

			// Check for completion event
			var eventType struct {
				Type string `json:"type"`
			}
			json.Unmarshal(resp.Result, &eventType)
			if eventType.Type == "complete" || eventType.Type == "error" {
				c.streamMu.Lock()
				for streamID, ch := range c.streamHandlers {
					close(ch)
					delete(c.streamHandlers, streamID)
				}
				c.streamMu.Unlock()
			}
		}
	}
}

// cleanup removes a pending request.
func (c *JSONRPCClient) cleanup(id int) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// Close closes the client.
func (c *JSONRPCClient) Close() error {
	return c.stdin.Close()
}