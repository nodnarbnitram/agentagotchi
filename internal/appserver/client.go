package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Client struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	pending sync.Map
	writeMu sync.Mutex
	nextID  atomic.Int64
	done    chan struct{}
}

type response struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type ThreadInfo struct {
	ID            string
	Title         string
	RuntimeStatus string
	Failed        bool
}

type threadReadParams struct {
	ThreadID     string `json:"threadId"`
	IncludeTurns bool   `json:"includeTurns"`
}

type threadReadEnvelope struct {
	Thread struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status struct {
			Type        string   `json:"type"`
			ActiveFlags []string `json:"activeFlags"`
		} `json:"status"`
	} `json:"thread"`
}

func Start(ctx context.Context, binary string) (*Client, error) {
	if binary == "" {
		binary = locateCodex()
	}
	if binary == "" {
		return nil, errors.New("codex executable not found")
	}
	cmd := exec.CommandContext(ctx, binary, "app-server", "--listen", "stdio://")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// App Server diagnostics may include local paths or other task-adjacent
	// context. The bridge exposes its own content-free availability messages,
	// so discard subprocess stderr rather than writing it to bridge.log.
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &Client{cmd: cmd, stdin: stdin, done: make(chan struct{})}
	c.nextID.Store(10)
	go c.readLoop(stdout)

	var initResult json.RawMessage
	if err := c.call(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name": "codex_pet_bridge", "title": "Codex Pet Bridge", "version": "0.1.0",
		},
	}, &initResult); err != nil {
		c.Close()
		return nil, fmt.Errorf("initialize app server: %w", err)
	}
	if err := c.notify("initialized", map[string]any{}); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) ReadThread(ctx context.Context, id string) (ThreadInfo, error) {
	var envelope threadReadEnvelope
	if err := c.call(ctx, "thread/read", threadReadParams{
		ThreadID: id, IncludeTurns: false,
	}, &envelope); err != nil {
		return ThreadInfo{}, err
	}
	return threadInfo(envelope), nil
}

func threadInfo(envelope threadReadEnvelope) ThreadInfo {
	info := ThreadInfo{
		ID:            envelope.Thread.ID,
		Title:         envelope.Thread.Name,
		RuntimeStatus: envelope.Thread.Status.Type,
		Failed:        envelope.Thread.Status.Type == "systemError",
	}
	if len(envelope.Thread.Status.ActiveFlags) > 0 {
		info.RuntimeStatus += ":" + strings.Join(envelope.Thread.Status.ActiveFlags, ",")
	}
	return info
}

func (c *Client) Close() error {
	select {
	case <-c.done:
	default:
		_ = c.stdin.Close()
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
	}
	return nil
}

func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	id := c.nextID.Add(1)
	ch := make(chan response, 1)
	c.pending.Store(id, ch)
	defer c.pending.Delete(id)
	if err := c.send(map[string]any{"method": method, "id": id, "params": params}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return errors.New("app server stopped")
	case msg := <-ch:
		if msg.Error != nil {
			return fmt.Errorf("app server error %d: %s", msg.Error.Code, msg.Error.Message)
		}
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(msg.Result, out); err != nil {
			return fmt.Errorf("decode %s response: %w", method, err)
		}
		return nil
	}
}

func (c *Client) notify(method string, params any) error {
	return c.send(map[string]any{"method": method, "params": params})
}

func (c *Client) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	b = append(b, '\n')
	_, err = c.stdin.Write(b)
	return err
}

func (c *Client) readLoop(r io.Reader) {
	defer close(c.done)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 2<<20)
	for scanner.Scan() {
		var msg response
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil || msg.ID == 0 {
			continue
		}
		if value, ok := c.pending.Load(msg.ID); ok {
			value.(chan response) <- msg
		}
	}
}

func locateCodex() string {
	if v := os.Getenv("CODEX_PET_CODEX_BIN"); v != "" {
		return v
	}
	candidates := []string{
		"/Applications/ChatGPT.app/Contents/Resources/codex",
		"/Applications/Codex.app/Contents/Resources/codex",
	}
	for _, candidate := range candidates {
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
			return candidate
		}
	}
	if v, err := exec.LookPath("codex"); err == nil {
		return v
	}
	return ""
}

// BinaryForDiagnostics returns a non-sensitive, display-friendly binary name.
func BinaryForDiagnostics(binary string) string {
	if binary == "" {
		binary = locateCodex()
	}
	return filepath.Base(binary)
}

func Poll(ctx context.Context, c *Client, ids func() []string, onInfo func(ThreadInfo)) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		for _, id := range ids() {
			callCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			info, err := c.ReadThread(callCtx, id)
			cancel()
			if err == nil {
				onInfo(info)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
