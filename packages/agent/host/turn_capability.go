package agenthost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

func encodeTurnCapabilityPlan(plan RuntimeTurnCapabilityPlan) (string, error) {
	if !validTurnCapabilityPlanKey(plan.Key) {
		return "", ErrInvalidArgument
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func decodeTurnCapabilityPlan(encoded string) (RuntimeTurnCapabilityPlan, error) {
	if strings.TrimSpace(encoded) == "" {
		return RuntimeTurnCapabilityPlan{}, nil
	}
	var plan RuntimeTurnCapabilityPlan
	decoder := json.NewDecoder(bytes.NewBufferString(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil || !validTurnCapabilityPlanKey(plan.Key) {
		return RuntimeTurnCapabilityPlan{}, ErrInvalidArgument
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return RuntimeTurnCapabilityPlan{}, ErrInvalidArgument
	}
	return plan, nil
}

func validTurnCapabilityPlanKey(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return false
	}
	for _, runeValue := range value {
		if (runeValue < 'a' || runeValue > 'z') &&
			(runeValue < 'A' || runeValue > 'Z') &&
			(runeValue < '0' || runeValue > '9') &&
			runeValue != '_' && runeValue != '-' {
			return false
		}
	}
	return true
}

func (h *Host) validatedTurnCapabilityInvocation(
	invocationInput *TurnCapabilityInvocation,
	guidance bool,
	turnID string,
	clientSubmitID string,
) (*TurnCapabilityInvocation, bool, error) {
	if invocationInput == nil {
		return nil, false, nil
	}
	if guidance || strings.TrimSpace(turnID) == "" || strings.TrimSpace(clientSubmitID) == "" || h.turnCapabilities == nil || h.turnCapabilityAdmission == nil {
		if h.turnCapabilities == nil || h.turnCapabilityAdmission == nil {
			return nil, true, ErrTurnCapabilityUnsupported
		}
		return nil, true, ErrInvalidArgument
	}
	invocation := TurnCapabilityInvocation{
		Semantic: strings.TrimSpace(invocationInput.Semantic),
		Consent:  TurnCapabilityConsent(strings.TrimSpace(string(invocationInput.Consent))),
	}
	if strings.TrimSpace(invocation.Semantic) == "" {
		return nil, true, ErrInvalidArgument
	}
	if invocation.Consent != "" && invocation.Consent != TurnCapabilityConsentExplicitSession {
		return nil, true, ErrInvalidArgument
	}
	return &invocation, true, nil
}

func (h *Host) admitTurnCapability(ctx context.Context, input RuntimeTurnCapabilityAdmissionInput) (RuntimeTurnCapabilityPlan, error) {
	result := h.turnCapabilityAdmission.AdmitTurnCapability(ctx, input)
	switch result.Disposition {
	case RuntimeTurnCapabilityAdmissionAllowed:
		return result.Plan, nil
	case RuntimeTurnCapabilityAdmissionRejected:
		return RuntimeTurnCapabilityPlan{}, ErrTurnCapabilityRejected
	case RuntimeTurnCapabilityAdmissionUnavailable:
		return RuntimeTurnCapabilityPlan{}, ErrTurnCapabilityAdmissionUnavailable
	default:
		// Admission runs before any provider mutation. An adapter that cannot
		// classify its policy read must leave the claim retryable, never create a
		// delivery-unknown replay fence.
		return RuntimeTurnCapabilityPlan{}, ErrTurnCapabilityAdmissionUnavailable
	}
}

func mergeTurnCapabilityPromptContent(
	base []PromptContentBlock,
	augmentation []PromptContentBlock,
) ([]PromptContentBlock, string, error) {
	if len(augmentation) == 0 {
		return normalizePromptContent(base)
	}
	validatedAugmentation, err := validateTurnCapabilityPromptAugmentation(augmentation)
	if err != nil {
		return nil, "", err
	}
	merged := make([]PromptContentBlock, 0, len(base)+len(augmentation))
	merged = append(merged, base...)
	merged = append(merged, validatedAugmentation...)
	return normalizePromptContent(merged)
}

func validateTurnCapabilityPromptAugmentation(input []PromptContentBlock) ([]PromptContentBlock, error) {
	if len(input) != 1 {
		return nil, ErrInvalidArgument
	}
	block := input[0]
	typ, name, path := strings.TrimSpace(block.Type), strings.TrimSpace(block.Name), strings.TrimSpace(block.Path)
	if (typ != "mention" && typ != "skill") || name == "" || path == "" {
		return nil, ErrInvalidArgument
	}
	return []PromptContentBlock{{Type: typ, Name: name, Path: path}}, nil
}

func turnCapabilityDeliveryUnknown(cause error) error {
	if cause == nil || errors.Is(cause, ErrSubmitDeliveryUnknown) {
		return ErrSubmitDeliveryUnknown
	}
	return errors.Join(ErrSubmitDeliveryUnknown, cause)
}

func turnCapabilityOutcomeError(
	cause error,
	outcome *RuntimeTurnCapabilityOutcome,
) error {
	if outcome == nil || !validTurnCapabilityOutcome(*outcome) {
		return cause
	}
	return &TurnCapabilityOutcomeError{Outcome: *outcome, Cause: cause}
}

func validTurnCapabilityOutcome(outcome RuntimeTurnCapabilityOutcome) bool {
	switch outcome.NextAction {
	case RuntimeTurnCapabilityNextActionSetup,
		RuntimeTurnCapabilityNextActionEnable,
		RuntimeTurnCapabilityNextActionAuthorize,
		RuntimeTurnCapabilityNextActionRetry,
		RuntimeTurnCapabilityNextActionBlocked:
	default:
		return false
	}
	return validTurnCapabilityPlanKey(outcome.ReasonCode)
}
