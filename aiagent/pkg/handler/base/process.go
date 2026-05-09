package base

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"aiagent/pkg/handler"
)

// FrameworkProcess represents a running framework process.
// Handler is the parent process, Framework is the child.
type FrameworkProcess struct {
	agentID      string
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       io.Reader
	args         []string
	workDir      string

	startedAt    time.Time
	rpcClient    *JSONRPCClient

	metrics      *handler.AgentMetrics
	mu           sync.RWMutex
}

// NewFrameworkProcess creates a new framework process instance.
func NewFrameworkProcess(agentID string, frameworkBin string, args []string, workDir string) *FrameworkProcess {
	fullArgs := append([]string{}, args...)

	cmd := exec.Command(frameworkBin, fullArgs...)
	cmd.Dir = workDir

	return &FrameworkProcess{
		agentID: agentID,
		cmd:     cmd,
		args:    fullArgs,
		workDir: workDir,
		metrics: &handler.AgentMetrics{},
	}
}

// Start starts the framework process.
func (p *FrameworkProcess) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Create stdin/stdout pipes
	stdin, err := p.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	p.stdin = stdin

	stdout, err := p.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	p.stdout = stdout

	// Start the process
	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start framework process: %w", err)
	}

	p.startedAt = time.Now()
	p.rpcClient = NewJSONRPCClient(stdin, stdout)

	// Start response reader
	go p.rpcClient.ReadResponses()

	return nil
}

// Stop stops the framework process.
func (p *FrameworkProcess) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Close stdin to signal shutdown
	if p.stdin != nil {
		p.stdin.Close()
	}

	// Wait for process to exit
	done := make(chan error, 1)
	go func() {
		done <- p.cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		// Force kill if doesn't exit gracefully
		if p.cmd.Process != nil {
			p.cmd.Process.Kill()
		}
		return p.cmd.Wait()
	case <-ctx.Done():
		if p.cmd.Process != nil {
			p.cmd.Process.Kill()
		}
		return ctx.Err()
	}
}

// IsRunning checks if process is running.
func (p *FrameworkProcess) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cmd.Process != nil && p.cmd.ProcessState == nil
}

// PID returns the process ID.
func (p *FrameworkProcess) PID() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.cmd.Process == nil {
		return -1
	}
	return p.cmd.Process.Pid
}

// StartedAt returns the start time.
func (p *FrameworkProcess) StartedAt() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.startedAt
}

// Call sends a JSON-RPC request and waits for response.
func (p *FrameworkProcess) Call(ctx context.Context, method string, params any) ([]byte, error) {
	p.mu.RLock()
	rpcClient := p.rpcClient
	p.mu.RUnlock()

	if rpcClient == nil {
		return nil, fmt.Errorf("process not started")
	}

	p.metrics.TotalInvocations++
	result, err := rpcClient.Call(ctx, method, params)
	if err != nil {
		p.metrics.FailedInvocations++
		return nil, err
	}

	p.metrics.SuccessfulInvocations++
	return result, nil
}

// Stream sends a streaming request and returns event channel.
func (p *FrameworkProcess) Stream(ctx context.Context, method string, params any) (chan []byte, error) {
	p.mu.RLock()
	rpcClient := p.rpcClient
	p.mu.RUnlock()

	if rpcClient == nil {
		return nil, fmt.Errorf("process not started")
	}

	p.metrics.TotalInvocations++
	return rpcClient.Stream(ctx, method, params)
}

// GetMetrics returns process metrics.
func (p *FrameworkProcess) GetMetrics() *handler.AgentMetrics {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.metrics
}

// AgentID returns the agent ID.
func (p *FrameworkProcess) AgentID() string {
	return p.agentID
}

// Args returns the process arguments.
func (p *FrameworkProcess) Args() []string {
	return p.args
}

// WorkDir returns the working directory.
func (p *FrameworkProcess) WorkDir() string {
	return p.workDir
}