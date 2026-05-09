// Package integration provides integration tests for Scheduler.
package integration

import (
	"context"
	"fmt"
	"testing"

	"aiagent/api/v1"
	"aiagent/pkg/scheduler"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestScheduler_Integration tests the complete scheduling workflow.
func TestScheduler_Integration(t *testing.T) {
	ctx := context.Background()

	// Test Case 1: Create simulated runtime environment
	t.Log("Test Case 1: Create simulated runtime environment")

	runtimes := []*v1.AgentRuntime{
		// Runtime 1: High capacity, already has many agents
		{
			ObjectMeta: metav1.ObjectMeta{Name: "runtime-high-load", Namespace: "production"},
			Spec: v1.AgentRuntimeSpec{
				AgentFramework: v1.AgentFrameworkSpec{Type: "adk-go"},
				Replicas:       3,
			},
			Status: v1.AgentRuntimeStatus{
				Phase:         v1.RuntimePhaseRunning,
				ReadyReplicas: 3,
				AgentCount:    50,
			},
		},
		// Runtime 2: Medium capacity
		{
			ObjectMeta: metav1.ObjectMeta{Name: "runtime-medium", Namespace: "production"},
			Spec: v1.AgentRuntimeSpec{
				AgentFramework: v1.AgentFrameworkSpec{Type: "adk-go"},
				Replicas:       2,
			},
			Status: v1.AgentRuntimeStatus{
				Phase:         v1.RuntimePhaseRunning,
				ReadyReplicas: 2,
				AgentCount:    20,
			},
		},
		// Runtime 3: Low capacity, fewer agents (good for spread)
		{
			ObjectMeta: metav1.ObjectMeta{Name: "runtime-low-load", Namespace: "production"},
			Spec: v1.AgentRuntimeSpec{
				AgentFramework: v1.AgentFrameworkSpec{Type: "adk-go"},
				Replicas:       1,
			},
			Status: v1.AgentRuntimeStatus{
				Phase:         v1.RuntimePhaseRunning,
				ReadyReplicas: 1,
				AgentCount:    5,
			},
		},
		// Runtime 4: Different namespace (should be filtered out)
		{
			ObjectMeta: metav1.ObjectMeta{Name: "runtime-other-ns", Namespace: "other"},
			Spec: v1.AgentRuntimeSpec{
				AgentFramework: v1.AgentFrameworkSpec{Type: "adk-go"},
				Replicas:       2,
			},
			Status: v1.AgentRuntimeStatus{
				Phase:         v1.RuntimePhaseRunning,
				ReadyReplicas: 2,
				AgentCount:    10,
			},
		},
		// Runtime 5: Different framework type (should be filtered out)
		{
			ObjectMeta: metav1.ObjectMeta{Name: "runtime-openclaw", Namespace: "production"},
			Spec: v1.AgentRuntimeSpec{
				AgentFramework: v1.AgentFrameworkSpec{Type: "openclaw"},
				Replicas:       2,
			},
			Status: v1.AgentRuntimeStatus{
				Phase:         v1.RuntimePhaseRunning,
				ReadyReplicas: 2,
				AgentCount:    15,
			},
		},
		// Runtime 6: Not running (should be filtered out)
		{
			ObjectMeta: metav1.ObjectMeta{Name: "runtime-pending", Namespace: "production"},
			Spec: v1.AgentRuntimeSpec{
				AgentFramework: v1.AgentFrameworkSpec{Type: "adk-go"},
				Replicas:       1,
			},
			Status: v1.AgentRuntimeStatus{
				Phase:         v1.RuntimePhasePending,
				ReadyReplicas: 0,
				AgentCount:    0,
			},
		},
	}

	// Test Case 2: Test scheduling with BinPack strategy
	t.Log("Test Case 2: Scheduling with BinPack strategy")

	agentBinPack := &v1.AIAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-binpack", Namespace: "production"},
		Spec: v1.AIAgentSpec{
			RuntimeRef: v1.RuntimeReference{Type: "adk-go"},
		},
	}

	binpackScheduler := scheduler.NewSchedulerWithStrategy(scheduler.StrategyBinPack)
	targetBinPack, err := binpackScheduler.Schedule(ctx, agentBinPack, runtimes)
	if err != nil {
		t.Fatalf("BinPack scheduling failed: %v", err)
	}

	t.Logf("BinPack selected: %s (AgentCount=%d, ReadyReplicas=%d)",
		targetBinPack.Name, targetBinPack.Status.AgentCount, targetBinPack.Status.ReadyReplicas)

	// BinPack should favor runtime with more agents (runtime-high-load with 50)
	// Score calculation: base 50 + type match 100 + running 50 + readyReplicas*10 (30) = 230
	// runtime-medium: 20 + 100 + 50 + 20 = 190
	if targetBinPack.Name != "runtime-high-load" {
		t.Errorf("BinPack should select runtime-high-load, got %s", targetBinPack.Name)
	}

	// Test Case 3: Test scheduling with Spread strategy
	t.Log("Test Case 3: Scheduling with Spread strategy")

	agentSpread := &v1.AIAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-spread", Namespace: "production"},
		Spec: v1.AIAgentSpec{
			RuntimeRef: v1.RuntimeReference{Type: "adk-go"},
		},
	}

	spreadScheduler := scheduler.NewSchedulerWithStrategy(scheduler.StrategySpread)
	targetSpread, err := spreadScheduler.Schedule(ctx, agentSpread, runtimes)
	if err != nil {
		t.Fatalf("Spread scheduling failed: %v", err)
	}

	t.Logf("Spread selected: %s (AgentCount=%d, ReadyReplicas=%d)",
		targetSpread.Name, targetSpread.Status.AgentCount, targetSpread.Status.ReadyReplicas)

	// Spread should favor runtime with fewer agents (runtime-low-load with 5)
	// Score: 5 + 100 + 50 + 10 = 165 (lowest)
	if targetSpread.Name != "runtime-low-load" {
		t.Errorf("Spread should select runtime-low-load, got %s", targetSpread.Name)
	}

	// Test Case 4: Test scheduling with FirstFit strategy
	t.Log("Test Case 4: Scheduling with FirstFit strategy")

	agentFirstFit := &v1.AIAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-firstfit", Namespace: "production"},
		Spec: v1.AIAgentSpec{
			RuntimeRef: v1.RuntimeReference{Type: "adk-go"},
		},
	}

	firstFitScheduler := scheduler.NewSchedulerWithStrategy(scheduler.StrategyFirstFit)
	targetFirstFit, err := firstFitScheduler.Schedule(ctx, agentFirstFit, runtimes)
	if err != nil {
		t.Fatalf("FirstFit scheduling failed: %v", err)
	}

	t.Logf("FirstFit selected: %s", targetFirstFit.Name)

	// FirstFit returns the first suitable runtime (runtime-high-load)
	if targetFirstFit.Name != "runtime-high-load" {
		t.Errorf("FirstFit should select first suitable runtime (runtime-high-load), got %s", targetFirstFit.Name)
	}

	// Test Case 5: Test scheduling failure scenarios
	t.Log("Test Case 5: Test scheduling failure scenarios")

	// No matching runtimes (wrong namespace)
	agentWrongNs := &v1.AIAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-wrong-ns", Namespace: "development"},
		Spec: v1.AIAgentSpec{
			RuntimeRef: v1.RuntimeReference{Type: "adk-go"},
		},
	}

	_, err = binpackScheduler.Schedule(ctx, agentWrongNs, runtimes)
	if err == nil {
		t.Error("Expected error when no runtimes in matching namespace")
	}
	t.Logf("Expected error (wrong namespace): %v", err)

	// No matching framework type
	agentWrongType := &v1.AIAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-wrong-type", Namespace: "production"},
		Spec: v1.AIAgentSpec{
			RuntimeRef: v1.RuntimeReference{Type: "langchain"},
		},
	}

	_, err = binpackScheduler.Schedule(ctx, agentWrongType, runtimes)
	if err == nil {
		t.Error("Expected error when no runtimes with matching framework type")
	}
	t.Logf("Expected error (wrong type): %v", err)

	// Test Case 6: Test scheduling scoring
	t.Log("Test Case 6: Test scheduling scoring details")

	for _, rt := range runtimes {
		if rt.Status.Phase == v1.RuntimePhaseRunning && rt.Namespace == "production" && rt.Spec.AgentFramework.Type == "adk-go" {
			score, _ := binpackScheduler.Score(ctx, agentBinPack, rt)
			canSchedule := binpackScheduler.CanSchedule(ctx, agentBinPack, rt)
			t.Logf("Runtime %s: score=%d, canSchedule=%v", rt.Name, score, canSchedule)
		}
	}

	t.Log("Scheduler integration test completed")
}

