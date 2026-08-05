package agent

import (
	"context"
	"errors"
	"testing"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
	agentactivitybiz "github.com/tutti-os/tutti/services/tuttid/biz/agentactivity"
	agenttargetbiz "github.com/tutti-os/tutti/services/tuttid/biz/agenttarget"
	tuttimodeactivationbiz "github.com/tutti-os/tutti/services/tuttid/biz/tuttimodeactivation"
)

type turnCapabilityRecordingRuntime struct {
	*fakeRuntime
	sequence     []string
	ensureCalls  []agenthost.RuntimeTurnCapabilityInput
	ensureResult agenthost.RuntimeTurnCapabilityResult
}

type serviceHostRuntimeWithTurnCapabilities struct{ serviceHostRuntime }

func (a serviceHostRuntimeWithTurnCapabilities) EnsureTurnCapability(ctx context.Context, input agenthost.RuntimeTurnCapabilityInput) (agenthost.RuntimeTurnCapabilityResult, error) {
	port, ok := a.service.controller().(agenthost.RuntimeTurnCapabilityPort)
	if !ok {
		return agenthost.RuntimeTurnCapabilityResult{Disposition: agenthost.RuntimeTurnCapabilityRejected}, nil
	}
	return port.EnsureTurnCapability(ctx, input)
}

func (r *turnCapabilityRecordingRuntime) EnsureTurnCapability(_ context.Context, input agenthost.RuntimeTurnCapabilityInput) (agenthost.RuntimeTurnCapabilityResult, error) {
	r.sequence = append(r.sequence, "ensure")
	r.ensureCalls = append(r.ensureCalls, input)
	if r.ensureResult.Disposition != "" {
		return r.ensureResult, nil
	}
	return agenthost.RuntimeTurnCapabilityResult{
		Disposition: agenthost.RuntimeTurnCapabilityApplied,
		PromptAugmentation: []agenthost.PromptContentBlock{{
			Type: "mention", Name: "browser@openai-bundled", Path: "plugin://browser@openai-bundled",
		}},
	}, nil
}

func TestSendInputProjectsSetupOutcomeAndRetainsClientSubmitIDForRetry(t *testing.T) {
	t.Parallel()
	service, runtime := newTurnCapabilityService(t, "codex")
	runtime.ensureResult = agenthost.RuntimeTurnCapabilityResult{
		Disposition: agenthost.RuntimeTurnCapabilityRejected,
		Outcome: &agenthost.RuntimeTurnCapabilityOutcome{
			NextAction: agenthost.RuntimeTurnCapabilityNextActionSetup,
			ReasonCode: "plugin_not_ready",
		},
	}
	input := SendInput{
		ClientSubmitID: "submit-browser-setup",
		Content:        []PromptContentBlock{{Type: "text", Text: "open browser"}},
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{
			Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse,
		},
	}
	_, err := service.SendInput(context.Background(), "workspace-1", "session-1", input)
	var recovery *TurnCapabilityRecoveryError
	if !errors.As(err, &recovery) {
		t.Fatalf("SendInput error = %v, want recovery outcome", err)
	}
	if recovery.NextAction != string(agenthost.RuntimeTurnCapabilityNextActionSetup) ||
		recovery.ReasonCode != "plugin_not_ready" {
		t.Fatalf("recovery = %#v", recovery)
	}
	if len(runtime.execCalls) != 0 || len(runtime.ensureCalls) != 1 {
		t.Fatalf("setup outcome must stop before Exec: ensure=%d exec=%d", len(runtime.ensureCalls), len(runtime.execCalls))
	}
	runtime.ensureResult = agenthost.RuntimeTurnCapabilityResult{}
	result, err := service.SendInput(context.Background(), "workspace-1", "session-1", input)
	if err != nil {
		t.Fatalf("same client submit retry: %v", err)
	}
	if result.TurnID == "" || len(runtime.ensureCalls) != 2 || len(runtime.execCalls) != 1 {
		t.Fatalf("retry must use one delivery: result=%#v ensure=%d exec=%d", result, len(runtime.ensureCalls), len(runtime.execCalls))
	}
}

