package agent

import (
	"context"
	"strings"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
)

const codexNativeTurnCapabilityPlanKey = "codex_native"

// codexNativeTurnCapabilityPlan is the only delivery plan available for the
// exact built-in Codex target. Tutti Mode is deliberately absent: it is a
// workflow concern carried by RuntimeExecInput, never a plugin selector.
func codexNativeTurnCapabilityPlan() agenthost.RuntimeTurnCapabilityPlan {
	return agenthost.RuntimeTurnCapabilityPlan{Key: codexNativeTurnCapabilityPlanKey}
}

// serviceHostTurnCapabilityPort accepts only the fixed native plan at the Host
// Ensure seam. This keeps Codex plugin delivery provider-local while Host owns
// claim, lock, and exactly-once Exec ordering.
type serviceHostTurnCapabilityPort struct {
	native agenthost.RuntimeTurnCapabilityPort
}

func (p serviceHostTurnCapabilityPort) EnsureTurnCapability(
	ctx context.Context,
	input agenthost.RuntimeTurnCapabilityInput,
) (agenthost.RuntimeTurnCapabilityResult, error) {
	switch strings.TrimSpace(input.Plan.Key) {
	case codexNativeTurnCapabilityPlanKey:
		return p.native.EnsureTurnCapability(ctx, input)
	default:
		return agenthost.RuntimeTurnCapabilityResult{Disposition: agenthost.RuntimeTurnCapabilityRejected}, nil
	}
}
