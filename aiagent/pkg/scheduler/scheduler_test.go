// Package scheduler provides scheduling algorithms for AIAgent to AgentRuntime placement.
package scheduler

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"aiagent/api/v1"
)

// Helper function to create a test AIAgent
func newTestAIAgent(name, namespace, runtimeType string) *v1.AIAgent {
	return &v1.AIAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1.AIAgentSpec{
			RuntimeRef: v1.RuntimeReference{
				Type: runtimeType,
			},
		},
	}
}

// Helper function to create a test AgentRuntime
func newTestAgentRuntime(name, namespace, frameworkType string, phase v1.RuntimePhase, readyReplicas int32, agentCount int32) *v1.AgentRuntime {
	return &v1.AgentRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1.AgentRuntimeSpec{
			AgentFramework: v1.AgentFrameworkSpec{
				Type: frameworkType,
			},
		},
		Status: v1.AgentRuntimeStatus{
			Phase:         phase,
			ReadyReplicas: readyReplicas,
			AgentCount:    agentCount,
		},
	}
}

func TestDefaultScheduler_Schedule(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		agent       *v1.AIAgent
		runtimes    []*v1.AgentRuntime
		strategy    ScheduleStrategy
		expectError bool
		expectName  string
	}{
		{
			name:     "no runtimes available",
			agent:    newTestAIAgent("agent1", "default", "adk-go"),
			runtimes: []*v1.AgentRuntime{},
			strategy: StrategyBinPack,
			expectError: true,
		},
		{
			name:     "single suitable runtime",
			agent:    newTestAIAgent("agent1", "default", "adk-go"),
			runtimes: []*v1.AgentRuntime{
				newTestAgentRuntime("runtime1", "default", "adk-go", v1.RuntimePhaseRunning, 1, 5),
			},
			strategy:   StrategyBinPack,
			expectError: false,
			expectName: "runtime1",
		},
		{
			name:     "multiple runtimes binpack picks highest score",
			agent:    newTestAIAgent("agent1", "default", "adk-go"),
			runtimes: []*v1.AgentRuntime{
				newTestAgentRuntime("runtime1", "default", "adk-go", v1.RuntimePhaseRunning, 2, 3),  // Score: 3+100+50+20=173
				newTestAgentRuntime("runtime2", "default", "adk-go", v1.RuntimePhaseRunning, 1, 10), // Score: 10+100+50+10=170
			},
			strategy:   StrategyBinPack,
			expectError: false,
			expectName: "runtime1", // runtime1 has higher score (173 vs 170) due to more ready replicas
		},
		{
			name:     "multiple runtimes spread picks lowest score",
			agent:    newTestAIAgent("agent1", "default", "adk-go"),
			runtimes: []*v1.AgentRuntime{
				newTestAgentRuntime("runtime1", "default", "adk-go", v1.RuntimePhaseRunning, 2, 10),
				newTestAgentRuntime("runtime2", "default", "adk-go", v1.RuntimePhaseRunning, 1, 3), // Fewer agents = lower score for spread
			},
			strategy:   StrategySpread,
			expectError: false,
			expectName: "runtime2", // runtime2 has fewer agents (3 vs 10), so picked by spread
		},
		{
			name:     "first fit picks first suitable",
			agent:    newTestAIAgent("agent1", "default", "adk-go"),
			runtimes: []*v1.AgentRuntime{
				newTestAgentRuntime("runtime1", "default", "adk-go", v1.RuntimePhaseRunning, 1, 100),
				newTestAgentRuntime("runtime2", "default", "adk-go", v1.RuntimePhaseRunning, 1, 1),
			},
			strategy:   StrategyFirstFit,
			expectError: false,
			expectName: "runtime1", // First fit returns first candidate
		},
		{
			name:     "filter out non-running runtimes",
			agent:    newTestAIAgent("agent1", "default", "adk-go"),
			runtimes: []*v1.AgentRuntime{
				newTestAgentRuntime("runtime1", "default", "adk-go", v1.RuntimePhaseFailed, 0, 0),
				newTestAgentRuntime("runtime2", "default", "adk-go", v1.RuntimePhaseRunning, 1, 5),
			},
			strategy:   StrategyBinPack,
			expectError: false,
			expectName: "runtime2", // Only runtime2 is running
		},
		{
			name:     "filter out wrong framework type",
			agent:    newTestAIAgent("agent1", "default", "adk-go"),
			runtimes: []*v1.AgentRuntime{
				newTestAgentRuntime("runtime1", "default", "openclaw", v1.RuntimePhaseRunning, 1, 5), // Wrong type
				newTestAgentRuntime("runtime2", "default", "adk-go", v1.RuntimePhaseRunning, 1, 5),
			},
			strategy:   StrategyBinPack,
			expectError: false,
			expectName: "runtime2", // Only runtime2 matches type
		},
		{
			name:     "filter out wrong namespace",
			agent:    newTestAIAgent("agent1", "default", "adk-go"),
			runtimes: []*v1.AgentRuntime{
				newTestAgentRuntime("runtime1", "other-ns", "adk-go", v1.RuntimePhaseRunning, 1, 5), // Wrong namespace
				newTestAgentRuntime("runtime2", "default", "adk-go", v1.RuntimePhaseRunning, 1, 5),
			},
			strategy:   StrategyBinPack,
			expectError: false,
			expectName: "runtime2",
		},
		{
			name:     "no suitable runtime after filtering",
			agent:    newTestAIAgent("agent1", "default", "adk-go"),
			runtimes: []*v1.AgentRuntime{
				newTestAgentRuntime("runtime1", "other-ns", "adk-go", v1.RuntimePhaseRunning, 1, 5),
				newTestAgentRuntime("runtime2", "default", "openclaw", v1.RuntimePhaseRunning, 1, 5),
			},
			strategy:    StrategyBinPack,
			expectError: true,
		},
		{
			name:     "agent with empty runtime type matches any framework",
			agent:    newTestAIAgent("agent1", "default", ""), // Empty type
			runtimes: []*v1.AgentRuntime{
				newTestAgentRuntime("runtime1", "default", "openclaw", v1.RuntimePhaseRunning, 1, 5),
			},
			strategy:   StrategyBinPack,
			expectError: false,
			expectName: "runtime1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheduler := NewSchedulerWithStrategy(tt.strategy)
			result, err := scheduler.Schedule(ctx, tt.agent, tt.runtimes)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result.Name != tt.expectName {
				t.Errorf("expected runtime %s, got %s", tt.expectName, result.Name)
			}
		})
	}
}

