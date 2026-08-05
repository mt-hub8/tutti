package agent

import (
	"errors"
	"strings"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

// TurnCapabilityRecoveryError is the product-facing projection of a Host
// pre-Exec outcome. It contains no provider implementation details and is
// deliberately retryable: setup never consumes a Turn delivery identity.
type TurnCapabilityRecoveryError struct {
	NextAction string
	ReasonCode string
	Cause      error
}

func (e *TurnCapabilityRecoveryError) Error() string {
	if e == nil || e.NextAction == "" {
		return "agent turn capability needs user action"
	}
	// Do not surface App Server or plugin diagnostics through the HTTP error
	// boundary. ReasonCode and NextAction are closed, product-safe values.
	return "agent turn capability requires " + e.NextAction
}

func (e *TurnCapabilityRecoveryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func turnCapabilityRecoveryError(err error) error {
	var outcome *agenthost.TurnCapabilityOutcomeError
	if !errors.As(err, &outcome) || outcome == nil {
		return err
	}
	return &TurnCapabilityRecoveryError{
		NextAction: string(outcome.Outcome.NextAction),
		ReasonCode: outcome.Outcome.ReasonCode,
		Cause:      err,
	}
}

// validateTurnCapabilityInvocationForService is adapter input validation only;
// Host remains the owner of claim, admission, runtime preparation, and Exec.
func validateTurnCapabilityInvocationForService(invocation *agenthost.TurnCapabilityInvocation, initial bool) error {
	if invocation == nil {
		return nil
	}
	semantic := strings.TrimSpace(invocation.Semantic)
	consent := agenthost.TurnCapabilityConsent(strings.TrimSpace(string(invocation.Consent)))
	if !runtimeprep.IsCodexTurnCapabilityInvocationSemantic(semantic) ||
		(consent != "" && consent != agenthost.TurnCapabilityConsentExplicitSession) {
		return ErrInvalidArgument
	}
	if initial && semantic == runtimeprep.CodexTurnCapabilitySemanticComputerUse && consent != agenthost.TurnCapabilityConsentExplicitSession {
		return ErrInvalidArgument
	}
	return nil
}

func modelEndpointUsesOpenAIProtocol(endpoint *runtimeprep.ModelEndpointConfig) bool {
	return endpoint != nil && strings.TrimSpace(endpoint.Protocol) == "openai" &&
		strings.TrimSpace(endpoint.BaseURL) != "" && strings.TrimSpace(endpoint.APIKey) != ""
}