// TestMigrationScheduler_Integration tests agent migration workflow.
func TestMigrationScheduler_Integration(t *testing.T) {
	ctx := context.Background()

	// Test Case 1: Create migration scenario
	t.Log("Test Case 1: Create migration scenario")

	oldRuntime := &v1.AgentRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "runtime-source", Namespace: "production"},
		Spec: v1.AgentRuntimeSpec{
			AgentFramework: v1.AgentFrameworkSpec{Type: "adk-go"},
		},
		Status: v1.AgentRuntimeStatus{
			Phase:         v1.RuntimePhaseRunning,
			ReadyReplicas: 1,
			AgentCount:    60, // Overloaded
		},
	}

	// Candidate runtimes for migration
	candidates := []*v1.AgentRuntime{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "runtime-target-1", Namespace: "production"},
			Spec: v1.AgentRuntimeSpec{
				AgentFramework: v1.AgentFrameworkSpec{Type: "adk-go"},
			},
			Status: v1.AgentRuntimeStatus{
				Phase:         v1.RuntimePhaseRunning,
				ReadyReplicas: 2,
				AgentCount:    10, // Light load
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "runtime-target-2", Namespace: "production"},
			Spec: v1.AgentRuntimeSpec{
				AgentFramework: v1.AgentFrameworkSpec{Type: "adk-go"},
			},
			Status: v1.AgentRuntimeStatus{
				Phase:         v1.RuntimePhaseRunning,
				ReadyReplicas: 1,
				AgentCount:    5, // Very light load
			},
		},
		// Old runtime itself (should be excluded)
		oldRuntime,
	}

	// Agent to migrate
	agent := &v1.AIAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-to-migrate", Namespace: "production"},
		Spec: v1.AIAgentSpec{
			RuntimeRef: v1.RuntimeReference{
				Name: oldRuntime.Name,
				Type: "adk-go",
			},
			Description: "Agent requiring migration",
		},
	}

	// Test Case 2: Plan migration
	t.Log("Test Case 2: Plan migration")

	migrationScheduler := scheduler.NewMigrationScheduler()
	target, plan, err := migrationScheduler.Migrate(ctx, agent, oldRuntime, candidates)
	if err != nil {
		t.Fatalf("Migration planning failed: %v", err)
	}

	// Verify target is not the old runtime
	if target.Name == oldRuntime.Name {
		t.Error("Target should be different from source runtime")
	}
	t.Logf("Migration target: %s (AgentCount=%d)", target.Name, target.Status.AgentCount)

	// Verify migration plan
	t.Logf("Migration plan created:")
	t.Logf("  Agent: %s/%s", plan.AgentNamespace, plan.AgentName)
	t.Logf("  Source: %s", plan.SourceRuntime)
	t.Logf("  Target: %s", plan.TargetRuntime)
	t.Logf("  Phase: %s", plan.Phase)
	t.Logf("  Steps: %d", len(plan.Steps))

	for i, step := range plan.Steps {
		t.Logf("    Step %d: %s (%s)", i+1, step.Phase, step.Description)
	}

	// Test Case 3: Verify spread strategy for migration
	t.Log("Test Case 3: Verify spread strategy for migration")

	// Migration scheduler uses spread to avoid overloading target
	// Should select runtime-target-2 with lowest AgentCount (5)
	if target.Name != "runtime-target-2" {
		t.Errorf("Migration should use spread and select runtime-target-2, got %s", target.Name)
	}

	// Test Case 4: Test migration with no alternatives
	t.Log("Test Case 4: Test migration with no alternatives")

	singleRuntime := &v1.AgentRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "only-runtime", Namespace: "production"},
		Status:     v1.AgentRuntimeStatus{Phase: v1.RuntimePhaseRunning},
	}

	_, _, err = migrationScheduler.Migrate(ctx, agent, singleRuntime, []*v1.AgentRuntime{singleRuntime})
	if err == nil {
		t.Error("Expected error when no alternative runtimes available")
	}
	t.Logf("Expected error (no alternatives): %v", err)

	t.Log("Migration scheduler integration test completed")
}