func TestDefaultScheduler_Score(t *testing.T) {
	ctx := context.Background()
	scheduler := NewDefaultScheduler()

	tests := []struct {
		name           string
		agent          *v1.AIAgent
		runtime        *v1.AgentRuntime
		expectedMin    int
		expectedMax    int
	}{
		{
			name:        "matching framework type and running",
			agent:       newTestAIAgent("agent1", "default", "adk-go"),
			runtime:     newTestAgentRuntime("runtime1", "default", "adk-go", v1.RuntimePhaseRunning, 2, 5),
			expectedMin: 150, // base 5 + type match 100 + running 50 + readyReplicas 20
			expectedMax: 200,
		},
		{
			name:        "non-matching framework type",
			agent:       newTestAIAgent("agent1", "default", "adk-go"),
			runtime:     newTestAgentRuntime("runtime1", "default", "openclaw", v1.RuntimePhaseRunning, 1, 5),
			// Score: 5 (agentCount) - 50 (type mismatch) + 50 (running) + 10 (readyReplicas) = 15
			expectedMin: 10,
			expectedMax: 20,
		},
		{
			name:        "not running runtime",
			agent:       newTestAIAgent("agent1", "default", "adk-go"),
			runtime:     newTestAgentRuntime("runtime1", "default", "adk-go", v1.RuntimePhaseFailed, 0, 5),
			expectedMin: -50, // base 5 + type match 100 + not running -100 + readyReplicas 0
			expectedMax: 50,
		},
		{
			name:        "agent with empty type no type penalty/bonus",
			agent:       newTestAIAgent("agent1", "default", ""),
			runtime:     newTestAgentRuntime("runtime1", "default", "adk-go", v1.RuntimePhaseRunning, 1, 5),
			expectedMin: 60, // base 5 + running 50 + readyReplicas 10 (no type adjustment)
			expectedMax: 70,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, err := scheduler.Score(ctx, tt.agent, tt.runtime)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if score < tt.expectedMin || score > tt.expectedMax {
				t.Errorf("expected score between %d and %d, got %d", tt.expectedMin, tt.expectedMax, score)
			}
		})
	}
}

