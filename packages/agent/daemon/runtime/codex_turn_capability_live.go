package agentruntime

import (
	"context"
	"strings"

	"github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

// EnsureLiveCodexTurnCapability admits one exact capability Turn on the
// already-running Codex Thread. Plugin bundles are resolved by App Server from
// the structured mention carried by that Turn; this adapter must not turn
// experimental plugin inventory, App, or MCP discovery into a prerequisite for
// delivery.
//
// The check is deliberately narrow: it proves only that the current runtime is
// still the Session's runtime, and (for Computer Use) that this Session has
// explicit user consent. It never creates or replaces a client, resumes a
// Thread, reloads configuration, or writes Loaded/Ready runtime state.
func (a *CodexAppServerAdapter) EnsureLiveCodexTurnCapability(_ context.Context, session Session, semantic string, consent runtimeprep.CodexTurnCapabilityConsent) (CodexTurnCapabilityEnsureResult, error) {
	a.mu.Lock()
	live := a.sessions[strings.TrimSpace(session.AgentSessionID)]
	if live == nil || live.client == nil || strings.TrimSpace(live.threadID) != strings.TrimSpace(session.ProviderSessionID) {
		a.mu.Unlock()
		return codexTurnCapabilityRejected("retry", "runtime_unavailable", "current Codex runtime is unavailable"), nil
	}
	if semantic == runtimeprep.CodexNativeCapabilityComputer && consent != runtimeprep.CodexTurnCapabilityConsentExplicitSession && !codexComputerAuthorized(session.RuntimeContext) {
		a.mu.Unlock()
		return codexTurnCapabilityRejected("authorize_required", "consent_required", "computer use requires explicit session-scoped authorization"), nil
	}
	a.mu.Unlock()

	pluginID, ok := codexTurnCapabilityPluginID(semantic)
	if !ok {
		return CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityRejected}, nil
	}
	return codexLiveTurnCapabilityResult(session, semantic, pluginID, consent), nil
}

func codexTurnCapabilityRejected(action, reasonCode, reason string) CodexTurnCapabilityEnsureResult {
	return CodexTurnCapabilityEnsureResult{
		Disposition: runtimeprep.CodexTurnCapabilityRejected,
		NextAction:  action,
		ReasonCode:  reasonCode,
		Reason:      reason,
	}
}

func codexLiveTurnCapabilityResult(session Session, semantic, pluginID string, consent runtimeprep.CodexTurnCapabilityConsent) CodexTurnCapabilityEnsureResult {
	mention := runtimeprep.CodexTurnCapabilityMention{Name: pluginID, Path: "plugin://" + pluginID}
	return CodexTurnCapabilityEnsureResult{
		Disposition: runtimeprep.CodexTurnCapabilityAlreadyBound,
		Mention:     mention,
		PromptItem:  runtimeprep.CodexTurnCapabilityPromptItem{Type: "mention", Name: mention.Name, Path: mention.Path},
		Binding: runtimeprep.CodexTurnCapabilityBinding{
			Semantic:   semantic,
			Authorized: semantic != runtimeprep.CodexNativeCapabilityComputer || consent == runtimeprep.CodexTurnCapabilityConsentExplicitSession || codexComputerAuthorized(session.RuntimeContext),
			Consent:    consent,
		},
	}
}

func codexComputerAuthorized(context map[string]any) bool {
	for _, binding := range runtimeprep.CodexTurnCapabilityBindingsFromRuntimeContext(context) {
		if binding.Semantic == runtimeprep.CodexNativeCapabilityComputer && binding.Authorized {
			return true
		}
	}
	return false
}

func codexTurnCapabilityPluginID(semantic string) (string, bool) {
	switch semantic {
	case runtimeprep.CodexNativeCapabilityBrowser:
		return runtimeprep.CodexNativePluginBrowser, true
	case runtimeprep.CodexNativeCapabilityComputer:
		return runtimeprep.CodexNativePluginComputerUse, true
	case runtimeprep.CodexNativeCapabilitySites:
		return runtimeprep.CodexNativePluginSites, true
	default:
		return "", false
	}
}
