package agent

import (
	"context"
	"reflect"
	"testing"

	agentsessionstore "github.com/tutti-os/tutti/packages/agent/daemon/activity"
	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	runtimeprep "github.com/tutti-os/tutti/packages/agent/runtimeprep"
	"github.com/tutti-os/tutti/packages/agent/store-sqlite/canonical"
	agentactivitybiz "github.com/tutti-os/tutti/services/tuttid/biz/agentactivity"
	workspacebiz "github.com/tutti-os/tutti/services/tuttid/biz/workspace"
)

func TestTurnCapabilityStatesFromRuntimeContextProjectsOnlyAuthorizedComputerBinding(t *testing.T) {
	t.Parallel()
	context := map[string]any{
		runtimeprep.CodexTurnCapabilityBindingsRuntimeContextKey: []map[string]any{
			{
				"semantic": runtimeprep.CodexNativeCapabilityComputer,
				"loaded":   true, "ready": true,
				"consent": string(runtimeprep.CodexTurnCapabilityConsentExplicitSession),
			},
			{
				"semantic": runtimeprep.CodexNativeCapabilityBrowser,
				"loaded":   true, "ready": true,
			},
		},
	}
	got := turnCapabilityStatesFromRuntimeContext(context)
	want := []TurnCapabilityState{{Semantic: runtimeprep.CodexTurnCapabilitySemanticComputerUse, State: "bound"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projection = %#v, want %#v", got, want)
	}
}

func TestTurnCapabilityServiceValidationRejectsForgedAndInitialUnconsentedComputer(t *testing.T) {
	t.Parallel()
	for _, input := range []struct {
		name    string
		initial bool
		value   *agenthost.TurnCapabilityInvocation
	}{
		{
			name: "forged consent", initial: false,
			value: &agenthost.TurnCapabilityInvocation{Semantic: runtimeprep.CodexTurnCapabilitySemanticComputerUse, Consent: "forged"},
		},
		{
			name: "initial computer without consent", initial: true,
			value: &agenthost.TurnCapabilityInvocation{Semantic: runtimeprep.CodexTurnCapabilitySemanticComputerUse},
		},
	} {
		t.Run(input.name, func(t *testing.T) {
			if err := validateTurnCapabilityInvocationForService(input.value, input.initial); err == nil {
				t.Fatal("invalid invocation passed product admission")
			}
		})
	}
}

func TestTurnCapabilityStatesFromRuntimeContextProjectsDurableComputerAuthorizationWithoutLiveReadiness(t *testing.T) {
	t.Parallel()
	context := map[string]any{
		runtimeprep.CodexTurnCapabilityBindingsRuntimeContextKey: []runtimeprep.CodexTurnCapabilityBinding{{
			Semantic:   runtimeprep.CodexNativeCapabilityComputer,
			Authorized: true,
		}},
	}
	if got := turnCapabilityStatesFromRuntimeContext(context); !reflect.DeepEqual(got, []TurnCapabilityState{{
		Semantic: runtimeprep.CodexTurnCapabilitySemanticComputerUse, State: "bound",
	}}) {
		t.Fatalf("durable authorization projection = %#v", got)
	}
}

func TestServiceSessionProjectsDurableCanonicalComputerAuthorization(t *testing.T) {
	t.Parallel()
	session := serviceSession(
		ProviderRuntimeSession{
			ID: "session-1", Provider: "codex", RuntimeContext: map[string]any{
				runtimeprep.CodexTurnCapabilityBindingsRuntimeContextKey: []runtimeprep.CodexTurnCapabilityBinding{{
					Semantic:   runtimeprep.CodexNativeCapabilityComputer,
					Authorized: true,
				}},
			},
		},
		true,
	)
	if !reflect.DeepEqual(session.TurnCapabilityStates, []TurnCapabilityState{{
		Semantic: runtimeprep.CodexTurnCapabilitySemanticComputerUse, State: "bound",
	}}) {
		t.Fatalf("live projection = %#v", session.TurnCapabilityStates)
	}
}

func TestCanonicalStatePatchPersistsComputerBindingForResume(t *testing.T) {
	ctx := context.Background()
	store := openAgentServiceSQLiteStore(t)
	if err := store.Create(ctx, workspacebiz.Summary{ID: "workspace-1", Name: "Capability resume"}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	projection := NewActivityProjection(store)
	if err := projection.Report(ctx, agentsessionstore.ReportActivityInput{
		WorkspaceID: "workspace-1",
		Source: canonical.EventSource{
			AgentID: "session-1", Provider: "codex",
			SessionOrigin: agentsessionstore.WorkspaceAgentSessionOriginRuntime,
		},
		StatePatches: []agentsessionstore.WorkspaceAgentStatePatch{{
			AgentSessionID: "session-1", Kind: agentactivitybiz.SessionKindRoot,
			Provider: "codex", ProviderSessionID: "provider-session-1",
			LifecycleStatus: "ready", CurrentPhase: "idle", OccurredAtUnixMS: 1,
			RuntimeContext: map[string]any{
				runtimeprep.CodexTurnCapabilityBindingsRuntimeContextKey: []map[string]any{{
					"semantic": runtimeprep.CodexNativeCapabilityComputer,
					"loaded":   true, "ready": true,
					"consent": string(runtimeprep.CodexTurnCapabilityConsentExplicitSession),
				}},
			},
		}},
	}); err != nil {
		t.Fatalf("persist runtime state patch: %v", err)
	}
	persisted, found := projection.GetSession("workspace-1", "session-1")
	if !found {
		t.Fatal("canonical session was not persisted")
	}
	resume := runtimeResumeInputFromPersistedSession(persisted)
	states := turnCapabilityStatesFromRuntimeContext(resume.RuntimeContext)
	if !reflect.DeepEqual(states, []TurnCapabilityState{{
		Semantic: runtimeprep.CodexTurnCapabilitySemanticComputerUse, State: "bound",
	}}) {
		t.Fatalf("persisted resume context = %#v, states = %#v", resume.RuntimeContext, states)
	}
}
