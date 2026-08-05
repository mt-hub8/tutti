package agent

import (
	"context"
	"testing"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
)

type recordingTurnCapabilityPort struct {
	input agenthost.RuntimeTurnCapabilityInput
}

func (p *recordingTurnCapabilityPort) EnsureTurnCapability(_ context.Context, input agenthost.RuntimeTurnCapabilityInput) (agenthost.RuntimeTurnCapabilityResult, error) {
	p.input = input
	return agenthost.RuntimeTurnCapabilityResult{Disposition: agenthost.RuntimeTurnCapabilityRejected}, nil
}

func TestServiceHostTurnCapabilityPortForwardsImmutableNativePlan(t *testing.T) {
	t.Parallel()
	native := &recordingTurnCapabilityPort{}
	port := serviceHostTurnCapabilityPort{native: native}
	input := agenthost.RuntimeTurnCapabilityInput{Plan: codexNativeTurnCapabilityPlan()}
	if _, err := port.EnsureTurnCapability(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if native.input.Plan.Key != codexNativeTurnCapabilityPlanKey {
		t.Fatalf("forwarded plan = %#v", native.input.Plan)
	}
}
