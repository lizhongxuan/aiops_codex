package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
)

type NotificationHandler func(method string, params json.RawMessage)
type ServerRequestHandler func(id json.RawMessage, method string, params json.RawMessage)

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type pendingResponse struct {
	result json.RawMessage
	err    *rpcError
}

type Client struct {
	path            string
	onNotification  NotificationHandler
	onServerRequest ServerRequestHandler
	nextID          atomic.Int64
	cmd             *exec.Cmd
	stdin           io.WriteCloser
	writeMu         sync.Mutex
	pendingMu       sync.Mutex
	pending         map[string]chan pendingResponse
	startOnce       sync.Once
	startErr        error
	closed          chan struct{}
	statusMu        sync.RWMutex
	alive           bool
	lastExitError   string
}

func New(path string, onNotification NotificationHandler, onServerRequest ServerRequestHandler) *Client {
	return &Client{
		path:            path,
		onNotification:  onNotification,
		onServerRequest: onServerRequest,
		pending:         make(map[string]chan pendingResponse),
		closed:          make(chan struct{}),
	}
}

func (c *Client) Start(ctx context.Context) error {
	c.startOnce.Do(func() {
		cmd := exec.CommandContext(ctx, c.path, "app-server")
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			c.startErr = err
			return
		}
		stdin, err := cmd.StdinPipe()
		if err != nil {
			c.startErr = err
			return
		}
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			c.startErr = err
			return
		}

		c.cmd = cmd
		c.stdin = stdin
		c.setAlive(true)

		go c.readLoop(stdout)
		go func() {
			err := cmd.Wait()
			if err != nil {
				log.Printf("codex app-server exited with error: %v", err)
				c.setLastExitError(err.Error())
			} else {
				log.Printf("codex app-server exited")
				c.setLastExitError("")
			}
			c.setAlive(false)
			close(c.closed)
		}()

		var initResp map[string]any
		if err := c.Request(ctx, "initialize", map[string]any{
			"clientInfo": map[string]any{
				"name":    "aiops-codex-web",
				"title":   "AIOps Codex Web",
				"version": "0.1.0",
			},
			"capabilities": map[string]any{
				"experimentalApi": true,
			},
		}, &initResp); err != nil {
			c.startErr = err
			return
		}
		if err := c.Notify(ctx, "initialized", map[string]any{}); err != nil {
			c.startErr = err
			return
		}
	})

	return c.startErr
}

func (c *Client) Request(ctx context.Context, method string, params any, result any) error {
	id := c.nextID.Add(1)
	key := fmt.Sprintf("%d", id)
	ch := make(chan pendingResponse, 1)

	c.pendingMu.Lock()
	c.pending[key] = ch
	c.pendingMu.Unlock()

	if err := c.send(map[string]any{
		"id":     id,
		"method": method,
		"params": params,
	}); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, key)
		c.pendingMu.Unlock()
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return errors.New("codex app-server exited")
	case resp := <-ch:
		if resp.err != nil {
			return fmt.Errorf("%s: %s", method, resp.err.Message)
		}
		if result == nil || len(resp.result) == 0 {
			return nil
		}
		return json.Unmarshal(resp.result, result)
	}
}

func (c *Client) Notify(_ context.Context, method string, params any) error {
	return c.send(map[string]any{
		"method": method,
		"params": params,
	})
}

func (c *Client) Respond(_ context.Context, rawID string, result any) error {
	return c.send(map[string]any{
		"id":     json.RawMessage(rawID),
		"result": result,
	})
}

func (c *Client) RespondError(_ context.Context, rawID string, code int, message string) error {
	return c.send(map[string]any{
		"id": json.RawMessage(rawID),
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func (c *Client) Close() error {
	if c.cmd != nil && c.cmd.Process != nil {
		return c.cmd.Process.Kill()
	}
	return nil
}

func (c *Client) Alive() bool {
	c.statusMu.RLock()
	defer c.statusMu.RUnlock()
	return c.alive
}

func (c *Client) LastExitError() string {
	c.statusMu.RLock()
	defer c.statusMu.RUnlock()
	return c.lastExitError
}

func (c *Client) PendingCount() int {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	return len(c.pending)
}

func (c *Client) setAlive(alive bool) {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	c.alive = alive
}

func (c *Client) setLastExitError(err string) {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	c.lastExitError = err
}

func (c *Client) send(message any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.stdin == nil {
		return errors.New("codex app-server stdin not ready")
	}
	_, err = c.stdin.Write(append(payload, '\n'))
	return err
}

func (c *Client) readLoop(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		switch {
		case len(msg.ID) > 0 && msg.Method == "":
			key := string(msg.ID)
			c.pendingMu.Lock()
			ch := c.pending[key]
			delete(c.pending, key)
			c.pendingMu.Unlock()
			if ch != nil {
				ch <- pendingResponse{result: msg.Result, err: msg.Error}
			}
		case len(msg.ID) > 0 && msg.Method != "":
			if c.onServerRequest != nil {
				c.onServerRequest(msg.ID, msg.Method, msg.Params)
			}
		case msg.Method != "":
			if c.onNotification != nil {
				c.onNotification(msg.Method, msg.Params)
			}
		}
	}
}