func (r *turnCapabilityRecordingRuntime) Exec(ctx context.Context, input RuntimeExecInput) (RuntimeExecResult, error) {
	r.sequence = append(r.sequence, "exec")
	return r.fakeRuntime.Exec(ctx, input)
}

func newTurnCapabilityService(t *testing.T, provider string) (*Service, *turnCapabilityRecordingRuntime) {
	t.Helper()
	agentTargetID := agenttargetbiz.IDLocalClaudeCode
	if provider == "codex" {
		agentTargetID = agenttargetbiz.IDLocalCodex
	}
	runtime := &turnCapabilityRecordingRuntime{fakeRuntime: newFakeRuntime()}
	runtime.sessions["workspace-1:session-1"] = ProviderRuntimeSession{
		ID: "session-1", WorkspaceID: "workspace-1", AgentTargetID: agentTargetID, Provider: provider,
		ProviderSessionID: "provider-session-1", Status: "ready", Resumable: true,
	}
	service := NewService(runtime)
	// Capability admission reads the canonical mode under the Host session
	// lock. Test sessions therefore provide an explicit inactive projection;
	// production must fail closed when that authority is absent.
	service.TuttiModeActivations = &fakeTuttiModeActivationCoordinator{
		current: activationSnapshot("activation-1", "revision-1", 1, tuttimodeactivationbiz.StateInactive, tuttimodeactivationbiz.SourceBadgeRemove),
	}
	service.AgentTargetStore = fakeAgentTargetStore{targets: defaultTestAgentTargets()}
	service.CapabilityLister = isolatedComposerCapabilityLister{}
	installFakeCanonicalSessionStore(service)
	reader := service.SessionReader.(*fakeSessionReader)
	reader.sessions["workspace-1:session-1"] = PersistedSession{
		ID: "session-1", WorkspaceID: "workspace-1", AgentTargetID: agentTargetID, Provider: provider,
		ProviderSessionID: "provider-session-1", Settings: ComposerSettings{},
	}
	service.SubmitClaimStore = openAgentServiceSQLiteStore(t)
	turns := &legacyHostConformanceTurnStore{
		sessions: map[string]agentactivitybiz.Session{}, turns: map[string]agentactivitybiz.Turn{}, interactions: map[string][]agentactivitybiz.Interaction{},
	}
	service.TurnStore = turns
	runtime.provenanceHook = func(input RuntimeSubmitProvenanceInput) error {
		turns.turns[input.AgentSessionID+":"+input.TurnID] = agentactivitybiz.Turn{
			WorkspaceID: input.WorkspaceID, AgentSessionID: input.AgentSessionID, TurnID: input.TurnID,
			Phase: agentactivitybiz.TurnPhaseSubmitted,
		}
		return nil
	}
	store := serviceHostStore{service: service}
	service.SetApplicationHost(composeApplicationHost(
		service, service, store, store, store, nil,
		serviceHostRuntimeWithTurnCapabilities{serviceHostRuntime{service: service}},
		serviceHostGoalRuntime{service: service},
	))
	return service, runtime
}

func TestSendInputRejectsTurnCapabilityForNonCodexBeforePreparation(t *testing.T) {
	t.Parallel()
	service, runtime := newTurnCapabilityService(t, "claude-code")
	activation := &fakeTuttiModeActivationCoordinator{}
	service.TuttiModeActivations = activation

	_, err := service.SendInput(context.Background(), "workspace-1", "session-1", SendInput{
		ClientSubmitID: "submit-1",
		Content:        []PromptContentBlock{{Type: "text", Text: "use browser"}},
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{
			Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse,
		},
	})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("SendInput error = %v, want invalid argument", err)
	}
	if activation.currentCalls != 0 || activation.boundTurnID != "" {
		t.Fatalf("non-Codex capability prepared Tutti mode: %#v", activation)
	}
	if len(runtime.execCalls) != 0 {
		t.Fatalf("non-Codex capability executed: %#v", runtime.execCalls)
	}
	if len(runtime.ensureCalls) != 0 {
		t.Fatalf("non-Codex capability reached Ensure: %#v", runtime.ensureCalls)
	}
}

