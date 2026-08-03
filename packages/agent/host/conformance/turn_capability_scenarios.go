package conformance

import (
	"context"
	"errors"
	"fmt"
	"slices"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
)

func runExistingSessionTurnCapability(ctx context.Context, driver TurnCapabilityDriver) error {
	if err := driver.ResetTurnCapability(ctx, TurnCapabilityFixture{}); err != nil {
		return err
	}
	first, err := driver.SendTurnCapability(ctx, agenthost.SendInput{
		TurnID: "turn-first", ClientSubmitID: "submit-first",
		Content: []agenthost.PromptContentBlock{{Type: "text", Text: "first ordinary message"}},
	})
	if err != nil {
		return fmt.Errorf("first ordinary turn: %w", err)
	}
	second, err := driver.SendTurnCapability(ctx, agenthost.SendInput{
		TurnID: "turn-second", ClientSubmitID: "submit-second",
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{Semantic: "conformance-capability"},
		Content:                  []agenthost.PromptContentBlock{{Type: "text", Text: "second capability message"}},
	})
	if err != nil {
		return fmt.Errorf("second capability turn: %w", err)
	}
	if first.Session.SessionID != second.Session.SessionID ||
		first.Session.ProviderSessionID != second.Session.ProviderSessionID {
		return fmt.Errorf("capability turn replaced session identity first=%#v second=%#v", first.Session, second.Session)
	}
	metrics := driver.TurnCapabilityMetrics()
	if metrics.EnsureCalls != 1 || metrics.ExecCalls != 2 {
		return fmt.Errorf("turn capability metrics=%#v, want one ensure before second of two execs", metrics)
	}
	if !slices.Equal(metrics.Sequence, []string{
		"validate:", "exec:turn-first",
		"validate:", "ensure:turn-second", "validate:", "exec:turn-second",
	}) {
		return fmt.Errorf("turn capability sequence=%v", metrics.Sequence)
	}
	return nil
}

func runInitialSessionTurnCapability(ctx context.Context, driver TurnCapabilityDriver) error {
	if err := driver.ResetTurnCapability(ctx, TurnCapabilityFixture{InitialSession: true}); err != nil {
		return err
	}
	input := agenthost.CreateSessionInput{
		AgentSessionID: "session-capability", AgentTargetID: "target-capability", Provider: "test-provider",
		TurnID: "turn-initial", ClientSubmitID: "submit-initial",
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{Semantic: "conformance-capability"},
		InitialContent:           []agenthost.PromptContentBlock{{Type: "text", Text: "initial capability message"}},
	}
	created, err := driver.CreateTurnCapability(ctx, input)
	if err != nil {
		return fmt.Errorf("initial capability create: %w", err)
	}
	if created.Session.SessionID != "session-capability" || created.TurnID != "turn-initial" {
		return fmt.Errorf("initial capability create=%#v", created)
	}
	if _, err := driver.CreateTurnCapability(ctx, input); err != nil {
		return fmt.Errorf("accepted initial capability retry: %w", err)
	}
	metrics := driver.TurnCapabilityMetrics()
	if metrics.StartCalls != 1 || metrics.EnsureCalls != 1 || metrics.ExecCalls != 1 {
		return fmt.Errorf("initial capability metrics=%#v", metrics)
	}
	if !slices.Equal(metrics.Sequence, []string{
		"start", "validate:", "ensure:turn-initial", "validate:", "exec:turn-initial",
	}) {
		return fmt.Errorf("initial capability sequence=%v", metrics.Sequence)
	}
	return nil
}

func runTurnCapabilityAdmissionPlan(ctx context.Context, driver TurnCapabilityDriver) error {
	if err := driver.ResetTurnCapability(ctx, TurnCapabilityFixture{AdmissionPlan: agenthost.RuntimeTurnCapabilityPlan{
		Key: "adapter-plan",
	}}); err != nil {
		return err
	}
	if _, err := driver.SendTurnCapability(ctx, conformanceTurnCapabilityInput("turn-admission-plan", "submit-admission-plan")); err != nil {
		return fmt.Errorf("planned capability submission: %w", err)
	}
	metrics := driver.TurnCapabilityMetrics()
	if metrics.AdmissionCalls != 1 || metrics.EnsureCalls != 1 || metrics.ExecCalls != 1 || !slices.Equal(metrics.EnsurePlanKeys, []string{"adapter-plan"}) {
		return fmt.Errorf("admission plan metrics=%#v", metrics)
	}
	return nil
}

