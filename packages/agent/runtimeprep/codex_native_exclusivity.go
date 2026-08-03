package runtimeprep

import "strings"

// ApplyNativeCapabilityExclusivity removes automatic Tutti browser/computer
// delivery from a Codex runtime. The managed skill files deliberately remain:
// a later immutable Turn plan may select Tutti and invoke one explicitly.
// Sites has no Tutti counterpart.
func ApplyNativeCapabilityExclusivity(input *PrepareInput, plan NativeCapabilityPlan) {
	if input == nil {
		return
	}
	if plan.Backend(CodexNativeCapabilityBrowser) == CapabilityBackendCodexNative {
		input.BrowserUse = false
	}
	if plan.Backend(CodexNativeCapabilityComputer) == CapabilityBackendCodexNative {
		input.ComputerUse = false
	}
	if input.resolved == nil {
		return
	}
	filteredSections := make([]PolicySection, 0, len(input.resolved.PolicySections))
	for _, section := range input.resolved.PolicySections {
		key := strings.TrimSpace(section.Key)
		if strings.HasPrefix(key, "browser-use/") {
			continue
		}
		if strings.HasPrefix(key, "computer-use/") {
			continue
		}
		filteredSections = append(filteredSections, section)
	}
	input.resolved.PolicySections = filteredSections

	filteredEnv := make([]string, 0, len(input.resolved.EnvOverlay))
	for _, entry := range input.resolved.EnvOverlay {
		if strings.HasPrefix(entry, browserUseEnabledSessionEnv+"=") {
			continue
		}
		if strings.HasPrefix(entry, computerUseEnabledSessionEnv+"=") {
			continue
		}
		filteredEnv = append(filteredEnv, entry)
	}
	input.resolved.EnvOverlay = filteredEnv
}

// FilterEnvForNativeCapabilityPlan removes automatic Tutti capability markers
// from Codex runtime process environments. Slash routing selects its backend
// through a structured Turn input, never through an inherited env marker.
func FilterEnvForNativeCapabilityPlan(env []string, plan *NativeCapabilityPlan) []string {
	if plan == nil || len(env) == 0 {
		return env
	}
	result := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, browserUseEnabledSessionEnv+"=") {
			continue
		}
		if strings.HasPrefix(entry, computerUseEnabledSessionEnv+"=") {
			continue
		}
		result = append(result, entry)
	}
	return result
}