// TestSchedulerBatch_Integration tests scheduling multiple agents.
func TestSchedulerBatch_Integration(t *testing.T) {
	ctx := context.Background()

	// Create runtimes
	runtimes := []*v1.AgentRuntime{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "batch-runtime-1", Namespace: "production"},
			Spec: v1.AgentRuntimeSpec{
				AgentFramework: v1.AgentFrameworkSpec{Type: "adk-go"},
				Replicas:       2,
			},
			Status: v1.AgentRuntimeStatus{
				Phase:         v1.RuntimePhaseRunning,
				ReadyReplicas: 2,
				AgentCount:    0, // Empty
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "batch-runtime-2", Namespace: "production"},
			Spec: v1.AgentRuntimeSpec{
				AgentFramework: v1.AgentFrameworkSpec{Type: "adk-go"},
				Replicas:       2,
			},
			Status: v1.AgentRuntimeStatus{
				Phase:         v1.RuntimePhaseRunning,
				ReadyReplicas: 2,
				AgentCount:    0, // Empty
			},
		},
	}

	// Test Case 1: Schedule batch with spread strategy
	t.Log("Test Case 1: Schedule batch with spread strategy")

	schedulerSpread := scheduler.NewSchedulerWithStrategy(scheduler.StrategySpread)

	agents := []*v1.AIAgent{}
	for i := 0; i < 10; i++ {
		agents = append(agents, &v1.AIAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("batch-agent-%d", i),
				Namespace: "production",
			},
			Spec: v1.AIAgentSpec{
				RuntimeRef: v1.RuntimeReference{Type: "adk-go"},
			},
		})
	}

	// Schedule agents one by one, updating runtime status after each
	for i, agent := range agents {
		target, err := schedulerSpread.Schedule(ctx, agent, runtimes)
		if err != nil {
			t.Errorf("Failed to schedule agent %d: %v", i, err)
			continue
		}

		// Update runtime agent count (simulating actual binding)
		for _, rt := range runtimes {
			if rt.Name == target.Name {
				rt.Status.AgentCount++
			}
		}

		t.Logf("Agent %d scheduled to %s (counts: rt1=%d, rt2=%d)",
			i, target.Name, runtimes[0].Status.AgentCount, runtimes[1].Status.AgentCount)
	}

	// Verify spread distribution
	// With 10 agents and spread strategy, should be roughly equal (5 each)
	diff := runtimes[0].Status.AgentCount - runtimes[1].Status.AgentCount
	if diff > 1 || diff < -1 {
		t.Errorf("Spread should distribute evenly, got counts: %d vs %d",
			runtimes[0].Status.AgentCount, runtimes[1].Status.AgentCount)
	}

	t.Logf("Final distribution: runtime-1=%d, runtime-2=%d",
		runtimes[0].Status.AgentCount, runtimes[1].Status.AgentCount)

	// Test Case 2: Schedule batch with binpack strategy
	t.Log("Test Case 2: Schedule batch with binpack strategy")

	// Reset runtime counts
	runtimes[0].Status.AgentCount = 0
	runtimes[1].Status.AgentCount = 0

	schedulerBinPack := scheduler.NewSchedulerWithStrategy(scheduler.StrategyBinPack)

	for i, agent := range agents {
		target, err := schedulerBinPack.Schedule(ctx, agent, runtimes)
		if err != nil {
			t.Errorf("Failed to schedule agent %d: %v", i, err)
			continue
		}

		for _, rt := range runtimes {
			if rt.Name == target.Name {
				rt.Status.AgentCount++
			}
		}

		t.Logf("Agent %d scheduled to %s (counts: rt1=%d, rt2=%d)",
			i, target.Name, runtimes[0].Status.AgentCount, runtimes[1].Status.AgentCount)
	}

	t.Logf("BinPack final distribution: runtime-1=%d, runtime-2=%d",
		runtimes[0].Status.AgentCount, runtimes[1].Status.AgentCount)

	// With binpack, one runtime should have significantly more agents
	if runtimes[0].Status.AgentCount < runtimes[1].Status.AgentCount {
		t.Errorf("BinPack should favor runtime-1, but got runtime-1=%d, runtime-2=%d",
			runtimes[0].Status.AgentCount, runtimes[1].Status.AgentCount)
	}

	t.Log("Batch scheduling integration test completed")
}