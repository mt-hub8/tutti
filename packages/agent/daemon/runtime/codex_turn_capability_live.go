package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tutti-os/tutti/packages/agent/daemon/runtime/codexproto"
	"github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

type codexTurnCapabilityLiveReadinessState struct {
	client *codexAppServerClient
	result CodexTurnCapabilityEnsureResult
}

type codexTurnCapabilityRefreshFlight struct {
	done   chan struct{}
	result CodexTurnCapabilityEnsureResult
}

var codexLiveCapabilityRefreshMinimumVersion = [3]int{0, 137, 0}

type codexTurnCapabilityReadinessUnknownError struct{ err error }

func (e codexTurnCapabilityReadinessUnknownError) Error() string { return e.err.Error() }
func (e codexTurnCapabilityReadinessUnknownError) Unwrap() error { return e.err }

// codexPluginReadinessError preserves a current plugin/list classification
// until it can be translated into the provider-neutral recovery outcome. It
// remains private to the Codex adapter: Host never sees plugin protocol data.
type codexPluginReadinessError struct {
	nextAction string
	reasonCode string
	err        error
}

func (e codexPluginReadinessError) Error() string { return e.err.Error() }
func (e codexPluginReadinessError) Unwrap() error { return e.err }

func codexPluginReadinessOutcome(err error) (CodexTurnCapabilityEnsureResult, bool) {
	var readiness codexPluginReadinessError
	if !errors.As(err, &readiness) {
		return CodexTurnCapabilityEnsureResult{}, false
	}
	return codexTurnCapabilityRejected(readiness.nextAction, readiness.reasonCode, readiness.Error()), true
}

func codexTurnCapabilityReadinessDisposition(err error) runtimeprep.CodexTurnCapabilityDisposition {
	var unknown codexTurnCapabilityReadinessUnknownError
	if errors.As(err, &unknown) {
		return runtimeprep.CodexTurnCapabilityUnknown
	}
	return runtimeprep.CodexTurnCapabilityRejected
}

// EnsureLiveCodexTurnCapability validates exactly one already-running official
// bundle. A refresh, if the selected bundle is installed/enabled but not live,
// uses only the existing client and thread. It never creates or replaces a
// client, calls thread/resume, or writes durable Loaded/Ready state.
func (a *CodexAppServerAdapter) EnsureLiveCodexTurnCapability(ctx context.Context, session Session, semantic string, consent runtimeprep.CodexTurnCapabilityConsent) (CodexTurnCapabilityEnsureResult, error) {
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
	if codexTurnCapabilityRefreshBusy(live) {
		a.mu.Unlock()
		return codexTurnCapabilityRejected("retry", "runtime_busy", "current Codex session is busy; retry capability after its active interaction completes"), nil
	}
	client, threadID := live.client, live.threadID
	a.mu.Unlock()
	pluginID, ok := codexTurnCapabilityPluginID(semantic)
	if !ok {
		return CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityRejected}, nil
	}
	pluginVersion, err := codexLivePluginVersion(ctx, client, pluginID)
	if err != nil {
		if outcome, mapped := codexPluginReadinessOutcome(err); mapped {
			return outcome, nil
		}
		if codexTurnCapabilityReadinessDisposition(err) == runtimeprep.CodexTurnCapabilityUnknown {
			return codexTurnCapabilityUnknown("retry", "availability_unknown", err.Error()), nil
		}
		return codexTurnCapabilityRejected("setup_required", "plugin_not_ready", err.Error()), nil
	}
	cacheKey := semantic + "\x00" + pluginVersion
	if result, waiting := a.waitForLiveCodexTurnCapabilityRefresh(ctx, session, live, client, cacheKey); waiting {
		return result, nil
	}
	// The session-local cache records a validated generation/version tuple, but
	// never skips this call's App/MCP check: those runtime facts may change while
	// the same client and installed package remain in place.
	if err := validateCodexTurnCapabilityRuntime(ctx, client, threadID, semantic, true); err != nil {
		if codexTurnCapabilityReadinessDisposition(err) == runtimeprep.CodexTurnCapabilityUnknown {
			return CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityUnknown, Reason: err.Error(), Retryable: true}, nil
		}
		return a.refreshLiveCodexTurnCapability(ctx, session, live, client, threadID, semantic, pluginID, pluginVersion, consent)
	}
	result := codexLiveTurnCapabilityResult(session, semantic, pluginID, pluginVersion, consent)
	a.mu.Lock()
	if current := a.sessions[strings.TrimSpace(session.AgentSessionID)]; current == live && current.client == client {
		if current.turnCapabilityReadiness == nil {
			current.turnCapabilityReadiness = make(map[string]codexTurnCapabilityLiveReadinessState)
		}
		current.turnCapabilityReadiness[cacheKey] = codexTurnCapabilityLiveReadinessState{client: client, result: result}
	}
	a.mu.Unlock()
	return result, nil
}