func TestSendInputRejectsCodexNamedCustomTargetBeforePreparation(t *testing.T) {
	t.Parallel()
	service, runtime := newTurnCapabilityService(t, "codex")
	const customTargetID = "extension:codex-compatible"
	service.AgentTargetStore = fakeAgentTargetStore{targets: map[string]agenttargetbiz.Target{
		customTargetID: {
			ID: customTargetID, Provider: "codex", Enabled: true, Source: agenttargetbiz.SourceUser,
			Name: "Custom Codex", LaunchRefJSON: agenttargetbiz.MustLocalCLILaunchRefJSON("codex"),
		},
	}}
	runtime.sessions["workspace-1:session-1"] = ProviderRuntimeSession{
		ID: "session-1", WorkspaceID: "workspace-1", AgentTargetID: customTargetID, Provider: "codex",
		ProviderSessionID: "provider-session-1", Status: "ready", Resumable: true,
	}
	reader := service.SessionReader.(*fakeSessionReader)
	reader.sessions["workspace-1:session-1"] = PersistedSession{
		ID: "session-1", WorkspaceID: "workspace-1", AgentTargetID: customTargetID, Provider: "codex",
		ProviderSessionID: "provider-session-1", Settings: ComposerSettings{},
	}
	activation := &fakeTuttiModeActivationCoordinator{}
	service.TuttiModeActivations = activation

	_, err := service.SendInput(context.Background(), "workspace-1", "session-1", SendInput{
		ClientSubmitID: "submit-browser",
		Content:        []PromptContentBlock{{Type: "text", Text: "use browser"}},
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{
			Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse,
		},
	})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("SendInput error = %v, want invalid argument", err)
	}
	if activation.currentCalls != 0 || len(runtime.ensureCalls) != 0 || len(runtime.execCalls) != 0 {
		t.Fatalf("custom Codex-named target had effects: activation=%#v ensure=%#v exec=%#v", activation, runtime.ensureCalls, runtime.execCalls)
	}
}

func TestSendInputActiveTuttiModeUsesOneTurnScopedCapabilitySnapshot(t *testing.T) {
	t.Parallel()
	service, runtime := newTurnCapabilityService(t, "codex")
	activation := &fakeTuttiModeActivationCoordinator{
		current: activationSnapshot("activation-1", "revision-1", 1, tuttimodeactivationbiz.StateActive, tuttimodeactivationbiz.SourceSlashCommand),
	}
	service.TuttiModeActivations = activation

	_, err := service.SendInput(context.Background(), "workspace-1", "session-1", SendInput{
		ClientSubmitID: "submit-browser", Content: []PromptContentBlock{{Type: "text", Text: "open browser"}},
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse},
	})
	if err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	if activation.boundTurnID == "" || activation.acceptedTurnID != activation.boundTurnID || len(runtime.ensureCalls) != 1 || len(runtime.updateSettingsCalls) != 0 || len(runtime.execCalls) != 1 {
		t.Fatalf("active mode must use the bound per-turn plan without settings leakage: activation=%#v ensure=%#v settings=%#v exec=%#v", activation, runtime.ensureCalls, runtime.updateSettingsCalls, runtime.execCalls)
	}
}

func TestSendInputNativeCapabilityBindsAndAcceptsItsExecutionSnapshot(t *testing.T) {
	t.Parallel()
	service, runtime := newTurnCapabilityService(t, "codex")
	activation := &fakeTuttiModeActivationCoordinator{
		current: activationSnapshot("activation-1", "revision-1", 1, tuttimodeactivationbiz.StateInactive, tuttimodeactivationbiz.SourceBadgeRemove),
	}
	service.TuttiModeActivations = activation

	if _, err := service.SendInput(context.Background(), "workspace-1", "session-1", SendInput{
		ClientSubmitID: "submit-browser", Content: []PromptContentBlock{{Type: "text", Text: "open browser"}},
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse},
	}); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	if activation.boundTurnID == "" || activation.acceptedTurnID != activation.boundTurnID || len(runtime.ensureCalls) != 1 || len(runtime.execCalls) != 1 {
		t.Fatalf("native capability must preserve one frozen execution snapshot: activation=%#v ensure=%#v exec=%#v", activation, runtime.ensureCalls, runtime.execCalls)
	}
}

