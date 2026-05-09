// Package scheduler provides scheduling algorithms for AIAgent to AgentRuntime placement.
package scheduler

import (
	"context"
	"fmt"
	"sort"

	"aiagent/api/v1"
)

// Scheduler interface defines the scheduling behavior for AIAgent placement.
type Scheduler interface {
	// Schedule selects the best AgentRuntime for an AIAgent.
	Schedule(ctx context.Context, agent *v1.AIAgent, runtimes []*v1.AgentRuntime) (*v1.AgentRuntime, error)

	// Score calculates a score for a runtime (higher = better match).
	Score(ctx context.Context, agent *v1.AIAgent, runtime *v1.AgentRuntime) (int, error)

	// CanSchedule checks if an agent can be scheduled to a runtime.
	CanSchedule(ctx context.Context, agent *v1.AIAgent, runtime *v1.AgentRuntime) bool
}

// DefaultScheduler implements basic scheduling logic.
type DefaultScheduler struct {
	Strategy ScheduleStrategy
}

// ScheduleStrategy defines the scheduling strategy.
type ScheduleStrategy string

const (
	// StrategyBinPack favors runtimes with more agents (pack tightly).
	StrategyBinPack ScheduleStrategy = "binpack"

	// StrategySpread favors runtimes with fewer agents (spread evenly).
	StrategySpread ScheduleStrategy = "spread"

	// StrategyFirstFit picks the first suitable runtime.
	StrategyFirstFit ScheduleStrategy = "firstfit"
)

// NewDefaultScheduler creates a new DefaultScheduler with binpack strategy.
func NewDefaultScheduler() *DefaultScheduler {
	return &DefaultScheduler{
		Strategy: StrategyBinPack,
	}
}

// NewSchedulerWithStrategy creates a scheduler with specified strategy.
func NewSchedulerWithStrategy(strategy ScheduleStrategy) *DefaultScheduler {
	return &DefaultScheduler{
		Strategy: strategy,
	}
}

