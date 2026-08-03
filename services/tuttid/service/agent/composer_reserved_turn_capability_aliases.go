package agent

import (
	"strings"

	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

// composerReservedTurnCapabilityAliases is an exact-Codex product projection.
// It reserves the native aliases even when plugin inventory is unavailable so
// the generic Composer never falls through to a legacy command for the same
// alias. CapabilityCatalog remains the independent inventory/skill surface.
func composerReservedTurnCapabilityAliases(
	agentTargetID string,
	provider string,
	providerTargetRef map[string]any,
	catalog []ComposerCapabilityOption,
	catalogErrors []string,
) []ComposerReservedTurnCapabilityAlias {
	if !isAuthoritativeCodexTurnCapabilityTarget(agentTargetID, provider, providerTargetRef) {
		return nil
	}
	bySemantic := make(map[string]ComposerCapabilityOption, len(catalog))
	for _, option := range catalog {
		semantic := strings.TrimSpace(option.Semantic)
		if semantic == "" {
			continue
		}
		bySemantic[semantic] = option
	}
	result := make([]ComposerReservedTurnCapabilityAlias, 0, 3)
	for _, native := range []struct {
		alias, semantic, name, label string
	}{
		{"/browser", runtimeprep.CodexTurnCapabilitySemanticBrowserUse, "browser", "Browser"},
		{"/computer", runtimeprep.CodexTurnCapabilitySemanticComputerUse, "computer", "Computer"},
		{"/sites", runtimeprep.CodexTurnCapabilitySemanticSites, "sites", "Sites"},
	} {
		alias := ComposerReservedTurnCapabilityAlias{
			Alias:           native.alias,
			Semantic:        native.semantic,
			Name:            native.name,
			Label:           native.label,
			Status:          "unknown",
			Reason:          "availabilityUnknown",
			NextAction:      "retry",
			Invocation:      "promptItem",
			InvocationScope: "turn",
		}
		if option, ok := bySemantic[native.semantic]; ok {
			alias.Name = firstNonEmptyString(strings.TrimSpace(option.Name), alias.Name)
			alias.Label = firstNonEmptyString(strings.TrimSpace(option.Label), alias.Label)
			alias.Description = strings.TrimSpace(option.Description)
			alias.Status = strings.TrimSpace(option.Status)
			if alias.Status == "" {
				alias.Status = "unknown"
			}
			alias.Reason = reservedTurnCapabilityAliasReason(alias.Status)
			alias.NextAction = reservedTurnCapabilityAliasNextAction(alias.Status)
		}
		if len(catalogErrors) > 0 {
			alias.Status = "unknown"
			alias.Reason = "availabilityUnknown"
			alias.NextAction = "retry"
		}
		if native.semantic == runtimeprep.CodexTurnCapabilitySemanticComputerUse {
			alias.ConsentRequirement = "explicitSession"
		}
		result = append(result, alias)
	}
	return result
}

func reservedTurnCapabilityAliasReason(status string) string {
	switch strings.TrimSpace(status) {
	case "available":
		return ""
	case "notInstalled", "setupRequired":
		return "notInstalled"
	case "disabled":
		return "disabled"
	case "disabledByAdmin":
		return "disabledByAdmin"
	case "unsupported":
		return "unsupported"
	case "authRequired":
		return "authRequired"
	default:
		return "availabilityUnknown"
	}
}

func reservedTurnCapabilityAliasNextAction(status string) string {
	switch strings.TrimSpace(status) {
	case "available":
		return "use"
	case "notInstalled", "setupRequired", "disabled", "authRequired":
		return "setup"
	case "disabledByAdmin", "unsupported":
		return "blocked"
	default:
		return "retry"
	}
}