func runDurableTurnCapabilityAdmissionPlan(ctx context.Context, driver TurnCapabilityDriver) error {
	if err := driver.ResetTurnCapability(ctx, TurnCapabilityFixture{
		AdmissionPlan: agenthost.RuntimeTurnCapabilityPlan{Key: "frozen-plan"},
		EnsureResults: []agenthost.RuntimeTurnCapabilityResult{{Disposition: agenthost.RuntimeTurnCapabilityUnknown}},
	}); err != nil {
		return err
	}
	input := conformanceTurnCapabilityInput("turn-frozen-plan", "submit-frozen-plan")
	if _, err := driver.SendTurnCapability(ctx, input); !errors.Is(err, agenthost.ErrSubmitDeliveryUnknown) {
		return fmt.Errorf("uncertain planned submission: %w", err)
	}
	if _, err := driver.SendTurnCapability(ctx, input); !errors.Is(err, agenthost.ErrSubmitDeliveryUnknown) {
		return fmt.Errorf("fenced planned retry: %w", err)
	}
	metrics := driver.TurnCapabilityMetrics()
	if metrics.AdmissionCalls != 1 || metrics.EnsureCalls != 1 || metrics.ExecCalls != 0 || !slices.Equal(metrics.EnsurePlanKeys, []string{"frozen-plan"}) {
		return fmt.Errorf("durable plan retry metrics=%#v", metrics)
	}
	return nil
}

func runRejectedInitialTurnCapabilityDoesNotExec(ctx context.Context, driver TurnCapabilityDriver) error {
	if err := driver.ResetTurnCapability(ctx, TurnCapabilityFixture{
		InitialSession: true,
		EnsureResults:  []agenthost.RuntimeTurnCapabilityResult{{Disposition: agenthost.RuntimeTurnCapabilityRejected}},
	}); err != nil {
		return err
	}
	_, err := driver.CreateTurnCapability(ctx, agenthost.CreateSessionInput{
		AgentSessionID: "session-capability", AgentTargetID: "target-capability", Provider: "test-provider",
		TurnID: "turn-initial-rejected", ClientSubmitID: "submit-initial-rejected",
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{Semantic: "conformance-capability"},
		InitialContent:           []agenthost.PromptContentBlock{{Type: "text", Text: "initial rejected capability message"}},
	})
	if !errors.Is(err, agenthost.ErrTurnCapabilityRejected) {
		return fmt.Errorf("rejected initial capability create: %w", err)
	}
	metrics := driver.TurnCapabilityMetrics()
	if metrics.EnsureCalls != 1 || metrics.ExecCalls != 0 || metrics.CloseCalls != 1 {
		return fmt.Errorf("rejected initial capability metrics=%#v", metrics)
	}
	return nil
}

func runAcceptedTurnCapabilityRetry(ctx context.Context, driver TurnCapabilityDriver) error {
	if err := driver.ResetTurnCapability(ctx, TurnCapabilityFixture{}); err != nil {
		return err
	}
	input := conformanceTurnCapabilityInput("turn-accepted", "submit-accepted")
	if _, err := driver.SendTurnCapability(ctx, input); err != nil {
		return fmt.Errorf("initial capability submission: %w", err)
	}
	if _, err := driver.SendTurnCapability(ctx, input); err != nil {
		return fmt.Errorf("accepted capability retry: %w", err)
	}
	if metrics := driver.TurnCapabilityMetrics(); metrics.EnsureCalls != 1 || metrics.ExecCalls != 1 || metrics.AdmissionCalls != 1 {
		return fmt.Errorf("accepted retry metrics=%#v", metrics)
	}
	return nil
}

func runTurnCapabilityAdmissionRejection(ctx context.Context, driver TurnCapabilityDriver) error {
	if err := driver.ResetTurnCapability(ctx, TurnCapabilityFixture{AdmissionRejected: true}); err != nil {
		return err
	}
	if _, err := driver.SendTurnCapability(ctx, conformanceTurnCapabilityInput("turn-admission", "submit-admission")); !errors.Is(err, agenthost.ErrTurnCapabilityRejected) {
		return fmt.Errorf("admission rejection: %w", err)
	}
	if metrics := driver.TurnCapabilityMetrics(); metrics.AdmissionCalls != 1 || metrics.ResumeCalls != 0 || metrics.EnsureCalls != 0 || metrics.ExecCalls != 0 {
		return fmt.Errorf("admission rejection metrics=%#v", metrics)
	}
	return nil
}

func runTurnCapabilityAdmissionUnavailable(ctx context.Context, driver TurnCapabilityDriver) error {
	if err := driver.ResetTurnCapability(ctx, TurnCapabilityFixture{AdmissionResults: []agenthost.RuntimeTurnCapabilityAdmissionDisposition{
		agenthost.RuntimeTurnCapabilityAdmissionUnavailable,
		agenthost.RuntimeTurnCapabilityAdmissionAllowed,
	}}); err != nil {
		return err
	}
	input := conformanceTurnCapabilityInput("turn-admission-unavailable", "submit-admission-unavailable")
	if _, err := driver.SendTurnCapability(ctx, input); !errors.Is(err, agenthost.ErrTurnCapabilityAdmissionUnavailable) {
		return fmt.Errorf("admission unavailable: %w", err)
	}
	if metrics := driver.TurnCapabilityMetrics(); metrics.ResumeCalls != 0 || metrics.EnsureCalls != 0 || metrics.ExecCalls != 0 {
		return fmt.Errorf("admission unavailable metrics=%#v", metrics)
	}
	if _, err := driver.SendTurnCapability(ctx, input); err != nil {
		return fmt.Errorf("admission retry: %w", err)
	}
	if metrics := driver.TurnCapabilityMetrics(); metrics.AdmissionCalls != 2 || metrics.EnsureCalls != 1 || metrics.ExecCalls != 1 {
		return fmt.Errorf("admission retry metrics=%#v", metrics)
	}
	return nil
}

