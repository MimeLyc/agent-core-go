package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// Transport defines the interface for MCP communication.
type Transport interface {
	// Send sends a request and waits for a response.
	Send(ctx context.Context, req Request) (Response, error)

	// Notify sends a notification (no response expected).
	Notify(ctx context.Context, notif Notification) error

	// Close closes the transport.
	Close() error
}

// StdioTransport implements Transport using stdio (stdin/stdout).
type StdioTransport struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	reader   *bufio.Reader
	mu       sync.Mutex
	closed   bool
	pending  map[any]chan Response
	pendingMu sync.Mutex
	nextID   int
}

// NewStdioTransport creates a new stdio transport for an MCP server.
func NewStdioTransport(command string, args []string, env []string, workDir string) (*StdioTransport, error) {
	cmd := exec.Command(command, args...)
	cmd.Dir = workDir
	if len(env) > 0 {
		cmd.Env = env
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("failed to start MCP server: %w", err)
	}

	t := &StdioTransport{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		reader:  bufio.NewReader(stdout),
		pending: make(map[any]chan Response),
		nextID:  1,
	}

	// Start response reader
	go t.readResponses()

	return t, nil
}

// Send sends a request and waits for a response.
func (t *StdioTransport) Send(ctx context.Context, req Request) (Response, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return Response{}, fmt.Errorf("transport closed")
	}

	// Assign ID if not set
	if req.ID == nil {
		req.ID = t.nextID
		t.nextID++
	}

	// Create response channel
	respCh := make(chan Response, 1)
	t.pendingMu.Lock()
	t.pending[req.ID] = respCh
	t.pendingMu.Unlock()

	// Send request
	data, err := json.Marshal(req)
	if err != nil {
		t.mu.Unlock()
		t.removePending(req.ID)
		return Response{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	_, err = t.stdin.Write(append(data, '\n'))
	t.mu.Unlock()

	if err != nil {
		t.removePending(req.ID)
		return Response{}, fmt.Errorf("failed to send request: %w", err)
	}

	// Wait for response
	select {
	case resp := <-respCh:
		return resp, nil
	case <-ctx.Done():
		t.removePending(req.ID)
		return Response{}, ctx.Err()
	}
}

// Notify sends a notification (no response expected).
func (t *StdioTransport) Notify(ctx context.Context, notif Notification) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return fmt.Errorf("transport closed")
	}

	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	_, err = t.stdin.Write(append(data, '\n'))
	if err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}

	return nil
}

// Close closes the transport and kills the server process.
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true

	t.stdin.Close()
	t.stdout.Close()

	// Kill the process
	if t.cmd.Process != nil {
		t.cmd.Process.Kill()
	}

	return t.cmd.Wait()
}

// readResponses reads responses from stdout and dispatches them.
func (t *StdioTransport) readResponses() {
	for {
		line, err := t.reader.ReadBytes('\n')
		if err != nil {
			return
		}

		var resp Response
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}

		// Find and dispatch to pending request
		t.pendingMu.Lock()
		ch, ok := t.pending[resp.ID]
		if ok {
			delete(t.pending, resp.ID)
		}
		t.pendingMu.Unlock()

		if ok && ch != nil {
			ch <- resp
			close(ch)
		}
	}
}

func (t *StdioTransport) removePending(id any) {
	t.pendingMu.Lock()
	delete(t.pending, id)
	t.pendingMu.Unlock()
}
