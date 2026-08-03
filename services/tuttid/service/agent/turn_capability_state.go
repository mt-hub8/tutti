package agent

import runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"

// turnCapabilityStatesFromRuntimeContext projects only the durable facts the
// Composer needs to avoid re-requesting a session consent. Catalog discovery
// never reads or caches this state.
func turnCapabilityStatesFromRuntimeContext(runtimeContext map[string]any) []TurnCapabilityState {
	bindings := runtimeprep.CodexTurnCapabilityBindingsFromRuntimeContext(runtimeContext)
	if len(bindings) == 0 {
		return nil
	}
	result := make([]TurnCapabilityState, 0, len(bindings))
	for _, binding := range bindings {
		// This projection is only the durable, session-scoped Computer
		// authorization fact used to avoid asking for the same consent again.
		// Loaded and Ready belong to one App Server client generation and are
		// deliberately never reconstructed from canonical state; the current
		// runtime revalidates them before every Computer turn.
		if binding.Semantic != runtimeprep.CodexNativeCapabilityComputer || !binding.Authorized {
			continue
		}
		result = append(result, TurnCapabilityState{
			Semantic: runtimeprep.CodexTurnCapabilitySemanticComputerUse,
			State:    "bound",
		})
	}
	return result
}