func (a *CodexAppServerAdapter) waitForLiveCodexTurnCapabilityRefresh(ctx context.Context, session Session, live *codexAppServerSession, client *codexAppServerClient, cacheKey string) (CodexTurnCapabilityEnsureResult, bool) {
	a.mu.Lock()
	current := a.sessions[strings.TrimSpace(session.AgentSessionID)]
	if current != live || current.client != client {
		a.mu.Unlock()
		return CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityUnknown, Reason: "Codex runtime generation changed during readiness", Retryable: true}, true
	}
	flight := current.turnCapabilityRefreshes[cacheKey]
	if flight == nil {
		a.mu.Unlock()
		return CodexTurnCapabilityEnsureResult{}, false
	}
	done := flight.done
	a.mu.Unlock()
	select {
	case <-done:
		return flight.result, true
	case <-ctx.Done():
		return CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityUnknown, Reason: "capability refresh wait cancelled", Retryable: true}, true
	}
}

func codexTurnCapabilityRefreshBusy(live *codexAppServerSession) bool {
	return codexAppServerSessionHasLiveWork(live)
}

func (a *CodexAppServerAdapter) refreshLiveCodexTurnCapability(ctx context.Context, session Session, live *codexAppServerSession, client *codexAppServerClient, threadID, semantic, pluginID, pluginVersion string, consent runtimeprep.CodexTurnCapabilityConsent) (CodexTurnCapabilityEnsureResult, error) {
	cacheKey := semantic + "\x00" + pluginVersion
	a.mu.Lock()
	current := a.sessions[strings.TrimSpace(session.AgentSessionID)]
	if current != live || current.client != client || current.threadID != threadID {
		a.mu.Unlock()
		return CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityUnknown, Reason: "Codex runtime generation changed during readiness", Retryable: true}, nil
	}
	if codexTurnCapabilityRefreshBusy(current) {
		a.mu.Unlock()
		return codexTurnCapabilityRejected("retry", "runtime_busy", "current Codex session is busy; retry capability after its active interaction completes"), nil
	}
	if !codexLiveCapabilityRefreshSupported(current.serverInfo) {
		a.mu.Unlock()
		return codexTurnCapabilityRejected("blocked", "refresh_unsupported", "current Codex runtime does not support in-place capability refresh"), nil
	}
	if flight := current.turnCapabilityRefreshes[cacheKey]; flight != nil {
		done := flight.done
		a.mu.Unlock()
		select {
		case <-done:
			return flight.result, nil
		case <-ctx.Done():
			return CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityUnknown, Reason: "capability refresh wait cancelled", Retryable: true}, nil
		}
	}
	if current.turnCapabilityRefreshes == nil {
		current.turnCapabilityRefreshes = make(map[string]*codexTurnCapabilityRefreshFlight)
	}
	flight := &codexTurnCapabilityRefreshFlight{done: make(chan struct{})}
	current.turnCapabilityRefreshes[cacheKey] = flight
	a.mu.Unlock()

	result := a.refreshCurrentCodexTurnCapability(ctx, session, client, threadID, semantic, pluginID, pluginVersion, consent)
	a.mu.Lock()
	if current := a.sessions[strings.TrimSpace(session.AgentSessionID)]; current != live || current.client != client || current.threadID != threadID {
		result = CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityUnknown, Reason: "Codex runtime generation changed during capability refresh", Retryable: true}
	} else {
		if result.Disposition == runtimeprep.CodexTurnCapabilityAlreadyBound {
			if current.turnCapabilityReadiness == nil {
				current.turnCapabilityReadiness = make(map[string]codexTurnCapabilityLiveReadinessState)
			}
			current.turnCapabilityReadiness[cacheKey] = codexTurnCapabilityLiveReadinessState{client: client, result: result}
		}
		delete(current.turnCapabilityRefreshes, cacheKey)
	}
	flight.result = result
	close(flight.done)
	a.mu.Unlock()
	return result, nil
}

