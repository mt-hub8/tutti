package agenthost_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	hostconformance "github.com/tutti-os/tutti/packages/agent/host/conformance"
	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
	_ "modernc.org/sqlite"
)

func TestTurnCapabilityConformance(t *testing.T) {
	for _, scenario := range hostconformance.TurnCapabilityScenarios() {
		scenario := scenario
		t.Run(scenario.Name, func(t *testing.T) {
			driver := &turnCapabilityConformanceDriver{t: t}
			if err := hostconformance.RunTurnCapability(t.Context(), driver, scenario); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTurnCapabilityAcceptedRetryDoesNotEnsureOrExecAgain(t *testing.T) {
	driver := &turnCapabilityConformanceDriver{t: t}
	if err := driver.ResetTurnCapability(t.Context(), hostconformance.TurnCapabilityFixture{}); err != nil {
		t.Fatal(err)
	}
	input := turnCapabilityInput("turn-capability", "submit-capability")
	if _, err := driver.SendTurnCapability(t.Context(), input); err != nil {
		t.Fatalf("first SendInput() error = %v", err)
	}
	if _, err := driver.SendTurnCapability(t.Context(), input); err != nil {
		t.Fatalf("accepted retry SendInput() error = %v", err)
	}
	if got := driver.TurnCapabilityMetrics(); got.EnsureCalls != 1 || got.ExecCalls != 1 {
		t.Fatalf("accepted retry metrics=%#v", got)
	}
}

func TestTurnCapabilityPlanPersistsBeforeUncertainEnsure(t *testing.T) {
	driver := &turnCapabilityConformanceDriver{t: t}
	if err := driver.ResetTurnCapability(t.Context(), hostconformance.TurnCapabilityFixture{
		AdmissionPlan: agenthost.RuntimeTurnCapabilityPlan{Key: "persisted-plan"},
		EnsureResults: []agenthost.RuntimeTurnCapabilityResult{{Disposition: agenthost.RuntimeTurnCapabilityUnknown}},
	}); err != nil {
		t.Fatal(err)
	}
	input := turnCapabilityInput("turn-persisted-plan", "submit-persisted-plan")
	if _, err := driver.SendTurnCapability(t.Context(), input); !errors.Is(err, agenthost.ErrSubmitDeliveryUnknown) {
		t.Fatalf("SendInput() error = %v", err)
	}
	claim, found, err := driver.store.GetSubmitClaim(t.Context(), "workspace-capability", "session-capability", "submit-persisted-plan")
	if err != nil || !found || !strings.Contains(claim.TurnCapabilityPlanJSON, "persisted-plan") {
		t.Fatalf("durable claim = %#v found=%v error=%v", claim, found, err)
	}
}

func TestTurnCapabilityRejectedCanRetry(t *testing.T) {
	driver := &turnCapabilityConformanceDriver{t: t}
	if err := driver.ResetTurnCapability(t.Context(), hostconformance.TurnCapabilityFixture{
		EnsureResults: []agenthost.RuntimeTurnCapabilityResult{{
			Disposition: agenthost.RuntimeTurnCapabilityRejected,
		}, {Disposition: agenthost.RuntimeTurnCapabilityApplied}},
	}); err != nil {
		t.Fatal(err)
	}
	input := turnCapabilityInput("turn-rejected", "submit-rejected")
	if _, err := driver.SendTurnCapability(t.Context(), input); !errors.Is(err, agenthost.ErrTurnCapabilityRejected) {
		t.Fatalf("rejected SendInput() error = %v", err)
	}
	if _, err := driver.SendTurnCapability(t.Context(), input); err != nil {
		t.Fatalf("retry SendInput() error = %v", err)
	}
	if got := driver.TurnCapabilityMetrics(); got.EnsureCalls != 2 || got.ExecCalls != 1 {
		t.Fatalf("rejected retry metrics=%#v", got)
	}
}

func TestTurnCapabilityRejectedOutcomeReleasesClaimForSetupRetry(t *testing.T) {
	driver := &turnCapabilityConformanceDriver{t: t}
	if err := driver.ResetTurnCapability(t.Context(), hostconformance.TurnCapabilityFixture{EnsureResults: []agenthost.RuntimeTurnCapabilityResult{{
		Disposition: agenthost.RuntimeTurnCapabilityRejected,
		Outcome: &agenthost.RuntimeTurnCapabilityOutcome{
			NextAction: agenthost.RuntimeTurnCapabilityNextActionSetup,
			ReasonCode: "plugin_not_ready",
		},
	}, turnCapabilityAppliedResult(agenthost.RuntimeTurnCapabilityAlreadyBound)}}); err != nil {
		t.Fatal(err)
	}
	input := agenthost.SendInput{
		Content:                  []agenthost.PromptContentBlock{{Type: "text", Text: "task"}},
		ClientSubmitID:           "setup-retry-submit",
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{Semantic: "conformance-capability"},
	}
	_, err := driver.host.SendInput(t.Context(), agenthost.SessionRef{WorkspaceID: "workspace-capability", AgentSessionID: "session-capability"}, input)
	var outcome *agenthost.TurnCapabilityOutcomeError
	if !errors.As(err, &outcome) || outcome.Outcome.NextAction != agenthost.RuntimeTurnCapabilityNextActionSetup {
		t.Fatalf("first error = %v, outcome = %#v", err, outcome)
	}
	if _, err := driver.host.SendInput(t.Context(), agenthost.SessionRef{WorkspaceID: "workspace-capability", AgentSessionID: "session-capability"}, input); err != nil {
		t.Fatalf("retry = %v", err)
	}
	if got := driver.TurnCapabilityMetrics(); got.EnsureCalls != 2 || got.ExecCalls != 1 {
		t.Fatalf("metrics = %#v", got)
	}
}

func TestTurnCapabilityRetryableUnknownReleasesClaimBeforeExec(t *testing.T) {
	driver := &turnCapabilityConformanceDriver{t: t}
	if err := driver.ResetTurnCapability(t.Context(), hostconformance.TurnCapabilityFixture{
		EnsureResults: []agenthost.RuntimeTurnCapabilityResult{
			{Disposition: agenthost.RuntimeTurnCapabilityUnknown, Retryable: true},
			turnCapabilityAppliedResult(agenthost.RuntimeTurnCapabilityAlreadyBound),
		},
	}); err != nil {
		t.Fatal(err)
	}
	input := turnCapabilityInput("turn-retryable-unknown", "submit-retryable-unknown")
	if _, err := driver.SendTurnCapability(t.Context(), input); !errors.Is(err, agenthost.ErrTurnCapabilityUnavailable) {
		t.Fatalf("retryable unknown SendInput() error = %v", err)
	}
	if _, err := driver.SendTurnCapability(t.Context(), input); err != nil {
		t.Fatalf("retryable unknown retry error = %v", err)
	}
	if got := driver.TurnCapabilityMetrics(); got.EnsureCalls != 2 || got.ExecCalls != 1 {
		t.Fatalf("retryable unknown metrics=%#v", got)
	}
}

func TestTurnCapabilityAppliedOrUnknownRetainsReplayFence(t *testing.T) {
	for _, testCase := range []struct {
		name                string
		result              agenthost.RuntimeTurnCapabilityResult
		failFinalValidation bool
		failExec            bool
		wantExec            int
	}{
		{
			name:                "applied then local validation fails",
			result:              agenthost.RuntimeTurnCapabilityResult{Disposition: agenthost.RuntimeTurnCapabilityApplied},
			failFinalValidation: true,
			wantExec:            0,
		},
		{
			name:     "provider reports unknown",
			result:   agenthost.RuntimeTurnCapabilityResult{Disposition: agenthost.RuntimeTurnCapabilityUnknown},
			wantExec: 0,
		},
		{
			name:     "exec delivery is unknown",
			result:   agenthost.RuntimeTurnCapabilityResult{Disposition: agenthost.RuntimeTurnCapabilityAlreadyBound},
			failExec: true,
			wantExec: 1,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			driver := &turnCapabilityConformanceDriver{t: t}
			if err := driver.ResetTurnCapability(t.Context(), hostconformance.TurnCapabilityFixture{
				EnsureResults:       []agenthost.RuntimeTurnCapabilityResult{testCase.result},
				FailFinalValidation: testCase.failFinalValidation,
			}); err != nil {
				t.Fatal(err)
			}
			if testCase.failExec {
				driver.runtime.execErr = errors.New("injected exec delivery failure")
			}
			input := turnCapabilityInput("turn-fenced", "submit-fenced")
			if _, err := driver.SendTurnCapability(t.Context(), input); !errors.Is(err, agenthost.ErrSubmitDeliveryUnknown) {
				t.Fatalf("initial SendInput() error = %v", err)
			}
			if _, err := driver.SendTurnCapability(t.Context(), input); !errors.Is(err, agenthost.ErrSubmitDeliveryUnknown) {
				t.Fatalf("fenced retry SendInput() error = %v", err)
			}
			if got := driver.TurnCapabilityMetrics(); got.EnsureCalls != 1 || got.ExecCalls != testCase.wantExec {
				t.Fatalf("fenced retry metrics=%#v", got)
			}
		})
	}
}

func TestTurnCapabilityNilAndInvalidRequestsHaveNoCapabilitySideEffects(t *testing.T) {
	driver := &turnCapabilityConformanceDriver{t: t}
	if err := driver.ResetTurnCapability(t.Context(), hostconformance.TurnCapabilityFixture{}); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.SendTurnCapability(t.Context(), agenthost.SendInput{
		TurnID: "turn-normal", ClientSubmitID: "submit-normal",
		Content: []agenthost.PromptContentBlock{{Type: "text", Text: "ordinary provider path"}},
	}); err != nil {
		t.Fatalf("nil invocation SendInput() error = %v", err)
	}
	if got := driver.TurnCapabilityMetrics(); got.EnsureCalls != 0 || got.ExecCalls != 1 {
		t.Fatalf("nil invocation metrics=%#v", got)
	}

	invalid := []agenthost.SendInput{
		{
			TurnID: "turn-guidance", ClientSubmitID: "submit-guidance", Guidance: true,
			TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{Semantic: "test"},
			Content:                  []agenthost.PromptContentBlock{{Type: "text", Text: "guide"}},
		},
		{
			TurnID: "turn-no-submit", TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{Semantic: "test"},
			Content: []agenthost.PromptContentBlock{{Type: "text", Text: "missing submit"}},
		},
		{
			TurnID: "turn-forged-consent", ClientSubmitID: "submit-forged-consent",
			TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{Semantic: "test", Consent: "forged"},
			Content:                  []agenthost.PromptContentBlock{{Type: "text", Text: "forged consent"}},
		},
	}
	for _, input := range invalid {
		if _, err := driver.SendTurnCapability(t.Context(), input); !errors.Is(err, agenthost.ErrInvalidArgument) {
			t.Fatalf("invalid invocation error = %v", err)
		}
	}
	if got := driver.TurnCapabilityMetrics(); got.EnsureCalls != 0 || got.ExecCalls != 1 {
		t.Fatalf("invalid invocation metrics=%#v", got)
	}

	unsupported := &turnCapabilityConformanceDriver{t: t}
	if err := unsupported.reset(t.Context(), nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := unsupported.SendTurnCapability(t.Context(), turnCapabilityInput("turn-unsupported", "submit-unsupported")); !errors.Is(err, agenthost.ErrTurnCapabilityUnsupported) {
		t.Fatalf("unsupported invocation error = %v", err)
	}
	if got := unsupported.TurnCapabilityMetrics(); got.EnsureCalls != 0 || got.ExecCalls != 0 {
		t.Fatalf("unsupported invocation metrics=%#v", got)
	}
}

func TestInitialTurnCapabilityUnsupportedBeforeRuntimeStart(t *testing.T) {
	driver := &turnCapabilityConformanceDriver{t: t}
	if err := driver.reset(t.Context(), nil, true); err != nil {
		t.Fatal(err)
	}
	_, err := driver.CreateTurnCapability(t.Context(), agenthost.CreateSessionInput{
		AgentSessionID: "session-capability", AgentTargetID: "target-capability", Provider: "test-provider",
		TurnID: "turn-initial-unsupported", ClientSubmitID: "submit-initial-unsupported",
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{Semantic: "test-capability"},
		InitialContent:           []agenthost.PromptContentBlock{{Type: "text", Text: "initial capability prompt"}},
	})
	if !errors.Is(err, agenthost.ErrTurnCapabilityUnsupported) {
		t.Fatalf("initial unsupported create error = %v", err)
	}
	if got := driver.TurnCapabilityMetrics(); got.StartCalls != 0 || got.EnsureCalls != 0 || got.ExecCalls != 0 {
		t.Fatalf("unsupported initial capability metrics=%#v", got)
	}
}

func TestTurnCapabilityRevalidatesPromptAugmentationBeforeExec(t *testing.T) {
	driver := &turnCapabilityConformanceDriver{t: t}
	if err := driver.ResetTurnCapability(t.Context(), hostconformance.TurnCapabilityFixture{
		EnsureResults: []agenthost.RuntimeTurnCapabilityResult{{
			Disposition: agenthost.RuntimeTurnCapabilityAlreadyBound,
			PromptAugmentation: []agenthost.PromptContentBlock{{
				Type: "mention", Name: "provider-skill", Path: "app://provider-skill",
			}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.SendTurnCapability(t.Context(), turnCapabilityInput("turn-augmentation", "submit-augmentation")); err != nil {
		t.Fatalf("SendInput() error = %v", err)
	}
	if len(driver.runtime.validated) != 2 || len(driver.runtime.executed) != 1 {
		t.Fatalf("runtime observations validated=%d exec=%d", len(driver.runtime.validated), len(driver.runtime.executed))
	}
	for _, content := range [][]agenthost.PromptContentBlock{driver.runtime.validated[1], driver.runtime.executed[0].Content} {
		if !containsPromptBlock(content, "mention", "provider-skill", "app://provider-skill") {
			t.Fatalf("prompt augmentation missing from %#v", content)
		}
	}
}

func TestTurnCapabilityAcceptsOneStructuredSkillAugmentation(t *testing.T) {
	driver := &turnCapabilityConformanceDriver{t: t}
	if err := driver.ResetTurnCapability(t.Context(), hostconformance.TurnCapabilityFixture{
		EnsureResults: []agenthost.RuntimeTurnCapabilityResult{{
			Disposition: agenthost.RuntimeTurnCapabilityAlreadyBound,
			PromptAugmentation: []agenthost.PromptContentBlock{{
				Type: "skill", Name: "browser-use", Path: "/runtime/skills/browser-use/SKILL.md",
			}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.SendTurnCapability(t.Context(), turnCapabilityInput("turn-skill", "submit-skill")); err != nil {
		t.Fatalf("SendInput() error = %v", err)
	}
	if got := driver.TurnCapabilityMetrics(); got.EnsureCalls != 1 || got.ExecCalls != 1 {
		t.Fatalf("metrics = %#v", got)
	}
	if !containsPromptBlock(driver.runtime.executed[0].Content, "skill", "browser-use", "/runtime/skills/browser-use/SKILL.md") {
		t.Fatalf("skill augmentation missing from %#v", driver.runtime.executed[0].Content)
	}
}

func TestTurnCapabilityInvalidBaseContentDoesNotEnsureAndReleasesClaim(t *testing.T) {
	driver := &turnCapabilityConformanceDriver{t: t}
	if err := driver.ResetTurnCapability(t.Context(), hostconformance.TurnCapabilityFixture{}); err != nil {
		t.Fatal(err)
	}
	driver.runtime.validateResults = []error{errors.New("injected invalid base content")}
	input := turnCapabilityInput("turn-invalid-base", "submit-invalid-base")
	if _, err := driver.SendTurnCapability(t.Context(), input); err == nil || errors.Is(err, agenthost.ErrSubmitDeliveryUnknown) {
		t.Fatalf("invalid base SendInput() error = %v", err)
	}
	if got := driver.TurnCapabilityMetrics(); got.EnsureCalls != 0 || got.ExecCalls != 0 {
		t.Fatalf("invalid base metrics=%#v", got)
	}
	if _, err := driver.SendTurnCapability(t.Context(), input); err != nil {
		t.Fatalf("base-validation retry SendInput() error = %v", err)
	}
	if got := driver.TurnCapabilityMetrics(); got.EnsureCalls != 1 || got.ExecCalls != 1 {
		t.Fatalf("base-validation retry metrics=%#v", got)
	}
}

func TestTurnCapabilityRejectsNonStructuredPromptAugmentation(t *testing.T) {
	invalidAugmentations := []agenthost.PromptContentBlock{
		{Type: "text", Text: "hidden provider instruction"},
		{Type: "image", MimeType: "image/png", Data: "aGVsbG8="},
		{Type: "unknown", Name: "provider", Path: "provider://skill"},
		{Type: "skill", Path: "provider://skill"},
	}

	t.Run("multiple blocks", func(t *testing.T) {
		driver := &turnCapabilityConformanceDriver{t: t}
		if err := driver.ResetTurnCapability(t.Context(), hostconformance.TurnCapabilityFixture{EnsureResults: []agenthost.RuntimeTurnCapabilityResult{{
			Disposition: agenthost.RuntimeTurnCapabilityAlreadyBound,
			PromptAugmentation: []agenthost.PromptContentBlock{
				{Type: "skill", Name: "browser-use", Path: "/runtime/browser/SKILL.md"},
				{Type: "mention", Name: "plugin", Path: "plugin://plugin"},
			},
		}}}); err != nil {
			t.Fatal(err)
		}
		if _, err := driver.SendTurnCapability(t.Context(), turnCapabilityInput("turn-multiple", "submit-multiple")); !errors.Is(err, agenthost.ErrInvalidArgument) {
			t.Fatalf("multiple augmentation error = %v", err)
		}
		if got := driver.TurnCapabilityMetrics(); got.EnsureCalls != 1 || got.ExecCalls != 0 {
			t.Fatalf("multiple augmentation metrics = %#v", got)
		}
	})
	for _, augmentation := range invalidAugmentations {
		t.Run(augmentation.Type, func(t *testing.T) {
			driver := &turnCapabilityConformanceDriver{t: t}
			if err := driver.ResetTurnCapability(t.Context(), hostconformance.TurnCapabilityFixture{
				EnsureResults: []agenthost.RuntimeTurnCapabilityResult{
					{Disposition: agenthost.RuntimeTurnCapabilityAlreadyBound, PromptAugmentation: []agenthost.PromptContentBlock{augmentation}},
					{Disposition: agenthost.RuntimeTurnCapabilityApplied},
				},
			}); err != nil {
				t.Fatal(err)
			}
			input := turnCapabilityInput("turn-invalid-augmentation", "submit-invalid-augmentation")
			if _, err := driver.SendTurnCapability(t.Context(), input); !errors.Is(err, agenthost.ErrInvalidArgument) {
				t.Fatalf("already-bound invalid augmentation error = %v", err)
			}
			if _, err := driver.SendTurnCapability(t.Context(), input); err != nil {
				t.Fatalf("already-bound invalid augmentation retry error = %v", err)
			}
			if got := driver.TurnCapabilityMetrics(); got.EnsureCalls != 2 || got.ExecCalls != 1 {
				t.Fatalf("already-bound invalid augmentation metrics=%#v", got)
			}
		})
	}

	driver := &turnCapabilityConformanceDriver{t: t}
	if err := driver.ResetTurnCapability(t.Context(), hostconformance.TurnCapabilityFixture{
		EnsureResults: []agenthost.RuntimeTurnCapabilityResult{{
			Disposition: agenthost.RuntimeTurnCapabilityApplied,
			PromptAugmentation: []agenthost.PromptContentBlock{{
				Type: "text", Text: "hidden provider instruction",
			}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	input := turnCapabilityInput("turn-applied-augmentation", "submit-applied-augmentation")
	if _, err := driver.SendTurnCapability(t.Context(), input); !errors.Is(err, agenthost.ErrSubmitDeliveryUnknown) {
		t.Fatalf("applied invalid augmentation error = %v", err)
	}
	if _, err := driver.SendTurnCapability(t.Context(), input); !errors.Is(err, agenthost.ErrSubmitDeliveryUnknown) {
		t.Fatalf("applied invalid augmentation retry error = %v", err)
	}
	if got := driver.TurnCapabilityMetrics(); got.EnsureCalls != 1 || got.ExecCalls != 0 {
		t.Fatalf("applied invalid augmentation metrics=%#v", got)
	}
}

type turnCapabilityConformanceDriver struct {
	t            *testing.T
	host         *agenthost.Host
	store        *storesqlite.Store
	runtime      *turnCapabilityRuntime
	capabilities *turnCapabilityPort
	admission    *turnCapabilityAdmission
}

func (d *turnCapabilityConformanceDriver) ResetTurnCapability(
	ctx context.Context,
	fixture hostconformance.TurnCapabilityFixture,
) error {
	capabilities := &turnCapabilityPort{results: append([]agenthost.RuntimeTurnCapabilityResult(nil), fixture.EnsureResults...)}
	if err := d.reset(ctx, capabilities, fixture.InitialSession); err != nil {
		return err
	}
	if fixture.FailFinalValidation {
		d.runtime.validateResults = []error{nil, errors.New("injected final validation failure")}
	}
	d.runtime.emptyExecTurnID = fixture.EmptyExecTurnID
	if fixture.FailStartupGate {
		d.host = agenthost.New(agenthost.Config{CanonicalStore: &agenthost.SQLiteWorkspaceStore{StoreForWorkspace: func(string) *storesqlite.Store { return d.store }}, Runtime: d.runtime, TurnCapabilities: capabilities, TurnCapabilityAdmission: d.admission, RuntimeStartGate: failingTurnCapabilityGate{}})
	}
	d.admission.rejected = fixture.AdmissionRejected
	d.admission.unavailable = fixture.AdmissionUnavailable
	d.admission.results = append([]agenthost.RuntimeTurnCapabilityAdmissionDisposition(nil), fixture.AdmissionResults...)
	if fixture.AdmissionPlan.Key != "" {
		d.admission.plan = fixture.AdmissionPlan
	}
	return nil
}

func (d *turnCapabilityConformanceDriver) reset(ctx context.Context, capabilities *turnCapabilityPort, initialSession bool) error {
	db, err := sql.Open("sqlite", filepath.Join(d.t.TempDir(), "turn-capability.db"))
	if err != nil {
		return err
	}
	d.t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	d.store = storesqlite.New(db, storesqlite.Options{})
	if err := d.store.Migrate(ctx); err != nil {
		return err
	}
	if !initialSession {
		if _, err := d.store.ReportSessionState(ctx, storesqlite.SessionStateReport{
			WorkspaceID: "workspace-capability", AgentSessionID: "session-capability",
			Kind: storesqlite.SessionKindRoot, Origin: "user", Provider: "test-provider",
			ProviderSessionID: "provider-session-capability", Cwd: "/workspace", OccurredAtUnixMS: 1,
		}); err != nil {
			return err
		}
	}
	d.runtime = &turnCapabilityRuntime{store: d.store, session: agenthost.ProviderRuntimeSession{
		ID: "session-capability", WorkspaceID: "workspace-capability", Provider: "test-provider",
		ProviderSessionID: "provider-session-capability", Resumable: true,
	}, live: !initialSession}
	d.capabilities = capabilities
	d.admission = &turnCapabilityAdmission{plan: agenthost.RuntimeTurnCapabilityPlan{Key: "conformance-plan"}}
	if capabilities != nil {
		capabilities.sequence = &d.runtime.sequence
	}
	config := agenthost.Config{
		CanonicalStore: &agenthost.SQLiteWorkspaceStore{
			StoreForWorkspace: func(string) *storesqlite.Store { return d.store },
		},
		Runtime: d.runtime,
	}
	if capabilities != nil {
		config.TurnCapabilities = capabilities
		config.TurnCapabilityAdmission = d.admission
	}
	d.host = agenthost.New(config)
	return nil
}

type turnCapabilityAdmission struct {
	calls       int
	rejected    bool
	unavailable bool
	results     []agenthost.RuntimeTurnCapabilityAdmissionDisposition
	plan        agenthost.RuntimeTurnCapabilityPlan
}

func (a *turnCapabilityAdmission) AdmitTurnCapability(context.Context, agenthost.RuntimeTurnCapabilityAdmissionInput) agenthost.RuntimeTurnCapabilityAdmissionResult {
	a.calls++
	if len(a.results) > 0 {
		result := a.results[0]
		a.results = a.results[1:]
		return agenthost.RuntimeTurnCapabilityAdmissionResult{Disposition: result, Plan: a.plan}
	}
	if a.rejected {
		return agenthost.RuntimeTurnCapabilityAdmissionResult{Disposition: agenthost.RuntimeTurnCapabilityAdmissionRejected}
	}
	if a.unavailable {
		return agenthost.RuntimeTurnCapabilityAdmissionResult{Disposition: agenthost.RuntimeTurnCapabilityAdmissionUnavailable}
	}
	return agenthost.RuntimeTurnCapabilityAdmissionResult{Disposition: agenthost.RuntimeTurnCapabilityAdmissionAllowed, Plan: a.plan}
}

type failingTurnCapabilityGate struct{}

func (failingTurnCapabilityGate) Acquire(context.Context, string) (func(), error) {
	return nil, errors.New("startup gate failed")
}

func (d *turnCapabilityConformanceDriver) CreateTurnCapability(
	ctx context.Context,
	input agenthost.CreateSessionInput,
) (hostconformance.SendObservation, error) {
	result, err := d.host.CreateSession(ctx, "workspace-capability", input)
	if err != nil {
		return hostconformance.SendObservation{}, err
	}
	return hostconformance.SendObservation{
		Session: hostconformance.SessionObservation{
			SessionID: result.Session.ID, ProviderSessionID: result.Session.ProviderSessionID,
		},
		TurnID: result.TurnID,
	}, nil
}

func (d *turnCapabilityConformanceDriver) SendTurnCapability(
	ctx context.Context,
	input agenthost.SendInput,
) (hostconformance.SendObservation, error) {
	result, err := d.host.SendInput(ctx, agenthost.SessionRef{
		WorkspaceID: "workspace-capability", AgentSessionID: "session-capability",
	}, input)
	if err != nil {
		return hostconformance.SendObservation{}, err
	}
	return hostconformance.SendObservation{
		Session: hostconformance.SessionObservation{
			SessionID: result.Session.ID, ProviderSessionID: result.Session.ProviderSessionID,
		},
		TurnID: result.TurnID,
	}, nil
}

func (d *turnCapabilityConformanceDriver) TurnCapabilityMetrics() hostconformance.TurnCapabilityMetrics {
	metrics := hostconformance.TurnCapabilityMetrics{
		StartCalls: d.runtime.startCalls, ResumeCalls: d.runtime.resumeCalls, CloseCalls: d.runtime.closeCalls,
		ExecCalls: len(d.runtime.executed), Sequence: append([]string(nil), d.runtime.sequence...),
	}
	if d.capabilities != nil {
		metrics.EnsureCalls = len(d.capabilities.inputs)
		for _, input := range d.capabilities.inputs {
			metrics.EnsurePlanKeys = append(metrics.EnsurePlanKeys, input.Plan.Key)
		}
	}
	if d.admission != nil {
		metrics.AdmissionCalls = d.admission.calls
	}
	return metrics
}

type turnCapabilityRuntime struct {
	agenthost.RuntimeController
	store           *storesqlite.Store
	session         agenthost.ProviderRuntimeSession
	live            bool
	startCalls      int
	resumeCalls     int
	closeCalls      int
	validated       [][]agenthost.PromptContentBlock
	executed        []agenthost.RuntimeExecInput
	sequence        []string
	validateResults []error
	execErr         error
	emptyExecTurnID bool
}

func (r *turnCapabilityRuntime) Resume(_ context.Context, input agenthost.RuntimeResumeInput) (agenthost.ProviderRuntimeSession, error) {
	r.resumeCalls++
	r.sequence = append(r.sequence, "resume")
	r.live = true
	return r.session, nil
}

func (r *turnCapabilityRuntime) Session(workspaceID, sessionID string) (agenthost.ProviderRuntimeSession, bool) {
	return r.session, r.live && workspaceID == r.session.WorkspaceID && sessionID == r.session.ID
}

func (r *turnCapabilityRuntime) Start(_ context.Context, input agenthost.RuntimeStartInput) (agenthost.ProviderRuntimeSession, error) {
	r.startCalls++
	r.sequence = append(r.sequence, "start")
	r.session = agenthost.ProviderRuntimeSession{
		ID: input.AgentSessionID, WorkspaceID: input.WorkspaceID, Provider: input.Provider,
		ProviderSessionID: "provider-session-capability", Resumable: true,
	}
	r.live = true
	return r.session, nil
}

func (r *turnCapabilityRuntime) Close(_ context.Context, _ agenthost.RuntimeCloseInput) error {
	r.closeCalls++
	r.live = false
	return nil
}

func (r *turnCapabilityRuntime) ValidatePromptContent(_ context.Context, input agenthost.RuntimeExecInput) error {
	r.validated = append(r.validated, append([]agenthost.PromptContentBlock(nil), input.Content...))
	r.sequence = append(r.sequence, "validate:"+input.TurnID)
	if len(r.validateResults) == 0 {
		return nil
	}
	err := r.validateResults[0]
	r.validateResults = r.validateResults[1:]
	return err
}

func (r *turnCapabilityRuntime) Exec(ctx context.Context, input agenthost.RuntimeExecInput) (agenthost.RuntimeExecResult, error) {
	r.executed = append(r.executed, input)
	r.sequence = append(r.sequence, "exec:"+input.TurnID)
	if r.execErr != nil {
		return agenthost.RuntimeExecResult{}, r.execErr
	}
	if _, err := r.store.ReportActivityState(ctx, storesqlite.ActivityStateReport{
		Session: storesqlite.SessionStateReport{
			WorkspaceID: r.session.WorkspaceID, AgentSessionID: r.session.ID,
			Kind: storesqlite.SessionKindRoot, Origin: "user", Provider: r.session.Provider,
			ProviderSessionID: r.session.ProviderSessionID, Cwd: "/workspace", OccurredAtUnixMS: int64(len(r.executed) + 1),
		},
		Turn: &storesqlite.TurnTransition{
			WorkspaceID: r.session.WorkspaceID, AgentSessionID: r.session.ID, TurnID: input.TurnID,
			Phase: storesqlite.TurnPhaseSettled, Outcome: storesqlite.TurnOutcomeCompleted,
			OccurredAtUnixMS: int64(len(r.executed) + 1),
		},
		RootProviderTurn: &storesqlite.RootProviderTurnTransition{
			WorkspaceID: r.session.WorkspaceID, RootAgentSessionID: r.session.ID,
			RootTurnID: input.TurnID, ProviderTurnID: "provider-" + input.TurnID,
			Phase:   storesqlite.RootProviderTurnPhaseCompleted,
			Outcome: storesqlite.TurnOutcomeCompleted, OccurredAtUnixMS: int64(len(r.executed) + 1),
		},
	}); err != nil {
		return agenthost.RuntimeExecResult{}, err
	}
	turnID := input.TurnID
	if r.emptyExecTurnID {
		turnID = ""
	}
	return agenthost.RuntimeExecResult{
		AgentSessionID: r.session.ID, TurnID: turnID, Accepted: true,
		SubmitAvailability: agenthost.SubmitAvailability{State: "available"},
	}, nil
}

type turnCapabilityPort struct {
	inputs   []agenthost.RuntimeTurnCapabilityInput
	results  []agenthost.RuntimeTurnCapabilityResult
	sequence *[]string
}

func (p *turnCapabilityPort) EnsureTurnCapability(
	_ context.Context,
	input agenthost.RuntimeTurnCapabilityInput,
) (agenthost.RuntimeTurnCapabilityResult, error) {
	p.inputs = append(p.inputs, input)
	if p.sequence != nil {
		*p.sequence = append(*p.sequence, "ensure:"+input.TurnID)
	}
	if len(p.results) == 0 {
		return turnCapabilityAppliedResult(agenthost.RuntimeTurnCapabilityApplied), nil
	}
	result := p.results[0]
	p.results = p.results[1:]
	if (result.Disposition == agenthost.RuntimeTurnCapabilityApplied || result.Disposition == agenthost.RuntimeTurnCapabilityAlreadyBound) && len(result.PromptAugmentation) == 0 {
		return turnCapabilityAppliedResult(result.Disposition), nil
	}
	return result, nil
}

func turnCapabilityAppliedResult(disposition agenthost.RuntimeTurnCapabilityDisposition) agenthost.RuntimeTurnCapabilityResult {
	return agenthost.RuntimeTurnCapabilityResult{Disposition: disposition, PromptAugmentation: []agenthost.PromptContentBlock{{
		Type: "mention", Name: "provider-capability", Path: "plugin://provider-capability",
	}}}
}

func turnCapabilityInput(turnID, submitID string) agenthost.SendInput {
	return agenthost.SendInput{
		TurnID: turnID, ClientSubmitID: submitID,
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{Semantic: "test-capability"},
		Content:                  []agenthost.PromptContentBlock{{Type: "text", Text: "capability prompt"}},
	}
}

func containsPromptBlock(content []agenthost.PromptContentBlock, typ, name, path string) bool {
	for _, block := range content {
		if block.Type == typ && block.Name == name && block.Path == path {
			return true
		}
	}
	return false
}
