package agent

import (
	"testing"

	agenttargetbiz "github.com/tutti-os/tutti/services/tuttid/biz/agenttarget"
)

func TestComposerReservedTurnCapabilityAliasesFailClosedWithoutInventory(t *testing.T) {
	aliases := composerReservedTurnCapabilityAliases(
		agenttargetbiz.IDLocalCodex,
		"codex",
		codexReservedAliasTargetRef(),
		nil,
		[]string{"inventory failed"},
	)
	if len(aliases) != 3 {
		t.Fatalf("aliases = %#v, want Browser/Computer/Sites", aliases)
	}
	for _, alias := range aliases {
		if alias.Status != "unknown" || alias.InvocationScope != "turn" || alias.Invocation != "promptItem" || alias.NextAction != "retry" {
			t.Fatalf("fail-closed alias = %#v", alias)
		}
	}
}

func TestComposerReservedTurnCapabilityAliasesMergeOnlyAvailability(t *testing.T) {
	aliases := composerReservedTurnCapabilityAliases(
		agenttargetbiz.IDLocalCodex,
		"codex",
		codexReservedAliasTargetRef(),
		[]ComposerCapabilityOption{
			{Semantic: "browserUse", Name: "different-browser", Label: "Browser from inventory", Status: "disabled", Invocation: "none", Trigger: "$unchanged"},
			{Semantic: "computerUse", Name: "computer", Label: "Computer", Status: "notInstalled", Invocation: "none"},
			{Semantic: "sites", Name: "sites", Label: "Sites", Status: "disabledByAdmin", Invocation: "none"},
		},
		nil,
	)
	if aliases[0].Alias != "/browser" || aliases[0].Status != "disabled" || aliases[0].NextAction != "setup" {
		t.Fatalf("browser alias = %#v", aliases[0])
	}
	if aliases[1].Alias != "/computer" || aliases[1].Status != "notInstalled" || aliases[1].ConsentRequirement != "explicitSession" {
		t.Fatalf("computer alias = %#v", aliases[1])
	}
	if aliases[2].Alias != "/sites" || aliases[2].Status != "disabledByAdmin" || aliases[2].NextAction != "blocked" {
		t.Fatalf("sites alias = %#v", aliases[2])
	}
}

func TestComposerReservedTurnCapabilityAliasesLeaveNonCodexUntouched(t *testing.T) {
	if aliases := composerReservedTurnCapabilityAliases("custom-codex", "codex", nil, nil, nil); aliases != nil {
		t.Fatalf("non-authoritative target aliases = %#v, want nil", aliases)
	}
}

func codexReservedAliasTargetRef() map[string]any {
	return map[string]any{
		"kind":     agenttargetbiz.LaunchRefTypeBuiltinLocal,
		"provider": "codex",
		"targetId": agenttargetbiz.IDLocalCodex,
	}
}