func codexTurnCapabilityRejected(action, reasonCode, reason string) CodexTurnCapabilityEnsureResult {
	return CodexTurnCapabilityEnsureResult{
		Disposition: runtimeprep.CodexTurnCapabilityRejected,
		NextAction:  action,
		ReasonCode:  reasonCode,
		Reason:      reason,
	}
}

func codexTurnCapabilityUnknown(action, reasonCode, reason string) CodexTurnCapabilityEnsureResult {
	return CodexTurnCapabilityEnsureResult{
		Disposition: runtimeprep.CodexTurnCapabilityUnknown,
		NextAction:  action,
		ReasonCode:  reasonCode,
		Reason:      reason,
		Retryable:   true,
	}
}

func codexLiveCapabilityRefreshSupported(serverInfo map[string]any) bool {
	version, ok := codexAppServerUserAgentVersion(serverInfo)
	return ok && versionAtLeast(version, codexLiveCapabilityRefreshMinimumVersion)
}

func (a *CodexAppServerAdapter) refreshCurrentCodexTurnCapability(ctx context.Context, session Session, client *codexAppServerClient, threadID, semantic, pluginID, pluginVersion string, consent runtimeprep.CodexTurnCapabilityConsent) CodexTurnCapabilityEnsureResult {
	typed, _ := client.typed(10*time.Second, nil, true)
	if semantic != runtimeprep.CodexNativeCapabilitySites {
		if _, err := typed.ConfigMcpServerReload(ctx); err != nil {
			return codexTurnCapabilityUnknown("retry", "refresh_failed", fmt.Sprintf("reload Codex MCP runtime: %v", err))
		}
	}
	forceReload := true
	if _, err := typed.SkillsList(ctx, codexproto.SkillsListParams{ForceReload: &forceReload}); err != nil {
		return codexTurnCapabilityUnknown("retry", "refresh_failed", fmt.Sprintf("reload Codex runtime skills: %v", err))
	}
	if semantic == runtimeprep.CodexNativeCapabilitySites {
		if _, err := typed.AppList(ctx, codexproto.AppsListParams{ForceRefetch: &forceReload, ThreadID: &threadID}); err != nil {
			return codexTurnCapabilityUnknown("retry", "refresh_failed", fmt.Sprintf("reload Sites app runtime: %v", err))
		}
	}
	if err := validateCodexTurnCapabilityRuntime(ctx, client, threadID, semantic, false); err != nil {
		if codexTurnCapabilityReadinessDisposition(err) == runtimeprep.CodexTurnCapabilityUnknown {
			return codexTurnCapabilityUnknown("retry", "availability_unknown", err.Error())
		}
		return codexTurnCapabilityRejected("setup_required", "runtime_not_ready", err.Error())
	}
	return codexLiveTurnCapabilityResult(session, semantic, pluginID, pluginVersion, consent)
}

func codexLiveTurnCapabilityResult(session Session, semantic, pluginID, pluginVersion string, consent runtimeprep.CodexTurnCapabilityConsent) CodexTurnCapabilityEnsureResult {
	mention := runtimeprep.CodexTurnCapabilityMention{Name: pluginID, Path: "plugin://" + pluginID}
	return CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityAlreadyBound, Mention: mention, PromptItem: runtimeprep.CodexTurnCapabilityPromptItem{Type: "mention", Name: mention.Name, Path: mention.Path}, Binding: runtimeprep.CodexTurnCapabilityBinding{Semantic: semantic, PackageVersion: pluginVersion, Authorized: semantic != runtimeprep.CodexNativeCapabilityComputer || consent == runtimeprep.CodexTurnCapabilityConsentExplicitSession || codexComputerAuthorized(session.RuntimeContext), Consent: consent}}
}

