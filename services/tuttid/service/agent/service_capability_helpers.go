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

const initialTurnCapabilityTuttiModeContextKey = "tuttiModeActiveAtInitialCapabilityAdmission"
const initialTurnCapabilityTuttiModeSnapshotContextKey = "tuttiModeSnapshotAtInitialCapabilityAdmission"

// initialTurnCapabilityAdmissionRuntimeContext carries immutable request
// policy to the Host-owned post-claim admission seam. Host treats it as opaque;
// only the product adapter interprets it.
func initialTurnCapabilityAdmissionRuntimeContext(context map[string]any, intent *TuttiModeActivationIntent) map[string]any {
	result := clonePayload(context)
	if result == nil {
		result = make(map[string]any, 1)
	}
	active := initialTuttiModeActive(intent)
	result[initialTurnCapabilityTuttiModeContextKey] = active
	if active {
		result[initialTurnCapabilityTuttiModeSnapshotContextKey] = &agenthost.TuttiModeTurnSnapshot{
			State: string(intent.State), Source: string(intent.Source),
			PreferenceVersion: agenthost.TuttiModePreferenceVersionEffectSpeed,
			Effect:            valueInt(intent.Effect), Speed: valueInt(intent.Speed), OrchestrationIntensity: valueInt(intent.Effect),
		}
	}
	return result
}

func valueInt(input *int) int {
	if input == nil {
		return 0
	}
	return *input
}

func initialTurnCapabilityTuttiModeSnapshot(context map[string]any) *agenthost.TuttiModeTurnSnapshot {
	value, ok := context[initialTurnCapabilityTuttiModeSnapshotContextKey].(*agenthost.TuttiModeTurnSnapshot)
	if !ok || value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func initialTurnCapabilityTuttiModeActive(context map[string]any) (bool, bool) {
	active, ok := context[initialTurnCapabilityTuttiModeContextKey].(bool)
	return active, ok
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
