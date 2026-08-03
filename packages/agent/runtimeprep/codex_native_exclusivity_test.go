package runtimeprep

import (
	"strings"
	"testing"
)

func TestApplyNativeCapabilityExclusivityRetainsTuttiSkillsButRemovesAutomaticDelivery(t *testing.T) {
	t.Parallel()

	input := &PrepareInput{
		BrowserUse:  true,
		ComputerUse: true,
		resolved: &resolvedCapabilities{
			Skills: []SkillSpec{
				{ID: "tutti/tutti-cli", Name: "tutti-cli"},
				{ID: "tutti/browser-use", Name: browserUseSkillName},
				{ID: "tutti/computer-use", Name: computerUseSkillName},
			},
			PolicySections: []PolicySection{
				{Key: "browser-use/handoff", Body: "browser"},
				{Key: "computer-use/handoff", Body: "computer"},
				{Key: "tutti-core-skills/x", Body: "core"},
			},
			EnvOverlay: []string{
				browserUseEnabledSessionEnv + "=1",
				computerUseEnabledSessionEnv + "=1",
				"OTHER=1",
			},
		},
	}
	plan := NativeCapabilityPlan{Entries: []NativeCapabilityPlanEntry{
		{Capability: CodexNativeCapabilityBrowser, Backend: CapabilityBackendCodexNative},
		{Capability: CodexNativeCapabilityComputer, Backend: CapabilityBackendTuttiDaemon},
	}}

	ApplyNativeCapabilityExclusivity(input, plan)

	if input.BrowserUse {
		t.Fatal("browser use should be disabled for native backend")
	}
	if !input.ComputerUse {
		t.Fatal("computer use should remain enabled for Tutti fallback")
	}
	if len(input.resolved.Skills) != 3 {
		t.Fatalf("skills = %#v", input.resolved.Skills)
	}
	var browserSkillFound bool
	for _, skill := range input.resolved.Skills {
		if skill.ID == "tutti/browser-use" {
			browserSkillFound = true
		}
	}
	if !browserSkillFound {
		t.Fatalf("browser skill should remain available for an explicit Tutti Turn: %#v", input.resolved.Skills)
	}
	if len(input.resolved.PolicySections) != 1 {
		t.Fatalf("policy = %#v", input.resolved.PolicySections)
	}
	env := strings.Join(input.resolved.EnvOverlay, ",")
	if strings.Contains(env, browserUseEnabledSessionEnv) || strings.Contains(env, computerUseEnabledSessionEnv) || !strings.Contains(env, "OTHER=1") {
		t.Fatalf("env = %#v", input.resolved.EnvOverlay)
	}
}

func TestFilterEnvForNativeCapabilityPlan(t *testing.T) {
	t.Parallel()

	plan := &NativeCapabilityPlan{Entries: []NativeCapabilityPlanEntry{
		{Capability: CodexNativeCapabilityBrowser, Backend: CapabilityBackendCodexNative},
		{Capability: CodexNativeCapabilityComputer, Backend: CapabilityBackendCodexNative},
	}}
	filtered := FilterEnvForNativeCapabilityPlan([]string{
		browserUseEnabledSessionEnv + "=1",
		computerUseEnabledSessionEnv + "=1",
		"CODEX_HOME=/tmp",
	}, plan)
	if len(filtered) != 1 || filtered[0] != "CODEX_HOME=/tmp" {
		t.Fatalf("filtered = %#v", filtered)
	}
}