// EnsureLiveCodexTuttiTurnCapability finds one already-materialized Tutti CLI
// skill from the current App Server. It never probes native plugins, mutates
// settings/env, or replaces the runtime. A stale skill list gets one in-place
// forceReload before this returns setup_required.
func (a *CodexAppServerAdapter) EnsureLiveCodexTuttiTurnCapability(ctx context.Context, session Session, semantic string, consent runtimeprep.CodexTurnCapabilityConsent) (CodexTurnCapabilityEnsureResult, error) {
	identity, supported := runtimeprep.CodexTuttiTurnSkillForTurnSemantic(semantic)
	if !supported {
		return codexTurnCapabilityRejected("blocked", "tutti_backend_unsupported", "Tutti backend does not support this capability"), nil
	}
	if semantic == runtimeprep.CodexTurnCapabilitySemanticComputerUse && consent != runtimeprep.CodexTurnCapabilityConsentExplicitSession && !codexComputerAuthorized(session.RuntimeContext) {
		return codexTurnCapabilityRejected("authorize_required", "consent_required", "computer use requires explicit session-scoped authorization"), nil
	}
	a.mu.Lock()
	live := a.sessions[strings.TrimSpace(session.AgentSessionID)]
	if live == nil || live.client == nil || strings.TrimSpace(live.threadID) != strings.TrimSpace(session.ProviderSessionID) {
		a.mu.Unlock()
		return codexTurnCapabilityRejected("retry", "runtime_unavailable", "current Codex runtime is unavailable"), nil
	}
	if codexTurnCapabilityRefreshBusy(live) {
		a.mu.Unlock()
		return codexTurnCapabilityRejected("retry", "runtime_busy", "current Codex session is busy; retry capability after its active interaction completes"), nil
	}
	client := live.client
	a.mu.Unlock()
	codexHome := envValueLast(session.Env, "CODEX_HOME")
	promptItem, found, err := codexLiveTuttiTurnSkill(ctx, client, codexHome, semantic, false)
	if err != nil {
		return codexTurnCapabilityUnknown("retry", "availability_unknown", err.Error()), nil
	}
	if !found {
		promptItem, found, err = codexLiveTuttiTurnSkill(ctx, client, codexHome, semantic, true)
		if err != nil {
			return codexTurnCapabilityUnknown("retry", "refresh_failed", err.Error()), nil
		}
	}
	if !found {
		return codexTurnCapabilityRejected("setup_required", "tutti_skill_not_ready", "Tutti capability skill is not materialized for this Codex runtime"), nil
	}
	nativeSemantic, _ := runtimeprep.CodexNativeCapabilityForTurnSemantic(semantic)
	return CodexTurnCapabilityEnsureResult{
		Disposition: runtimeprep.CodexTurnCapabilityAlreadyBound,
		PromptItem:  promptItem,
		Binding: runtimeprep.CodexTurnCapabilityBinding{
			Semantic:   nativeSemantic,
			Authorized: nativeSemantic != runtimeprep.CodexNativeCapabilityComputer || consent == runtimeprep.CodexTurnCapabilityConsentExplicitSession || codexComputerAuthorized(session.RuntimeContext),
			Consent:    consent,
		},
		Reason: identity.Name,
	}, nil
}

func codexLiveTuttiTurnSkill(ctx context.Context, client *codexAppServerClient, codexHome, semantic string, forceReload bool) (runtimeprep.CodexTurnCapabilityPromptItem, bool, error) {
	typed, caller := client.typed(10*time.Second, nil, true)
	if _, err := typed.SkillsList(ctx, codexproto.SkillsListParams{ForceReload: &forceReload}); err != nil {
		return runtimeprep.CodexTurnCapabilityPromptItem{}, false, fmt.Errorf("list current Codex runtime skills: %w", err)
	}
	promptItem, found := codexTuttiTurnSkillFromList(caller.rawResult, codexHome, semantic)
	return promptItem, found, nil
}