func TestDefaultScheduler_CanSchedule(t *testing.T) {
	ctx := context.Background()
	scheduler := NewDefaultScheduler()

	tests := []struct {
		name     string
		agent    *v1.AIAgent
		runtime  *v1.AgentRuntime
		expected bool
	}{
		{
			name:     "can schedule - matching namespace and type, running",
			agent:    newTestAIAgent("agent1", "default", "adk-go"),
			runtime:  newTestAgentRuntime("runtime1", "default", "adk-go", v1.RuntimePhaseRunning, 1, 5),
			expected: true,
		},
		{
			name:     "cannot schedule - not running",
			agent:    newTestAIAgent("agent1", "default", "adk-go"),
			runtime:  newTestAgentRuntime("runtime1", "default", "adk-go", v1.RuntimePhasePending, 0, 0),
			expected: false,
		},
		{
			name:     "cannot schedule - wrong namespace",
			agent:    newTestAIAgent("agent1", "default", "adk-go"),
			runtime:  newTestAgentRuntime("runtime1", "other-ns", "adk-go", v1.RuntimePhaseRunning, 1, 5),
			expected: false,
		},
		{
			name:     "cannot schedule - wrong framework type",
			agent:    newTestAIAgent("agent1", "default", "adk-go"),
			runtime:  newTestAgentRuntime("runtime1", "default", "openclaw", v1.RuntimePhaseRunning, 1, 5),
			expected: false,
		},
		{
			name:     "can schedule - empty agent type matches any",
			agent:    newTestAIAgent("agent1", "default", ""),
			runtime:  newTestAgentRuntime("runtime1", "default", "openclaw", v1.RuntimePhaseRunning, 1, 5),
			expected: true,
		},
		{
			name:     "cannot schedule - empty runtime type with non-empty agent type",
			agent:    newTestAIAgent("agent1", "default", "adk-go"),
			runtime:  newTestAgentRuntime("runtime1", "default", "", v1.RuntimePhaseRunning, 1, 5),
			// Current behavior: empty runtime type does NOT match non-empty agent type
			// This is a limitation that could be improved in future
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scheduler.CanSchedule(ctx, tt.agent, tt.runtime)
			if result != tt.expected {
				t.Errorf("expected CanSchedule=%v, got %v", tt.expected, result)
			}
		})
	}
}

func TestNewDefaultScheduler(t *testing.T) {
	scheduler := NewDefaultScheduler()
	if scheduler.Strategy != StrategyBinPack {
		t.Errorf("expected default strategy to be binpack, got %s", scheduler.Strategy)
	}
}

func TestNewSchedulerWithStrategy(t *testing.T) {
	tests := []struct {
		strategy ScheduleStrategy
	}{
		{StrategyBinPack},
		{StrategySpread},
		{StrategyFirstFit},
		{"unknown"},
	}

	for _, tt := range tests {
		t.Run(string(tt.strategy), func(t *testing.T) {
			scheduler := NewSchedulerWithStrategy(tt.strategy)
			if scheduler.Strategy != tt.strategy {
				t.Errorf("expected strategy %s, got %s", tt.strategy, scheduler.Strategy)
			}
		})
	}
}