func TestSendInputActiveTuttiModeRoutesBrowserThroughOneNativeEnsure(t *testing.T) {
	t.Parallel()
	service, runtime := newTurnCapabilityService(t, "codex")
	service.TuttiModeActivations = &fakeTuttiModeActivationCoordinator{
		current: activationSnapshot("activation-1", "revision-1", 1, tuttimodeactivationbiz.StateActive, tuttimodeactivationbiz.SourceSlashCommand),
	}
	_, err := service.SendInput(context.Background(), "workspace-1", "session-1", SendInput{
		ClientSubmitID: "submit-tutti-browser", Content: []PromptContentBlock{{Type: "text", Text: "open browser"}},
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse},
	})
	if err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	if len(runtime.ensureCalls) != 1 || runtime.ensureCalls[0].Plan.Key != codexNativeTurnCapabilityPlanKey || len(runtime.updateSettingsCalls) != 0 || len(runtime.execCalls) != 1 {
		t.Fatalf("active Tutti Browser must use the native Turn plan without settings mutation: capability=%#v settings=%#v exec=%#v", runtime.ensureCalls, runtime.updateSettingsCalls, runtime.execCalls)
	}
}

// Tutti Mode never changes a Codex capability backend. Switching it between
// Turns must neither rewrite session settings nor resume/replace the loaded
// runtime, and every capability Turn remains native.
func TestSendInputKeepsCodexCapabilityNativeAcrossTuttiModeChanges(t *testing.T) {
	t.Parallel()
	service, runtime := newTurnCapabilityService(t, "codex")
	activation := &fakeTuttiModeActivationCoordinator{
		current: activationSnapshot("activation-1", "revision-1", 1, tuttimodeactivationbiz.StateInactive, tuttimodeactivationbiz.SourceBadgeRemove),
	}
	service.TuttiModeActivations = activation

	sendBrowser := func(clientSubmitID string) {
		t.Helper()
		result, err := service.SendInput(context.Background(), "workspace-1", "session-1", SendInput{
			ClientSubmitID: clientSubmitID,
			Content:        []PromptContentBlock{{Type: "text", Text: "open browser"}},
			TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{
				Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse,
			},
		})
		if err != nil {
			t.Fatalf("SendInput(%q): %v", clientSubmitID, err)
		}
		if result.Session.ID != "session-1" || result.Session.ProviderSessionID != "provider-session-1" {
			t.Fatalf("SendInput(%q) session = %#v, want existing runtime session", clientSubmitID, result.Session)
		}
	}

	sendBrowser("submit-native-before")
	activation.current = activationSnapshot("activation-1", "revision-2", 2, tuttimodeactivationbiz.StateActive, tuttimodeactivationbiz.SourceSlashCommand)
	sendBrowser("submit-native-while-tutti")
	activation.current = activationSnapshot("activation-1", "revision-3", 3, tuttimodeactivationbiz.StateInactive, tuttimodeactivationbiz.SourceBadgeRemove)
	sendBrowser("submit-native-after")

	if got, want := len(runtime.ensureCalls), 3; got != want {
		t.Fatalf("Ensure calls = %d, want %d", got, want)
	}
	plans := []string{
		runtime.ensureCalls[0].Plan.Key,
		runtime.ensureCalls[1].Plan.Key,
		runtime.ensureCalls[2].Plan.Key,
	}
	wantPlans := []string{codexNativeTurnCapabilityPlanKey, codexNativeTurnCapabilityPlanKey, codexNativeTurnCapabilityPlanKey}
	for index, want := range wantPlans {
		if plans[index] != want {
			t.Fatalf("Ensure plan[%d] = %q, want %q (all=%#v)", index, plans[index], want, plans)
		}
	}
	if got := len(runtime.resumeCalls); got != 0 {
		t.Fatalf("backend switching resumed/replaced the loaded runtime %d times", got)
	}
	if got := len(runtime.updateSettingsCalls); got != 0 {
		t.Fatalf("backend switching mutated session settings %d times", got)
	}
	if got := len(runtime.execCalls); got != 3 {
		t.Fatalf("Exec calls = %d, want one for each turn", got)
	}
}

