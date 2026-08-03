package agent

import (
	"context"
	"strings"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
	"github.com/tutti-os/tutti/services/tuttid/biz/tuttimodeactivation"
)

// codexTurnBackend is daemon product policy for an already admitted built-in
// Codex target. It deliberately does not inspect plugin inventory or runtime
// readiness: mode alone chooses the delivery family.
type codexTurnBackend string

const (
	codexTurnBackendNative      codexTurnBackend = "codex_native"
	codexTurnBackendTutti       codexTurnBackend = "tutti"
	codexTurnBackendUnavailable codexTurnBackend = "unavailable"
)

func codexTurnCapabilityPlan(backend codexTurnBackend) agenthost.RuntimeTurnCapabilityPlan {
	return agenthost.RuntimeTurnCapabilityPlan{Key: string(backend)}
}

func legacyTuttiTurnCapabilitySemantic(semantic string) bool {
	switch strings.TrimSpace(semantic) {
	case runtimeprep.CodexTurnCapabilitySemanticBrowserUse, runtimeprep.CodexTurnCapabilitySemanticComputerUse:
		return true
	default:
		return false
	}
}

// selectCodexTurnBackend is intentionally closed and private. The caller must
// first prove the exact built-in Codex target; non-Codex providers never enter
// this policy.
func selectCodexTurnBackend(tuttiModeActive bool, semantic string, legacySupported bool) codexTurnBackend {
	if !tuttiModeActive {
		return codexTurnBackendNative
	}
	if legacySupported {
		return codexTurnBackendTutti
	}
	return codexTurnBackendUnavailable
}

func initialTuttiModeActive(intent *TuttiModeActivationIntent) bool {
	return intent != nil && strings.TrimSpace(intent.State) == string(tuttimodeactivation.StateActive)
}

// serviceHostTurnCapabilityPort keeps backend mechanics behind the one Host
// Ensure call. The plan is supplied directly by the post-claim admission
// result; this adapter never stores it or looks it up by key.
type serviceHostTurnCapabilityPort struct {
	native agenthost.RuntimeTurnCapabilityPort
}

func (p serviceHostTurnCapabilityPort) EnsureTurnCapability(
	ctx context.Context,
	input agenthost.RuntimeTurnCapabilityInput,
) (agenthost.RuntimeTurnCapabilityResult, error) {
	switch codexTurnBackend(strings.TrimSpace(input.Plan.Key)) {
	case codexTurnBackendNative, codexTurnBackendTutti:
		return p.native.EnsureTurnCapability(ctx, input)
	default:
		return agenthost.RuntimeTurnCapabilityResult{Disposition: agenthost.RuntimeTurnCapabilityRejected}, nil
	}
}