func TestMigrationScheduler_Migrate(t *testing.T) {
	ctx := context.Background()
	scheduler := NewMigrationScheduler()

	agent := newTestAIAgent("agent1", "default", "adk-go")
	oldRuntime := newTestAgentRuntime("old-runtime", "default", "adk-go", v1.RuntimePhaseRunning, 1, 10)
	candidates := []*v1.AgentRuntime{
		newTestAgentRuntime("runtime1", "default", "adk-go", v1.RuntimePhaseRunning, 1, 3),
		newTestAgentRuntime("runtime2", "default", "adk-go", v1.RuntimePhaseRunning, 2, 5),
	}

	target, plan, err := scheduler.Migrate(ctx, agent, oldRuntime, candidates)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
		return
	}

	// Target should be different from old runtime
	if target.Name == oldRuntime.Name {
		t.Errorf("target runtime should be different from old runtime")
	}

	// Plan should be valid
	if plan == nil {
		t.Errorf("expected migration plan, got nil")
		return
	}

	if plan.AgentName != agent.Name {
		t.Errorf("expected agent name %s, got %s", agent.Name, plan.AgentName)
	}

	if plan.SourceRuntime != oldRuntime.Name {
		t.Errorf("expected source runtime %s, got %s", oldRuntime.Name, plan.SourceRuntime)
	}

	if plan.TargetRuntime != target.Name {
		t.Errorf("expected target runtime %s, got %s", target.Name, plan.TargetRuntime)
	}

	if plan.Phase != MigrationPhasePending {
		t.Errorf("expected initial phase %s, got %s", MigrationPhasePending, plan.Phase)
	}

	if len(plan.Steps) != 4 {
		t.Errorf("expected 4 migration steps, got %d", len(plan.Steps))
	}
}

func TestMigrationScheduler_Migrate_NoAlternatives(t *testing.T) {
	ctx := context.Background()
	scheduler := NewMigrationScheduler()

	agent := newTestAIAgent("agent1", "default", "adk-go")
	oldRuntime := newTestAgentRuntime("old-runtime", "default", "adk-go", v1.RuntimePhaseRunning, 1, 10)
	candidates := []*v1.AgentRuntime{
		oldRuntime, // Only old runtime available
	}

	_, _, err := scheduler.Migrate(ctx, agent, oldRuntime, candidates)
	if err == nil {
		t.Errorf("expected error when no alternative runtimes available")
	}
}

func TestMigrationScheduler_Migrate_AllFiltered(t *testing.T) {
	ctx := context.Background()
	scheduler := NewMigrationScheduler()

	agent := newTestAIAgent("agent1", "default", "adk-go")
	oldRuntime := newTestAgentRuntime("old-runtime", "default", "adk-go", v1.RuntimePhaseRunning, 1, 10)
	candidates := []*v1.AgentRuntime{
		newTestAgentRuntime("runtime1", "other-ns", "adk-go", v1.RuntimePhaseRunning, 1, 5), // Wrong namespace
		newTestAgentRuntime("runtime2", "default", "openclaw", v1.RuntimePhaseRunning, 1, 5), // Wrong type
	}

	_, _, err := scheduler.Migrate(ctx, agent, oldRuntime, candidates)
	if err == nil {
		t.Errorf("expected error when all candidates are filtered")
	}
}

func TestMigrationScheduler_Strategy(t *testing.T) {
	scheduler := NewMigrationScheduler()
	// Migration scheduler should use spread strategy
	if scheduler.Strategy != StrategySpread {
		t.Errorf("expected migration scheduler to use spread strategy, got %s", scheduler.Strategy)
	}
}

func TestMigrationPlan(t *testing.T) {
	plan := &MigrationPlan{
		AgentName:      "test-agent",
		AgentNamespace: "default",
		SourceRuntime:  "runtime-old",
		TargetRuntime:  "runtime-new",
		Phase:          MigrationPhasePending,
		Steps: []MigrationStep{
			{Phase: MigrationPhasePrepare, Description: "Prepare"},
			{Phase: MigrationPhaseTransfer, Description: "Transfer"},
		},
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-01T00:01:00Z",
	}

	if plan.AgentName != "test-agent" {
		t.Errorf("unexpected agent name")
	}

	if len(plan.Steps) != 2 {
		t.Errorf("unexpected steps count")
	}
}

func TestMigrationPhases(t *testing.T) {
	phases := []MigrationPhase{
		MigrationPhasePending,
		MigrationPhasePrepare,
		MigrationPhaseTransfer,
		MigrationPhaseActivate,
		MigrationPhaseCleanup,
		MigrationPhaseCompleted,
		MigrationPhaseFailed,
	}

	// Verify all phases are defined
	for _, phase := range phases {
		if phase == "" {
			t.Errorf("migration phase should not be empty")
		}
	}
}