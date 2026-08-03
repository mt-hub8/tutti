package api

import (
	"testing"

	agentservice "github.com/tutti-os/tutti/services/tuttid/service/agent"
)

func TestGeneratedAgentProviderCapabilityOptionsPreservesNativeSemantic(t *testing.T) {
	options := generatedAgentProviderCapabilityOptions([]agentservice.ComposerCapabilityOption{{
		ID:         "plugin:browser@openai-bundled",
		Kind:       "plugin",
		Name:       "browser",
		Label:      "Browser",
		Status:     "available",
		Invocation: "promptItem",
		Semantic:   "browserUse",
	}})
	if len(options) != 1 || options[0].Semantic == nil || string(*options[0].Semantic) != "browserUse" {
		t.Fatalf("capability semantic = %#v", options)
	}
	if options[0].InvocationScope != nil {
		t.Fatalf("create-only capability scope = %#v, want omitted", options[0])
	}
}

func TestGeneratedAgentProviderCapabilityOptionsProjectsTurnScope(t *testing.T) {
	options := generatedAgentProviderCapabilityOptions([]agentservice.ComposerCapabilityOption{{
		ID: "plugin:browser@openai-bundled", Kind: "plugin", Name: "browser", Label: "Browser",
		Status: "available", Invocation: "promptItem", Semantic: "browserUse", InvocationScope: "turn",
	}})
	if len(options) != 1 || options[0].InvocationScope == nil || string(*options[0].InvocationScope) != "turn" {
		t.Fatalf("turn capability scope = %#v", options)
	}
}

func TestGeneratedAgentProviderCapabilityOptionsProjectsExplicitSessionConsentRequirement(t *testing.T) {
	options := generatedAgentProviderCapabilityOptions([]agentservice.ComposerCapabilityOption{{
		ID: "plugin:computer-use@openai-bundled", Kind: "plugin", Name: "computer-use", Label: "Computer",
		Status: "available", Invocation: "promptItem", Semantic: "computerUse", InvocationScope: "turn", ConsentRequirement: "explicitSession",
	}})
	if len(options) != 1 || options[0].ConsentRequirement == nil || string(*options[0].ConsentRequirement) != "explicitSession" {
		t.Fatalf("consent requirement = %#v", options)
	}
}

func TestGeneratedAgentReservedTurnCapabilityAliasesPreservePresentationOnlyFields(t *testing.T) {
	aliases := generatedAgentReservedTurnCapabilityAliases([]agentservice.ComposerReservedTurnCapabilityAlias{{
		Alias: "/browser", Semantic: "browserUse", Name: "browser", Label: "Browser",
		Status: "unknown", Reason: "inventory unavailable", NextAction: "retry",
		Invocation: "promptItem", InvocationScope: "turn",
	}})
	if len(aliases) != 1 || aliases[0].Alias != "/browser" || string(aliases[0].Semantic) != "browserUse" || aliases[0].NextAction == nil || string(*aliases[0].NextAction) != "retry" {
		t.Fatalf("reserved aliases = %#v", aliases)
	}
}

// Requested-origin model entries (warm-catalog append of the requested model,
// bootstrap echo) must keep their provenance across the API projection so
// clients can exclude them from catalog testimony; catalog entries omit the
// field entirely (backward-compatible optional).
func TestGeneratedComposerConfigOptionKeepsRequestedProvenance(t *testing.T) {
	generated := generatedComposerConfigOption(agentservice.ComposerConfigOption{
		Configurable:   true,
		CurrentValue:   "default",
		EffectiveValue: "claude-haiku-4-5-20251001",
		Options: []agentservice.ComposerConfigOptionValue{
			{ID: "gpt-5.6-sol", Label: "GPT-5.6 Sol", Value: "gpt-5.6-sol"},
			{ID: "x-ai/grok-4.5", Label: "x-ai/grok-4.5", Value: "x-ai/grok-4.5", Requested: true},
		},
	})
	if len(generated.Options) != 2 {
		t.Fatalf("expected both options, got %d", len(generated.Options))
	}
	if generated.Options[0].Requested != nil {
		t.Fatal("catalog entry must omit the requested field")
	}
	if generated.Options[1].Requested == nil || !*generated.Options[1].Requested {
		t.Fatal("requested-origin entry must project requested=true")
	}
	if generated.CurrentValue == nil || *generated.CurrentValue != "default" {
		t.Fatalf("current value = %#v, want default", generated.CurrentValue)
	}
	if generated.EffectiveValue == nil ||
		*generated.EffectiveValue != "claude-haiku-4-5-20251001" {
		t.Fatalf("effective value = %#v, want resolved Haiku model", generated.EffectiveValue)
	}
}
