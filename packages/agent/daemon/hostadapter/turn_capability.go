package hostadapter

import (
	"context"
	"strings"

	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
	host "github.com/tutti-os/tutti/packages/agent/host"
	"github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

var _ host.RuntimeTurnCapabilityPort = (*RuntimeController)(nil)

type codexTurnCapabilityBackend interface {
	EnsureCodexTurnCapability(context.Context, agentruntime.CodexTurnCapabilityEnsureInput) (agentruntime.CodexTurnCapabilityEnsureResult, error)
}

// EnsureTurnCapability adapts the narrow Host seam to the Codex-only daemon
// preparation path. Provider checks happen before any filesystem, config, or
// process effect; the Host never receives plugin inventory or binding details.
func (a *RuntimeController) EnsureTurnCapability(ctx context.Context, input host.RuntimeTurnCapabilityInput) (host.RuntimeTurnCapabilityResult, error) {
	if err := a.requireBackend(); err != nil {
		return host.RuntimeTurnCapabilityResult{Disposition: host.RuntimeTurnCapabilityUnknown}, err
	}
	if _, found := a.Backend.Session(input.WorkspaceID, input.AgentSessionID); !found {
		return host.RuntimeTurnCapabilityResult{Disposition: host.RuntimeTurnCapabilityRejected}, nil
	}
	backend, ok := a.Backend.(codexTurnCapabilityBackend)
	if !ok {
		return host.RuntimeTurnCapabilityResult{Disposition: host.RuntimeTurnCapabilityRejected}, nil
	}
	result, err := backend.EnsureCodexTurnCapability(ctx, agentruntime.CodexTurnCapabilityEnsureInput{
		RoomID:         input.WorkspaceID,
		AgentSessionID: input.AgentSessionID,
		TurnID:         input.TurnID,
		ClientSubmitID: input.ClientSubmitID,
		Semantic:       input.Invocation.Semantic,
		Consent:        runtimeprep.CodexTurnCapabilityConsent(input.Invocation.Consent),
		PlanKey:        input.Plan.Key,
	})
	return hostTurnCapabilityResult(result), err
}

func hostTurnCapabilityResult(result agentruntime.CodexTurnCapabilityEnsureResult) host.RuntimeTurnCapabilityResult {
	hostResult := host.RuntimeTurnCapabilityResult{Disposition: host.RuntimeTurnCapabilityUnknown}
	switch result.Disposition {
	case runtimeprep.CodexTurnCapabilityAlreadyBound:
		hostResult.Disposition = host.RuntimeTurnCapabilityAlreadyBound
	case runtimeprep.CodexTurnCapabilityApplied:
		hostResult.Disposition = host.RuntimeTurnCapabilityApplied
	case runtimeprep.CodexTurnCapabilityRejected:
		hostResult.Disposition = host.RuntimeTurnCapabilityRejected
	case runtimeprep.CodexTurnCapabilityUnknown:
		hostResult.Disposition = host.RuntimeTurnCapabilityUnknown
	}
	hostResult.Retryable = result.Retryable
	if outcome, ok := hostTurnCapabilityOutcome(result.NextAction, result.ReasonCode); ok {
		hostResult.Outcome = &outcome
	}
	if hostResult.Disposition == host.RuntimeTurnCapabilityAlreadyBound || hostResult.Disposition == host.RuntimeTurnCapabilityApplied {
		promptItem := result.PromptItem
		if strings.TrimSpace(promptItem.Type) == "" {
			promptItem = runtimeprep.CodexTurnCapabilityPromptItem{Type: "mention", Name: result.Mention.Name, Path: result.Mention.Path}
		}
		hostResult.PromptAugmentation = []host.PromptContentBlock{{
			Type: promptItem.Type,
			Name: promptItem.Name,
			Path: promptItem.Path,
		}}
	}
	return hostResult
}

func hostTurnCapabilityOutcome(action, reason string) (host.RuntimeTurnCapabilityOutcome, bool) {
	var next host.RuntimeTurnCapabilityNextAction
	switch strings.TrimSpace(action) {
	case "setup_required":
		next = host.RuntimeTurnCapabilityNextActionSetup
	case "enable_required":
		next = host.RuntimeTurnCapabilityNextActionEnable
	case "authorize_required":
		next = host.RuntimeTurnCapabilityNextActionAuthorize
	case "retry":
		next = host.RuntimeTurnCapabilityNextActionRetry
	case "blocked":
		next = host.RuntimeTurnCapabilityNextActionBlocked
	default:
		return host.RuntimeTurnCapabilityOutcome{}, false
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return host.RuntimeTurnCapabilityOutcome{}, false
	}
	return host.RuntimeTurnCapabilityOutcome{NextAction: next, ReasonCode: reason}, true
}
