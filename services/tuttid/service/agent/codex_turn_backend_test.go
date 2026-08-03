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

func TestSelectCodexTurnBackendIsModeOnly(t *testing.T) {
	if got := selectCodexTurnBackend(false, "browserUse", false); got != codexTurnBackendNative {
		t.Fatalf("mode off backend = %q, want native", got)
	}
	if got := selectCodexTurnBackend(true, "browserUse", true); got != codexTurnBackendTutti {
		t.Fatalf("mode on legacy backend = %q, want tutti", got)
	}
	if got := selectCodexTurnBackend(true, "sites", false); got != codexTurnBackendUnavailable {
		t.Fatalf("mode on Sites backend = %q, want unavailable", got)
	}
}

func TestServiceHostTurnCapabilityPortForwardsImmutableTuttiPlan(t *testing.T) {
	t.Parallel()
	native := &recordingTurnCapabilityPort{}
	port := serviceHostTurnCapabilityPort{native: native}
	input := agenthost.RuntimeTurnCapabilityInput{Plan: agenthost.RuntimeTurnCapabilityPlan{Key: string(codexTurnBackendTutti)}}
	if _, err := port.EnsureTurnCapability(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if native.input.Plan.Key != string(codexTurnBackendTutti) {
		t.Fatalf("forwarded plan = %#v", native.input.Plan)
	}
}