func runRejectedTurnCapabilityRetry(ctx context.Context, driver TurnCapabilityDriver) error {
	if err := driver.ResetTurnCapability(ctx, TurnCapabilityFixture{EnsureResults: []agenthost.RuntimeTurnCapabilityResult{
		{Disposition: agenthost.RuntimeTurnCapabilityRejected},
		{Disposition: agenthost.RuntimeTurnCapabilityApplied},
	}}); err != nil {
		return err
	}
	input := conformanceTurnCapabilityInput("turn-rejected", "submit-rejected")
	if _, err := driver.SendTurnCapability(ctx, input); !errors.Is(err, agenthost.ErrTurnCapabilityRejected) {
		return fmt.Errorf("rejected capability submission: %w", err)
	}
	if _, err := driver.SendTurnCapability(ctx, input); err != nil {
		return fmt.Errorf("rejected capability retry: %w", err)
	}
	if metrics := driver.TurnCapabilityMetrics(); metrics.EnsureCalls != 2 || metrics.ExecCalls != 1 {
		return fmt.Errorf("rejected retry metrics=%#v", metrics)
	}
	return nil
}

func runUncertainTurnCapabilityDoesNotReplay(ctx context.Context, driver TurnCapabilityDriver) error {
	fixtures := []TurnCapabilityFixture{
		{EnsureResults: []agenthost.RuntimeTurnCapabilityResult{{Disposition: agenthost.RuntimeTurnCapabilityUnknown}}},
		{EnsureResults: []agenthost.RuntimeTurnCapabilityResult{{Disposition: agenthost.RuntimeTurnCapabilityApplied}}, FailFinalValidation: true},
	}
	for index, fixture := range fixtures {
		if err := driver.ResetTurnCapability(ctx, fixture); err != nil {
			return err
		}
		input := conformanceTurnCapabilityInput(fmt.Sprintf("turn-uncertain-%d", index), fmt.Sprintf("submit-uncertain-%d", index))
		if _, err := driver.SendTurnCapability(ctx, input); !errors.Is(err, agenthost.ErrSubmitDeliveryUnknown) {
			return fmt.Errorf("uncertain capability submission: %w", err)
		}
		if _, err := driver.SendTurnCapability(ctx, input); !errors.Is(err, agenthost.ErrSubmitDeliveryUnknown) {
			return fmt.Errorf("uncertain capability retry: %w", err)
		}
		if metrics := driver.TurnCapabilityMetrics(); metrics.EnsureCalls != 1 || metrics.ExecCalls != 0 {
			return fmt.Errorf("uncertain retry metrics=%#v", metrics)
		}
	}
	return nil
}

func runAppliedCapabilityStartupGateFailure(ctx context.Context, driver TurnCapabilityDriver) error {
	if err := driver.ResetTurnCapability(ctx, TurnCapabilityFixture{FailStartupGate: true}); err != nil {
		return err
	}
	input := conformanceTurnCapabilityInput("turn-gate", "submit-gate")
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := driver.SendTurnCapability(ctx, input); !errors.Is(err, agenthost.ErrSubmitDeliveryUnknown) {
			return fmt.Errorf("startup gate attempt %d: %w", attempt, err)
		}
	}
	if metrics := driver.TurnCapabilityMetrics(); metrics.EnsureCalls != 1 || metrics.ExecCalls != 0 || metrics.AdmissionCalls != 1 {
		return fmt.Errorf("startup gate metrics=%#v", metrics)
	}
	return nil
}

func runCapabilityEmptyTurnID(ctx context.Context, driver TurnCapabilityDriver) error {
	if err := driver.ResetTurnCapability(ctx, TurnCapabilityFixture{EmptyExecTurnID: true}); err != nil {
		return err
	}
	input := conformanceTurnCapabilityInput("turn-empty", "submit-empty")
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := driver.SendTurnCapability(ctx, input); !errors.Is(err, agenthost.ErrSubmitDeliveryUnknown) {
			return fmt.Errorf("empty turn id attempt %d: %w", attempt, err)
		}
	}
	if metrics := driver.TurnCapabilityMetrics(); metrics.EnsureCalls != 1 || metrics.ExecCalls != 1 || metrics.AdmissionCalls != 1 {
		return fmt.Errorf("empty turn id metrics=%#v", metrics)
	}
	return nil
}

func conformanceTurnCapabilityInput(turnID, submitID string) agenthost.SendInput {
	return agenthost.SendInput{
		TurnID: turnID, ClientSubmitID: submitID,
		TurnCapabilityInvocation: &agenthost.TurnCapabilityInvocation{Semantic: "conformance-capability"},
		Content:                  []agenthost.PromptContentBlock{{Type: "text", Text: "capability message"}},
	}
}