func TestSendInputActiveTuttiModeRoutesSitesThroughNativeEnsure(t *testing.T) {
	t.Parallel()
	service, runtime := newTurnCapabilityService(t, "codex")
	service.TuttiModeActivations = &fakeTuttiModeActivationCoordinator{
		current: activationSnapshot("activation-1", "revision-1", 1, tuttimodeactivationbiz.StateActive, tuttimodeactivationbiz.SourceSlashCommand),
	}
	_, err := service.SendInput(context.Background(), "workspace-1", "session-1", SendInput{
		ClientSubmitID: "submit-tutti-sites", Content: []PromptContentBlock{{Type: "text", Text: "build a site"}},
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{Semantic: runtimeprep.CodexTurnCapabilitySemanticSites},
	})
	if err != nil {
		t.Fatalf("SendInput error = %v", err)
	}
	if len(runtime.ensureCalls) != 1 || runtime.ensureCalls[0].Plan.Key != codexNativeTurnCapabilityPlanKey || len(runtime.updateSettingsCalls) != 0 || len(runtime.execCalls) != 1 {
		t.Fatalf("active Tutti Sites must use native capability: native=%#v settings=%#v exec=%#v", runtime.ensureCalls, runtime.updateSettingsCalls, runtime.execCalls)
	}
}

func TestCreateRejectsTurnCapabilityForNonCodexBeforeRuntimeStart(t *testing.T) {
	t.Parallel()
	runtime := newFakeRuntime()
	service := newTestService(runtime)

	_, err := service.Create(context.Background(), "workspace-1", CreateSessionInput{
		AgentSessionID: "11111111-1111-4111-8111-111111111111",
		AgentTargetID:  agenttargetbiz.IDLocalClaudeCode,
		ClientSubmitID: "submit-browser",
		InitialContent: TextPromptContent("open browser"),
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{
			Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse,
		},
	})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Create error = %v, want invalid argument", err)
	}
	if len(runtime.startCalls) != 0 || len(runtime.execCalls) != 0 {
		t.Fatalf("non-Codex create had runtime effects: start=%#v exec=%#v", runtime.startCalls, runtime.execCalls)
	}
}

func TestCreateRejectsCodexNamedCustomTargetBeforeRuntimeStart(t *testing.T) {
	t.Parallel()
	runtime := newFakeRuntime()
	service := newTestService(runtime)
	const customTargetID = "extension:codex-compatible"
	service.AgentTargetStore = fakeAgentTargetStore{targets: map[string]agenttargetbiz.Target{
		customTargetID: {
			ID: customTargetID, Provider: "codex", Enabled: true, Source: agenttargetbiz.SourceUser,
			Name: "Custom Codex", LaunchRefJSON: agenttargetbiz.MustLocalCLILaunchRefJSON("codex"),
		},
	}}

	_, err := service.Create(context.Background(), "workspace-1", CreateSessionInput{
		AgentSessionID: "11111111-1111-4111-8111-111111111111",
		AgentTargetID:  customTargetID,
		ClientSubmitID: "submit-browser",
		InitialContent: TextPromptContent("open browser"),
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{
			Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse,
		},
	})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Create error = %v, want invalid argument", err)
	}
	if len(runtime.startCalls) != 0 || len(runtime.execCalls) != 0 {
		t.Fatalf("custom Codex-named target had runtime effects: start=%#v exec=%#v", runtime.startCalls, runtime.execCalls)
	}
}

