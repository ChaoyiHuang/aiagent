// Package base provides shared functionality for all framework handlers.
// This package contains common implementations for:
// - Framework process execution (stdin/stdout JSON-RPC)
// - Process lifecycle management
// - JSON-RPC client
// - Configuration management
package base

import (
	"context"
	"fmt"
	"sync"
	"time"

	"aiagent/pkg/handler"
)

// FrameworkExecutor manages framework process execution.
// Handler is the parent process, Framework is the child process.
// Communication via stdin/stdout JSON-RPC (no gRPC/HTTP server needed).
type FrameworkExecutor struct {
	frameworkBin string
	workDir      string

	processes map[string]*FrameworkProcess
	mu        sync.RWMutex
}

// NewFrameworkExecutor creates a new framework executor.
func NewFrameworkExecutor(frameworkBin string, workDir string) *FrameworkExecutor {
	return &FrameworkExecutor{
		frameworkBin: frameworkBin,
		workDir:      workDir,
		processes:    make(map[string]*FrameworkProcess),
	}
}

// StartProcess starts a framework process as a child process.
// Handler is the parent, Framework is the child.
func (e *FrameworkExecutor) StartProcess(ctx context.Context, agentID string, args []string) (*FrameworkProcess, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if process already exists
	if _, exists := e.processes[agentID]; exists {
		return nil, fmt.Errorf("process for agent %s already running", agentID)
	}

	process := NewFrameworkProcess(agentID, e.frameworkBin, args, e.workDir)

	if err := process.Start(ctx); err != nil {
		return nil, err
	}

	e.processes[agentID] = process
	return process, nil
}

// StopProcess stops a framework process.
func (e *FrameworkExecutor) StopProcess(ctx context.Context, agentID string) error {
	e.mu.Lock()
	process := e.processes[agentID]
	delete(e.processes, agentID)
	e.mu.Unlock()

	if process == nil {
		return fmt.Errorf("process %s not found", agentID)
	}

	return process.Stop(ctx)
}

// GetProcess retrieves a framework process.
func (e *FrameworkExecutor) GetProcess(agentID string) *FrameworkProcess {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.processes[agentID]
}

// ListProcesses returns all running processes.
func (e *FrameworkExecutor) ListProcesses() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ids := make([]string, 0, len(e.processes))
	for id := range e.processes {
		ids = append(ids, id)
	}
	return ids
}

// WorkDir returns the working directory path.
func (e *FrameworkExecutor) WorkDir() string {
	return e.workDir
}

// FrameworkBin returns the framework binary path.
func (e *FrameworkExecutor) FrameworkBin() string {
	return e.frameworkBin
}

// StopAll stops all running processes.
func (e *FrameworkExecutor) StopAll(ctx context.Context) error {
	e.mu.RLock()
	processes := make([]*FrameworkProcess, 0, len(e.processes))
	for _, p := range e.processes {
		processes = append(processes, p)
	}
	e.mu.RUnlock()

	for _, p := range processes {
		if err := p.Stop(ctx); err != nil {
			return err
		}
	}

	e.mu.Lock()
	e.processes = make(map[string]*FrameworkProcess)
	e.mu.Unlock()

	return nil
}

// ProcessStatus represents process status.
type ProcessStatus struct {
	AgentID  string
	PID      int
	Running  bool
	StartedAt time.Time
	Metrics  *handler.AgentMetrics
}

// GetProcessStatus returns status of a process.
func (e *FrameworkExecutor) GetProcessStatus(agentID string) (*ProcessStatus, error) {
	process := e.GetProcess(agentID)
	if process == nil {
		return nil, fmt.Errorf("process %s not found", agentID)
	}

	return &ProcessStatus{
		AgentID:  agentID,
		PID:      process.PID(),
		Running:  process.IsRunning(),
		StartedAt: process.StartedAt(),
		Metrics:  process.GetMetrics(),
	}, nil
}