// Schedule selects the best AgentRuntime for the given AIAgent.
func (s *DefaultScheduler) Schedule(ctx context.Context, agent *v1.AIAgent, runtimes []*v1.AgentRuntime) (*v1.AgentRuntime, error) {
	if len(runtimes) == 0 {
		return nil, fmt.Errorf("no runtimes available for scheduling")
	}

	// Filter schedulable runtimes
	candidates := []*v1.AgentRuntime{}
	for _, rt := range runtimes {
		if s.CanSchedule(ctx, agent, rt) {
			candidates = append(candidates, rt)
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no suitable runtime found for agent %s", agent.Name)
	}

	// Score candidates
	scores := make(map[string]int)
	for _, rt := range candidates {
		score, err := s.Score(ctx, agent, rt)
		if err != nil {
			continue
		}
		scores[rt.Name] = score
	}

	// Sort by score based on strategy
	sort.Slice(candidates, func(i, j int) bool {
		scoreI := scores[candidates[i].Name]
		scoreJ := scores[candidates[j].Name]

		switch s.Strategy {
		case StrategyBinPack:
			// Higher score = more agents = better for binpack
			return scoreI > scoreJ
		case StrategySpread:
			// Lower score = fewer agents = better for spread
			return scoreI < scoreJ
		case StrategyFirstFit:
			// First fit: just return first
			return i < j
		default:
			return scoreI > scoreJ
		}
	})

	return candidates[0], nil
}

// Score calculates a scheduling score for a runtime.
// Higher score = runtime has more capacity (for binpack).
func (s *DefaultScheduler) Score(ctx context.Context, agent *v1.AIAgent, runtime *v1.AgentRuntime) (int, error) {
	// Base score from agent count (proxy for utilization)
	score := int(runtime.Status.AgentCount)

	// Add score for matching framework type
	if agent.Spec.RuntimeRef.Type != "" {
		if runtime.Spec.AgentFramework.Type == agent.Spec.RuntimeRef.Type {
			score += 100 // Strong match bonus
		} else {
			score -= 50 // Type mismatch penalty
		}
	}

	// Add score for runtime health
	if runtime.Status.Phase == v1.RuntimePhaseRunning {
		score += 50
	} else {
		score -= 100 // Not running penalty
	}

	// Add score for capacity (ready replicas)
	score += int(runtime.Status.ReadyReplicas * 10)

	return score, nil
}

// CanSchedule checks if an agent can be scheduled to a runtime.
func (s *DefaultScheduler) CanSchedule(ctx context.Context, agent *v1.AIAgent, runtime *v1.AgentRuntime) bool {
	// Runtime must be running
	if runtime.Status.Phase != v1.RuntimePhaseRunning {
		return false
	}

	// Namespace must match (or cross-namespace scheduling allowed)
	if runtime.Namespace != agent.Namespace {
		// TODO: Check cross-namespace scheduling policy
		return false
	}

	// Framework type must match if specified
	if agent.Spec.RuntimeRef.Type != "" && runtime.Spec.AgentFramework.Type != agent.Spec.RuntimeRef.Type {
		return false
	}

	// Runtime must have capacity
	// TODO: Check actual resource limits vs. current usage

	return true
}

// MigrationScheduler handles agent migration between runtimes.
type MigrationScheduler struct {
	DefaultScheduler
}

// NewMigrationScheduler creates a scheduler for migration scenarios.
func NewMigrationScheduler() *MigrationScheduler {
	return &MigrationScheduler{
		DefaultScheduler: DefaultScheduler{
			Strategy: StrategySpread, // Spread for migration to avoid overloading
		},
	}
}

// Migrate performs migration of an agent from old runtime to new runtime.
// Returns the target runtime and migration plan.
func (s *MigrationScheduler) Migrate(ctx context.Context, agent *v1.AIAgent, oldRuntime *v1.AgentRuntime, candidates []*v1.AgentRuntime) (*v1.AgentRuntime, *MigrationPlan, error) {
	// Filter candidates (exclude old runtime)
	newCandidates := []*v1.AgentRuntime{}
	for _, rt := range candidates {
		if rt.Name != oldRuntime.Name {
			newCandidates = append(newCandidates, rt)
		}
	}

	if len(newCandidates) == 0 {
		return nil, nil, fmt.Errorf("no alternative runtimes available for migration")
	}

	// Select target
	target, err := s.Schedule(ctx, agent, newCandidates)
	if err != nil {
		return nil, nil, err
	}

	// Create migration plan
	plan := &MigrationPlan{
		AgentName:     agent.Name,
		AgentNamespace: agent.Namespace,
		SourceRuntime: oldRuntime.Name,
		TargetRuntime: target.Name,
		Phase:         MigrationPhasePending,
		Steps: []MigrationStep{
			{Phase: MigrationPhasePrepare, Description: "Prepare agent data for migration"},
			{Phase: MigrationPhaseTransfer, Description: "Transfer agent state to target runtime"},
			{Phase: MigrationPhaseActivate, Description: "Activate agent on target runtime"},
			{Phase: MigrationPhaseCleanup, Description: "Cleanup agent on source runtime"},
		},
	}

	return target, plan, nil
}

// MigrationPlan describes the migration process.
type MigrationPlan struct {
	AgentName      string          `json:"agentName"`
	AgentNamespace string          `json:"agentNamespace"`
	SourceRuntime  string          `json:"sourceRuntime"`
	TargetRuntime  string          `json:"targetRuntime"`
	Phase          MigrationPhase  `json:"phase"`
	Steps          []MigrationStep `json:"steps"`
	StartTime      string          `json:"startTime,omitempty"`
	EndTime        string          `json:"endTime,omitempty"`
}

// MigrationPhase represents the migration phase.
type MigrationPhase string

const (
	MigrationPhasePending   MigrationPhase = "Pending"
	MigrationPhasePrepare   MigrationPhase = "Prepare"
	MigrationPhaseTransfer  MigrationPhase = "Transfer"
	MigrationPhaseActivate  MigrationPhase = "Activate"
	MigrationPhaseCleanup   MigrationPhase = "Cleanup"
	MigrationPhaseCompleted MigrationPhase = "Completed"
	MigrationPhaseFailed    MigrationPhase = "Failed"
)

// MigrationStep represents a step in the migration process.
type MigrationStep struct {
	Phase        MigrationPhase `json:"phase"`
	Description  string         `json:"description"`
	Status       string         `json:"status,omitempty"`
	StartTime    string         `json:"startTime,omitempty"`
	EndTime      string         `json:"endTime,omitempty"`
}