func TestCreateRejectsStaleNativeCapabilityWhenInitialTuttiModeIsActive(t *testing.T) {
	t.Parallel()
	runtime := newFakeRuntime()
	service := newTestService(runtime)
	_, err := service.Create(context.Background(), "workspace-1", CreateSessionInput{
		AgentSessionID: "11111111-1111-4111-8111-111111111111",
		AgentTargetID:  agenttargetbiz.IDLocalCodex,
		ClientSubmitID: "submit-browser",
		InitialContent: TextPromptContent("open browser"),
		InitialTuttiModeActivation: &TuttiModeActivationIntent{
			State: string(tuttimodeactivationbiz.StateActive), Source: string(tuttimodeactivationbiz.SourceSlashCommand),
		},
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse},
	})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Create error = %v, want invalid argument", err)
	}
	if len(runtime.startCalls) != 0 || len(runtime.execCalls) != 0 {
		t.Fatalf("stale native create had runtime effects: start=%#v exec=%#v", runtime.startCalls, runtime.execCalls)
	}
}

func TestCreateDefersComputerConsentToHostCapabilityEnsure(t *testing.T) {
	t.Parallel()
	service, runtime := newTurnCapabilityService(t, "codex")
	activation := &fakeTuttiModeActivationCoordinator{}
	service.TuttiModeActivations = activation
	var prepareInput runtimeprep.PrepareInput
	service.RuntimePreparer = fakeRuntimePreparer{
		input:  &prepareInput,
		result: runtimeprep.PreparedRuntime{Cwd: "/workspace"},
	}

	_, err := service.Create(context.Background(), "workspace-1", CreateSessionInput{
		AgentSessionID: "11111111-1111-4111-8111-111111111111",
		AgentTargetID:  agenttargetbiz.IDLocalCodex,
		ClientSubmitID: "submit-computer",
		InitialContent: TextPromptContent("use computer"),
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{
			Semantic: runtimeprep.CodexTurnCapabilitySemanticComputerUse,
			Consent:  agenthost.TurnCapabilityConsentExplicitSession,
		},
		InitialTuttiModeActivation: &TuttiModeActivationIntent{
			State: string(tuttimodeactivationbiz.StateInactive), Source: string(tuttimodeactivationbiz.SourceBadgeRemove),
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if prepareInput.AuthorizeCodexNativeComputerUse {
		t.Fatal("service runtime preparation must not authorize Computer Use before the Host claim")
	}
	if len(activation.setInputs) != 1 || activation.setInputs[0].State != tuttimodeactivationbiz.StateInactive {
		t.Fatalf("native create must persist its independent Tutti activation: %#v", activation.setInputs)
	}
	if len(runtime.ensureCalls) != 1 ||
		runtime.ensureCalls[0].Invocation.Semantic != runtimeprep.CodexTurnCapabilitySemanticComputerUse ||
		runtime.ensureCalls[0].Invocation.Consent != agenthost.TurnCapabilityConsentExplicitSession {
		t.Fatalf("Host Ensure capability input = %#v", runtime.ensureCalls)
	}
	if len(runtime.execCalls) != 1 || len(runtime.sequence) != 2 ||
		runtime.sequence[0] != "ensure" || runtime.sequence[1] != "exec" {
		t.Fatalf("expected Host Ensure then Exec once, sequence=%#v exec=%#v", runtime.sequence, runtime.execCalls)
	}
}

func TestCreateActiveTuttiCapabilityCleansActivationOnlyBeforeDelivery(t *testing.T) {
	newInput := func(sessionID, submitID string) CreateSessionInput {
		return CreateSessionInput{
			AgentSessionID: sessionID, AgentTargetID: agenttargetbiz.IDLocalCodex, ClientSubmitID: submitID,
			InitialContent: TextPromptContent("open browser"),
			InitialTuttiModeActivation: &TuttiModeActivationIntent{
				State: string(tuttimodeactivationbiz.StateActive), Source: string(tuttimodeactivationbiz.SourceSlashCommand),
			},
			TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse},
		}
	}
	newService := func(t *testing.T) (*Service, *turnCapabilityRecordingRuntime, *fakeTuttiModeActivationCoordinator) {
		t.Helper()
		service, runtime := newTurnCapabilityService(t, "codex")
		activation := &fakeTuttiModeActivationCoordinator{
			current: activationSnapshot("activation-1", "revision-1", 1, tuttimodeactivationbiz.StateActive, tuttimodeactivationbiz.SourceSlashCommand),
		}
		service.TuttiModeActivations = activation
		return service, runtime, activation
	}
	claimExists := func(t *testing.T, service *Service, sessionID, submitID string) bool {
		t.Helper()
		_, found, err := service.SubmitClaimStore.GetSubmitClaim(context.Background(), "workspace-1", sessionID, submitID)
		if err != nil {
			t.Fatal(err)
		}
		return found
	}

	t.Run("recovery abandons the turn snapshot and retains the session activation", func(t *testing.T) {
		service, runtime, activation := newService(t)
		runtime.ensureResult = agenthost.RuntimeTurnCapabilityResult{Disposition: agenthost.RuntimeTurnCapabilityRejected, Outcome: &agenthost.RuntimeTurnCapabilityOutcome{NextAction: agenthost.RuntimeTurnCapabilityNextActionSetup, ReasonCode: "plugin_not_installed"}}
		created, err := service.CreateWithResult(context.Background(), "workspace-1", newInput("create-recovery", "submit-recovery"))
		var recovery *TurnCapabilityRecoveryError
		if !errors.As(err, &recovery) {
			t.Fatalf("Create error = %v, want recovery", err)
		}
		if created.Session.ID != "create-recovery" {
			t.Fatalf("CreateWithResult session = %#v, want retained session", created.Session)
		}
		if activation.boundTurnID == "" || activation.abandonedTurnID != activation.boundTurnID || len(activation.deleteSessionIDs) != 0 {
			t.Fatalf("activation cleanup = %#v", activation)
		}
		if _, found := runtime.Session("workspace-1", "create-recovery"); !found || claimExists(t, service, "create-recovery", "submit-recovery") {
			t.Fatalf("recovery session or claim state: session=%v claim=%v", found, claimExists(t, service, "create-recovery", "submit-recovery"))
		}
	})

	t.Run("rejected capability abandons snapshot and activation", func(t *testing.T) {
		service, runtime, activation := newService(t)
		runtime.ensureResult = agenthost.RuntimeTurnCapabilityResult{Disposition: agenthost.RuntimeTurnCapabilityRejected}
		_, err := service.Create(context.Background(), "workspace-1", newInput("create-rejected", "submit-rejected"))
		if !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("Create error = %v, want invalid argument", err)
		}
		if activation.boundTurnID == "" || activation.abandonedTurnID != activation.boundTurnID || len(activation.deleteSessionIDs) != 1 {
			t.Fatalf("activation cleanup = %#v", activation)
		}
		if _, found := runtime.Session("workspace-1", "create-rejected"); found || claimExists(t, service, "create-rejected", "submit-rejected") {
			t.Fatalf("rejected Create retained session or claim: session=%v claim=%v", found, claimExists(t, service, "create-rejected", "submit-rejected"))
		}
	})

	t.Run("delivery unknown retains every durable fence", func(t *testing.T) {
		service, runtime, activation := newService(t)
		runtime.ensureResult = agenthost.RuntimeTurnCapabilityResult{Disposition: agenthost.RuntimeTurnCapabilityUnknown}
		_, err := service.Create(context.Background(), "workspace-1", newInput("create-unknown", "submit-unknown"))
		if !errors.Is(err, ErrSubmitDeliveryUnknown) {
			t.Fatalf("Create error = %v, want delivery unknown", err)
		}
		if activation.boundTurnID == "" || activation.abandonedTurnID != "" || activation.acceptedTurnID != "" || len(activation.deleteSessionIDs) != 0 {
			t.Fatalf("delivery-unknown activation fence = %#v", activation)
		}
		if _, found := runtime.Session("workspace-1", "create-unknown"); !found || !claimExists(t, service, "create-unknown", "submit-unknown") {
			t.Fatalf("delivery unknown lost session or claim: session=%v claim=%v", found, claimExists(t, service, "create-unknown", "submit-unknown"))
		}
	})
}

func TestComposedHostEnsuresSecondCodexCapabilityTurnExactlyOnce(t *testing.T) {
	t.Parallel()
	service, runtime := newTurnCapabilityService(t, "codex")
	first, err := service.SendInput(context.Background(), "workspace-1", "session-1", SendInput{
		ClientSubmitID: "submit-normal", Content: []PromptContentBlock{{Type: "text", Text: "ordinary message"}},
	})
	if err != nil || first.Session.ID != "session-1" || first.Session.ProviderSessionID != "provider-session-1" {
		t.Fatalf("first send = %#v, error = %v", first, err)
	}
	second, err := service.SendInput(context.Background(), "workspace-1", "session-1", SendInput{
		ClientSubmitID: "submit-browser", TurnID: "turn-browser", Content: []PromptContentBlock{{Type: "text", Text: "open browser"}},
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse},
	})
	if err != nil || second.Session.ID != "session-1" || second.Session.ProviderSessionID != "provider-session-1" {
		t.Fatalf("capability send = %#v, error = %v", second, err)
	}
	if got, want := runtime.sequence, []string{"exec", "ensure", "exec"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("runtime sequence = %#v, want %#v", got, want)
	}
	if len(runtime.ensureCalls) != 1 || runtime.ensureCalls[0].AgentSessionID != "session-1" || runtime.ensureCalls[0].Invocation.Semantic != runtimeprep.CodexTurnCapabilitySemanticBrowserUse {
		t.Fatalf("ensure calls = %#v", runtime.ensureCalls)
	}
	if len(runtime.execCalls) != 2 {
		t.Fatalf("exec calls = %#v", runtime.execCalls)
	}
	if _, err := service.SendInput(context.Background(), "workspace-1", "session-1", SendInput{
		ClientSubmitID: "submit-browser", TurnID: "turn-browser", Content: []PromptContentBlock{{Type: "text", Text: "open browser"}},
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse},
	}); err != nil {
		t.Fatalf("duplicate capability send: %v", err)
	}
	if len(runtime.ensureCalls) != 1 || len(runtime.execCalls) != 2 {
		t.Fatalf("duplicate replayed side effects: ensures=%#v execs=%#v", runtime.ensureCalls, runtime.execCalls)
	}
}