func codexTuttiTurnSkillFromList(raw json.RawMessage, codexHome, semantic string) (runtimeprep.CodexTurnCapabilityPromptItem, bool) {
	identity, supported := runtimeprep.CodexTuttiTurnSkillForTurnSemantic(semantic)
	if !supported {
		return runtimeprep.CodexTurnCapabilityPromptItem{}, false
	}
	var result struct {
		Data []struct {
			Skills []struct {
				Name    string `json:"name"`
				Path    string `json:"path"`
				Enabled *bool  `json:"enabled"`
			} `json:"skills"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return runtimeprep.CodexTurnCapabilityPromptItem{}, false
	}
	var matched runtimeprep.CodexTurnCapabilityPromptItem
	for _, group := range result.Data {
		for _, skill := range group.Skills {
			if strings.TrimSpace(skill.Name) != identity.Name || skill.Enabled == nil || !*skill.Enabled || !runtimeprep.ValidateCodexTuttiTurnSkill(codexHome, semantic, skill.Name, skill.Path) {
				continue
			}
			if matched.Name != "" {
				return runtimeprep.CodexTurnCapabilityPromptItem{}, false
			}
			matched = runtimeprep.CodexTurnCapabilityPromptItem{Type: "skill", Name: strings.TrimSpace(skill.Name), Path: strings.TrimSpace(skill.Path)}
		}
	}
	return matched, matched.Name != ""
}

func codexComputerAuthorized(context map[string]any) bool {
	for _, binding := range runtimeprep.CodexTurnCapabilityBindingsFromRuntimeContext(context) {
		if binding.Semantic == runtimeprep.CodexNativeCapabilityComputer && binding.Authorized {
			return true
		}
	}
	return false
}

// validateCodexTurnCapabilityRuntime verifies current App Server runtime facts.
// When pluginReady is true the caller has just checked exact plugin/list state.
func validateCodexTurnCapabilityRuntime(ctx context.Context, client *codexAppServerClient, threadID, semantic string, pluginReady bool) error {
	pluginID, ok := codexTurnCapabilityPluginID(semantic)
	if !ok {
		return fmt.Errorf("unknown Codex native capability %q", semantic)
	}
	if !pluginReady {
		if _, err := codexLivePluginVersion(ctx, client, pluginID); err != nil {
			return err
		}
	}
	typed, caller := client.typed(10*time.Second, nil, true)
	if semantic == runtimeprep.CodexNativeCapabilitySites {
		if _, err := typed.AppList(ctx, codexproto.AppsListParams{}); err != nil || !codexSitesAppVisible(caller.rawResult) {
			if err != nil {
				return codexTurnCapabilityReadinessUnknownError{err: fmt.Errorf("verify Sites app runtime: %w", err)}
			}
			return fmt.Errorf("Sites app runtime is unavailable")
		}
		return nil
	}
	if _, err := typed.McpServerStatusList(ctx, codexproto.ListMCPServerStatusParams{ThreadID: &threadID}); err != nil {
		return codexTurnCapabilityReadinessUnknownError{err: fmt.Errorf("verify Codex MCP runtime: %w", err)}
	}
	if !codexMCPAvailable(caller.rawResult, codexTurnCapabilityMCPName(semantic)) {
		return fmt.Errorf("Codex MCP runtime is unavailable")
	}
	return nil
}

func codexLivePluginVersion(ctx context.Context, client *codexAppServerClient, pluginID string) (string, error) {
	typed, caller := client.typed(10*time.Second, nil, true)
	if _, err := typed.PluginList(ctx, codexproto.PluginListParams{}); err != nil {
		return "", codexTurnCapabilityReadinessUnknownError{err: fmt.Errorf("verify Codex plugin inventory: %w", err)}
	}
	return codexPluginAvailableVersionResult(caller.rawResult, pluginID)
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

func codexTurnCapabilityMCPName(semantic string) string {
	if semantic == runtimeprep.CodexNativeCapabilityBrowser {
		return "node_repl"
	}
	return "computer-use"
}

func codexPluginAvailable(raw json.RawMessage, pluginID string) bool {
	_, err := codexPluginAvailableVersionResult(raw, pluginID)
	return err == nil
}

func codexPluginAvailableVersion(raw json.RawMessage, pluginID string) (string, bool) {
	version, err := codexPluginAvailableVersionResult(raw, pluginID)
	return version, err == nil
}

func codexPluginAvailableVersionResult(raw json.RawMessage, pluginID string) (string, error) {
	var result struct {
		Marketplaces []struct {
			Plugins []struct {
				ID             string `json:"id"`
				Installed      *bool  `json:"installed"`
				Enabled        *bool  `json:"enabled"`
				Availability   string `json:"availability"`
				InstallPolicy  string `json:"installPolicy"`
				Version        string `json:"version"`
				PackageVersion string `json:"packageVersion"`
			} `json:"plugins"`
		} `json:"marketplaces"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Marketplaces == nil {
		return "", codexTurnCapabilityReadinessUnknownError{err: fmt.Errorf("Codex plugin inventory response is invalid")}
	}
	for _, marketplace := range result.Marketplaces {
		for _, plugin := range marketplace.Plugins {
			if strings.TrimSpace(plugin.ID) != pluginID {
				continue
			}
			availability := strings.ToUpper(strings.TrimSpace(plugin.Availability))
			installPolicy := strings.ToUpper(strings.TrimSpace(plugin.InstallPolicy))
			if availability == "DISABLED_BY_ADMIN" || availability == "UNSUPPORTED" || availability == "NOT_AVAILABLE" || installPolicy == "NOT_AVAILABLE" {
				return "", codexPluginReadinessError{nextAction: "blocked", reasonCode: "plugin_blocked", err: fmt.Errorf("official Codex plugin %q is blocked", pluginID)}
			}
			if plugin.Installed == nil || plugin.Enabled == nil || availability == "" || (installPolicy != "AVAILABLE" && installPolicy != "INSTALLED_BY_DEFAULT") {
				return "", codexTurnCapabilityReadinessUnknownError{err: fmt.Errorf("Codex plugin %q inventory state is incomplete or unsupported", pluginID)}
			}
			if !*plugin.Installed {
				return "", codexPluginReadinessError{nextAction: "setup_required", reasonCode: "plugin_not_installed", err: fmt.Errorf("official Codex plugin %q is not installed", pluginID)}
			}
			if !*plugin.Enabled {
				return "", codexPluginReadinessError{nextAction: "enable_required", reasonCode: "plugin_disabled", err: fmt.Errorf("official Codex plugin %q is disabled", pluginID)}
			}
			if availability != "AVAILABLE" {
				return "", codexTurnCapabilityReadinessUnknownError{err: fmt.Errorf("Codex plugin %q availability is unknown", pluginID)}
			}
			version := strings.TrimSpace(plugin.PackageVersion)
			if version == "" {
				version = strings.TrimSpace(plugin.Version)
			}
			if version == "" {
				// The installed identity itself is still a generation-local cache key
				// when older app-server builds omit version fields. It is not a digest.
				version = pluginID
			}
			return version, nil
		}
	}
	return "", codexTurnCapabilityReadinessUnknownError{err: fmt.Errorf("official Codex plugin %q was not present in the current inventory", pluginID)}
}

func codexSitesAppVisible(raw json.RawMessage) bool {
	var result struct {
		Data []struct {
			ID           string `json:"id"`
			IsEnabled    *bool  `json:"isEnabled"`
			IsAccessible *bool  `json:"isAccessible"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return false
	}
	for _, app := range result.Data {
		if strings.TrimSpace(app.ID) == "sites" && app.IsEnabled != nil && app.IsAccessible != nil {
			return *app.IsEnabled && *app.IsAccessible
		}
	}
	return false
}

func codexMCPAvailable(raw json.RawMessage, name string) bool {
	var value struct {
		Data []struct {
			Name       string `json:"name"`
			ServerName string `json:"serverName"`
			AuthStatus string `json:"authStatus"`
			Tools      any    `json:"tools"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	for _, item := range value.Data {
		identity := strings.TrimSpace(item.Name)
		if identity == "" {
			identity = strings.TrimSpace(item.ServerName)
		}
		if identity == name && codexMCPAuthRunnable(item.AuthStatus) && codexMCPToolsAvailable(item.Tools) {
			return true
		}
	}
	return false
}

func codexMCPAuthRunnable(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	// bearerToken and oAuth are the protocol's authenticated states. unsupported
	// means this server does not require an auth flow, so non-empty tools remain
	// the concrete runtime evidence. notLoggedIn is intentionally rejected.
	case "bearertoken", "oauth", "unsupported":
		return true
	default:
		return false
	}
}

func codexMCPToolsAvailable(value any) bool {
	switch tools := value.(type) {
	case map[string]any:
		return len(tools) > 0
	case []any:
		return len(tools) > 0
	default:
		return false
	}
}
