package api

import (
	"testing"

	"github.com/tutti-os/tutti/services/tuttid/apierrors"
)

func TestProtocolErrorResponseProjectsTurnCapabilityRecoveryOutcome(t *testing.T) {
	t.Parallel()

	response := protocolErrorResponse(apierrors.InvalidRequest(
		"agent.turn_capability_plugin_not_ready",
		apierrors.WithTurnCapabilityOutcome(apierrors.TurnCapabilityOutcome{
			NextAction: "setup_required",
			ReasonCode: "plugin_not_ready",
		}),
	))
	if response.Error.TurnCapabilityOutcome == nil {
		t.Fatal("TurnCapabilityOutcome = nil")
	}
	if got := string(response.Error.TurnCapabilityOutcome.NextAction); got != "setup_required" {
		t.Fatalf("NextAction = %q, want setup_required", got)
	}
	if got := response.Error.TurnCapabilityOutcome.ReasonCode; got != "plugin_not_ready" {
		t.Fatalf("ReasonCode = %q, want plugin_not_ready", got)
	}
}