func TestComposedHostRestoresCanonicalTurnIDForDuplicateCapabilitySubmitWithoutCallerTurnID(t *testing.T) {
	t.Parallel()
	service, runtime := newTurnCapabilityService(t, "codex")
	service.TuttiModeActivations = &fakeTuttiModeActivationCoordinator{
		current: activationSnapshot("activation-1", "revision-1", 1, tuttimodeactivationbiz.StateInactive, tuttimodeactivationbiz.SourceBadgeRemove),
	}
	input := SendInput{
		ClientSubmitID: "submit-browser-without-turn-id",
		Content:        []PromptContentBlock{{Type: "text", Text: "open browser"}},
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{
			Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse,
		},
	}
	first, err := service.SendInput(context.Background(), "workspace-1", "session-1", input)
	if err != nil {
		t.Fatalf("first SendInput: %v", err)
	}
	if first.TurnID == "" {
		t.Fatal("first SendInput returned an empty canonical turn id")
	}
	second, err := service.SendInput(context.Background(), "workspace-1", "session-1", input)
	if err != nil {
		t.Fatalf("duplicate SendInput: %v", err)
	}
	if second.TurnID != first.TurnID {
		t.Fatalf("duplicate turn id = %q, want canonical %q", second.TurnID, first.TurnID)
	}
	if len(runtime.ensureCalls) != 1 || len(runtime.execCalls) != 1 {
		t.Fatalf("duplicate replayed side effects: ensures=%d execs=%d", len(runtime.ensureCalls), len(runtime.execCalls))
	}
}
