package hostadapter

import (
	"context"
	"testing"

	agentruntime "github.com/tutti-os/tutti/packages/agent/daemon/runtime"
	host "github.com/tutti-os/tutti/packages/agent/host"
	"github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

type turnCapabilityRuntimeBackend struct {
	RuntimeBackend
	session agentruntime.Session
	input   agentruntime.CodexTurnCapabilityEnsureInput
	result  agentruntime.CodexTurnCapabilityEnsureResult
}

func (b *turnCapabilityRuntimeBackend) Session(_, _ string) (agentruntime.Session, bool) {
	return b.session, true
}

func (b *turnCapabilityRuntimeBackend) EnsureCodexTurnCapability(_ context.Context, input agentruntime.CodexTurnCapabilityEnsureInput) (agentruntime.CodexTurnCapabilityEnsureResult, error) {
	b.input = input
	return b.result, nil
}

func TestRuntimeControllerPassesPublicTurnSemanticToCodexBoundary(t *testing.T) {
	t.Parallel()
	backend := &turnCapabilityRuntimeBackend{
		session: agentruntime.Session{RoomID: "workspace-1", AgentSessionID: "session-1", Provider: agentruntime.ProviderCodex},
		result:  agentruntime.CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityRejected},
	}
	adapter := &RuntimeController{Backend: backend}
	_, err := adapter.EnsureTurnCapability(context.Background(), host.RuntimeTurnCapabilityInput{
		WorkspaceID: "workspace-1", AgentSessionID: "session-1", TurnID: "turn-1", ClientSubmitID: "submit-1",
		Invocation: host.TurnCapabilityInvocation{Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse},
		Plan:       host.RuntimeTurnCapabilityPlan{Key: "tutti"},
	})
	if err != nil || backend.input.Semantic != runtimeprep.CodexTurnCapabilitySemanticBrowserUse || backend.input.PlanKey != "tutti" {
		t.Fatalf("ensure input = %#v, error = %v", backend.input, err)
	}
}

func TestHostTurnCapabilityResultProjectsOneStructuredSkill(t *testing.T) {
	t.Parallel()
	result := hostTurnCapabilityResult(agentruntime.CodexTurnCapabilityEnsureResult{
		Disposition: runtimeprep.CodexTurnCapabilityAlreadyBound,
		PromptItem: runtimeprep.CodexTurnCapabilityPromptItem{
			Type: "skill", Name: "browser-use", Path: "/runtime/skills/browser-use/SKILL.md",
		},
	})
	if result.Disposition != host.RuntimeTurnCapabilityAlreadyBound || len(result.PromptAugmentation) != 1 {
		t.Fatalf("result = %#v", result)
	}
	skill := result.PromptAugmentation[0]
	if skill.Type != "skill" || skill.Name != "browser-use" || skill.Path != "/runtime/skills/browser-use/SKILL.md" {
		t.Fatalf("augmentation = %#v", skill)
	}
}

func TestHostTurnCapabilityResultOnlyProjectsValidatedMention(t *testing.T) {
	t.Parallel()
	result := hostTurnCapabilityResult(agentruntime.CodexTurnCapabilityEnsureResult{
		Disposition: runtimeprep.CodexTurnCapabilityApplied,
		Mention:     runtimeprep.CodexTurnCapabilityMention{Name: "sites@openai-bundled", Path: "plugin://sites@openai-bundled"},
	})
	if result.Disposition != host.RuntimeTurnCapabilityApplied || len(result.PromptAugmentation) != 1 {
		t.Fatalf("result = %#v", result)
	}
	mention := result.PromptAugmentation[0]
	if mention.Type != "mention" || mention.Name != "sites@openai-bundled" || mention.Path != "plugin://sites@openai-bundled" || mention.Text != "" {
		t.Fatalf("augmentation = %#v", mention)
	}
}

func TestHostTurnCapabilityResultDoesNotProjectRejectedMention(t *testing.T) {
	t.Parallel()
	result := hostTurnCapabilityResult(agentruntime.CodexTurnCapabilityEnsureResult{
		Disposition: runtimeprep.CodexTurnCapabilityRejected,
		Mention:     runtimeprep.CodexTurnCapabilityMention{Name: "ignored", Path: "plugin://ignored"},
	})
	if result.Disposition != host.RuntimeTurnCapabilityRejected || len(result.PromptAugmentation) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestHostTurnCapabilityResultProjectsOnlyClosedRecoveryOutcome(t *testing.T) {
	t.Parallel()
	result := hostTurnCapabilityResult(agentruntime.CodexTurnCapabilityEnsureResult{
		Disposition: runtimeprep.CodexTurnCapabilityRejected,
		NextAction:  "setup_required",
		ReasonCode:  "plugin_not_ready",
	})
	if result.Outcome == nil || result.Outcome.NextAction != host.RuntimeTurnCapabilityNextActionSetup || result.Outcome.ReasonCode != "plugin_not_ready" {
		t.Fatalf("outcome = %#v", result.Outcome)
	}
	if invalid := hostTurnCapabilityResult(agentruntime.CodexTurnCapabilityEnsureResult{
		Disposition: runtimeprep.CodexTurnCapabilityRejected,
		NextAction:  "plugin/install",
		ReasonCode:  "plugin://private",
	}); invalid.Outcome != nil {
		t.Fatalf("unsafe outcome leaked: %#v", invalid.Outcome)
	}
}
