package agentruntime

import (
	"context"
	"strings"

	"github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

// CodexTurnCapabilityEnsureInput is the daemon-internal exact invocation key
// for a Codex native capability. It is deliberately not part of the Host
// RuntimeController surface.
type CodexTurnCapabilityEnsureInput struct {
	RoomID         string
	AgentSessionID string
	TurnID         string
	ClientSubmitID string
	Semantic       string
	Consent        runtimeprep.CodexTurnCapabilityConsent
	// PlanKey is the immutable, Host-admitted delivery family. It is opaque to
	// Host and comes directly from the durable submit claim, never current UI
	// state or a process-local staging map.
	PlanKey string
}

// CodexTurnCapabilityEnsureResult contains the safe structured plugin mention and
// sanitized binding facts after a provider-local capability check.
type CodexTurnCapabilityEnsureResult struct {
	Disposition runtimeprep.CodexTurnCapabilityDisposition
	Mention     runtimeprep.CodexTurnCapabilityMention
	PromptItem  runtimeprep.CodexTurnCapabilityPromptItem
	Binding     runtimeprep.CodexTurnCapabilityBinding
	// NextAction and ReasonCode are sanitized provider-local recovery facts.
	// The Host adapter translates the closed action vocabulary; raw plugin and
	// App Server diagnostics remain in Reason only.
	NextAction string
	ReasonCode string
	Reason     string
	Retryable  bool
}

// codexTurnCapabilityRuntime marks the provider-owned adapter that can safely
// rebind a native Codex capability. It avoids a cross-provider identity branch
// in the controller; adapters opt in through their own mechanics.
type codexTurnCapabilityRuntime interface {
	supportsCodexTurnCapabilityRuntime()
}

type codexTurnCapabilityLiveReadiness interface {
	EnsureLiveCodexTurnCapability(context.Context, Session, string, runtimeprep.CodexTurnCapabilityConsent) (CodexTurnCapabilityEnsureResult, error)
	EnsureLiveCodexTuttiTurnCapability(context.Context, Session, string, runtimeprep.CodexTurnCapabilityConsent) (CodexTurnCapabilityEnsureResult, error)
}

// EnsureCodexTurnCapability checks only the current Codex runtime. It never
// prepares configuration, resumes, or replaces a client on this normal path.
func (c *Controller) EnsureCodexTurnCapability(ctx context.Context, input CodexTurnCapabilityEnsureInput) (CodexTurnCapabilityEnsureResult, error) {
	roomID := strings.TrimSpace(input.RoomID)
	agentSessionID := strings.TrimSpace(input.AgentSessionID)
	if roomID == "" || agentSessionID == "" || strings.TrimSpace(input.TurnID) == "" || strings.TrimSpace(input.ClientSubmitID) == "" {
		return CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityRejected}, nil
	}
	release, err := c.acquireLifecycleLockContext(ctx, roomID, agentSessionID)
	if err != nil {
		return CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityUnknown}, err
	}
	defer release()

	session, adapter, err := c.sessionAndAdapter(roomID, agentSessionID)
	if err != nil {
		return CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityRejected}, nil
	}
	if _, ok := adapter.(codexTurnCapabilityRuntime); !ok {
		return CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityRejected, Reason: "native turn capability is only available for Codex"}, nil
	}
	live, ok := adapter.(codexTurnCapabilityLiveReadiness)
	if !ok {
		return CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityRejected, Reason: "current runtime does not support live capability readiness"}, nil
	}
	var result CodexTurnCapabilityEnsureResult
	switch strings.TrimSpace(input.PlanKey) {
	case "codex_native":
		nativeSemantic, known := runtimeprep.CodexNativeCapabilityForTurnSemantic(input.Semantic)
		if !known {
			return CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityRejected, Reason: "unknown Codex turn capability"}, nil
		}
		result, err = live.EnsureLiveCodexTurnCapability(ctx, session, nativeSemantic, input.Consent)
	case "tutti":
		result, err = live.EnsureLiveCodexTuttiTurnCapability(ctx, session, input.Semantic, input.Consent)
	default:
		return CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityRejected, Reason: "unknown capability delivery plan"}, nil
	}
	if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound || strings.TrimSpace(input.Semantic) != runtimeprep.CodexTurnCapabilitySemanticComputerUse || !result.Binding.Authorized {
		return result, err
	}
	// Only the coarse Computer authorization fact survives a daemon restart.
	// Current-client readiness is deliberately session-local and is never a
	// durable substitute for the next App Server generation's verification.
	result.Binding.Loaded = false
	result.Binding.Ready = false
	session.RuntimeContext = runtimeprep.RuntimeContextWithCodexTurnCapabilityBinding(session.RuntimeContext, result.Binding)
	c.store(session)
	return result, nil
}
