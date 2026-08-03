package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	tuttigenerated "github.com/tutti-os/tutti/services/tuttid/api/generated"
	agentactivitybiz "github.com/tutti-os/tutti/services/tuttid/biz/agentactivity"
	agentservice "github.com/tutti-os/tutti/services/tuttid/service/agent"
	agentsessionreplay "github.com/tutti-os/tutti/services/tuttid/service/agentsessionreplay"
)

func TestSessionRecordingStimuliDoNotPersistRawTurnCapabilityInvocationOrConsent(t *testing.T) {
	t.Parallel()
	consent := tuttigenerated.ExplicitSession
	updatedAt := time.UnixMilli(1_000)
	newService := func() stubAgentSessionService {
		return stubAgentSessionService{
			createFn: func(_ context.Context, _ string, input agentservice.CreateSessionInput) (agentservice.Session, error) {
				return agentservice.Session{ID: input.AgentSessionID, AgentTargetID: input.AgentTargetID, Provider: "codex", Visible: true, CreatedAt: updatedAt, UpdatedAt: &updatedAt}, nil
			},
			sendInputFn: func(_ context.Context, workspaceID, agentSessionID string, _ agentservice.SendInput) (agentservice.SendInputResult, error) {
				return agentservice.SendInputResult{Kind: "turn", TurnID: "turn-1", Session: agentservice.Session{ID: agentSessionID, Provider: "codex", Visible: true, CreatedAt: updatedAt, UpdatedAt: &updatedAt}, Turn: &agentactivitybiz.Turn{WorkspaceID: workspaceID, AgentSessionID: agentSessionID, TurnID: "turn-1", Phase: agentactivitybiz.TurnPhaseSubmitted, Origin: agentactivitybiz.TurnOriginUserPrompt, StartedAtUnixMS: 1_000, UpdatedAtUnixMS: 1_000}}, nil
			},
		}
	}
	assertNoRawCapability := func(t *testing.T, events []agentsessionreplay.ActivityEvent) {
		t.Helper()
		if len(events) != 1 {
			t.Fatalf("events = %#v", events)
		}
		raw, err := json.Marshal(events[0])
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"turnCapabilityInvocation", "explicitSession", "consent", "authorization", "AuthorizeCodexNativeComputerUse"} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("recording contains %q: %s", forbidden, raw)
			}
		}
	}

	t.Run("send", func(t *testing.T) {
		recording := &agentSessionRecordingServiceStub{}
		api := DaemonAPI{AgentSessionService: newService(), AgentSessionRecordingService: recording}
		_, err := api.SendWorkspaceAgentSessionInput(context.Background(), tuttigenerated.SendWorkspaceAgentSessionInputRequestObject{
			WorkspaceID: "workspace-1", AgentSessionID: "session-1",
			Body: &tuttigenerated.SendWorkspaceAgentSessionInputRequest{ClientSubmitId: "submit-1", Content: []tuttigenerated.AgentPromptContentBlock{{Type: "text", Text: stringPointer("computer task")}}, TurnCapabilityInvocation: &tuttigenerated.AgentTurnCapabilityInvocation{Semantic: tuttigenerated.ComputerUse, Consent: &consent}},
		})
		if err != nil {
			t.Fatal(err)
		}
		assertNoRawCapability(t, recording.events)
	})

	t.Run("create", func(t *testing.T) {
		recording := &agentSessionRecordingServiceStub{}
		api := DaemonAPI{AgentSessionService: newService(), AgentSessionRecordingService: recording}
		recordingID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
		_, err := api.CreateWorkspaceAgentSession(context.Background(), tuttigenerated.CreateWorkspaceAgentSessionRequestObject{
			WorkspaceID: "workspace-1",
			Body:        &tuttigenerated.CreateWorkspaceAgentSessionRequest{AgentSessionId: uuid.MustParse("11111111-1111-4111-8111-111111111111"), AgentTargetId: "local-codex", ClientSubmitId: "submit-1", RecordingId: &recordingID, InitialContent: []tuttigenerated.AgentPromptContentBlock{{Type: "text", Text: stringPointer("computer task")}}, TurnCapabilityInvocation: &tuttigenerated.AgentTurnCapabilityInvocation{Semantic: tuttigenerated.ComputerUse, Consent: &consent}},
		})
		if err != nil {
			t.Fatal(err)
		}
		assertNoRawCapability(t, recording.events)
	})
}

func TestTurnCapabilityInvocationFromGeneratedAcceptsOnlyKnownSemantic(t *testing.T) {
	invocation, err := turnCapabilityInvocationFromGenerated(&tuttigenerated.AgentTurnCapabilityInvocation{
		Semantic: tuttigenerated.BrowserUse,
	})
	if err != nil || invocation == nil || invocation.Semantic != "browserUse" {
		t.Fatalf("browser invocation = %#v, %v", invocation, err)
	}
	_, err = turnCapabilityInvocationFromGenerated(&tuttigenerated.AgentTurnCapabilityInvocation{
		Semantic: tuttigenerated.AgentNativeCapabilitySemantic("unknown"),
	})
	if err == nil {
		t.Fatal("unknown semantic was accepted")
	}
}

func TestTurnCapabilityInvocationFromGeneratedPreservesOnlyExplicitSessionConsent(t *testing.T) {
	consent := tuttigenerated.ExplicitSession
	invocation, err := turnCapabilityInvocationFromGenerated(&tuttigenerated.AgentTurnCapabilityInvocation{
		Semantic: tuttigenerated.ComputerUse,
		Consent:  &consent,
	})
	if err != nil || invocation == nil || invocation.Consent != "explicitSession" {
		t.Fatalf("computer consent invocation = %#v, %v", invocation, err)
	}
	forged := tuttigenerated.AgentTurnCapabilityConsent("forged")
	if _, err := turnCapabilityInvocationFromGenerated(&tuttigenerated.AgentTurnCapabilityInvocation{
		Semantic: tuttigenerated.ComputerUse,
		Consent:  &forged,
	}); err == nil {
		t.Fatal("forged consent was accepted")
	}
}

func TestSendWorkspaceAgentSessionInputForwardsAtomicTurnCapabilityInvocation(t *testing.T) {
	t.Parallel()
	var calls int
	var captured agentservice.SendInput
	updatedAt := time.UnixMilli(1_000)
	api := DaemonAPI{AgentSessionService: stubAgentSessionService{
		sendInputFn: func(_ context.Context, workspaceID, agentSessionID string, input agentservice.SendInput) (agentservice.SendInputResult, error) {
			calls++
			if workspaceID != "workspace-1" || agentSessionID != "session-1" {
				t.Fatalf("workspace/session = %q/%q", workspaceID, agentSessionID)
			}
			captured = input
			return agentservice.SendInputResult{
				Kind: "turn", TurnID: "turn-1",
				Session: agentservice.Session{ID: agentSessionID, Provider: "codex", Visible: true, CreatedAt: updatedAt, UpdatedAt: &updatedAt},
				Turn:    &agentactivitybiz.Turn{WorkspaceID: workspaceID, AgentSessionID: agentSessionID, TurnID: "turn-1", Phase: agentactivitybiz.TurnPhaseSubmitted, Origin: agentactivitybiz.TurnOriginUserPrompt, StartedAtUnixMS: 1_000, UpdatedAtUnixMS: 1_000},
			}, nil
		},
	}}
	response, err := api.SendWorkspaceAgentSessionInput(context.Background(), tuttigenerated.SendWorkspaceAgentSessionInputRequestObject{
		WorkspaceID: "workspace-1", AgentSessionID: "session-1",
		Body: &tuttigenerated.SendWorkspaceAgentSessionInputRequest{
			ClientSubmitId:           "submit-1",
			Content:                  []tuttigenerated.AgentPromptContentBlock{{Type: "text", Text: stringPointer("open browser")}},
			TurnCapabilityInvocation: &tuttigenerated.AgentTurnCapabilityInvocation{Semantic: tuttigenerated.BrowserUse},
		},
	})
	if err != nil || response == nil || calls != 1 {
		t.Fatalf("response = %#v, error = %v, calls = %d", response, err, calls)
	}
	if captured.ClientSubmitID != "submit-1" || len(captured.Content) != 1 || captured.Content[0].Text != "open browser" ||
		captured.TurnCapabilityInvocation == nil || captured.TurnCapabilityInvocation.Semantic != "browserUse" {
		t.Fatalf("captured send input = %#v", captured)
	}
}

func TestSendWorkspaceAgentSessionInputLeavesTurnIDEmptyForHostCanonicalDuplicateRecovery(t *testing.T) {
	t.Parallel()
	var captured []agentservice.SendInput
	updatedAt := time.UnixMilli(1_000)
	api := DaemonAPI{AgentSessionService: stubAgentSessionService{
		sendInputFn: func(_ context.Context, workspaceID, agentSessionID string, input agentservice.SendInput) (agentservice.SendInputResult, error) {
			captured = append(captured, input)
			return agentservice.SendInputResult{
				Kind: "turn", TurnID: "canonical-turn-1",
				Session: agentservice.Session{ID: agentSessionID, Provider: "codex", Visible: true, CreatedAt: updatedAt, UpdatedAt: &updatedAt},
				Turn:    &agentactivitybiz.Turn{WorkspaceID: workspaceID, AgentSessionID: agentSessionID, TurnID: "canonical-turn-1", Phase: agentactivitybiz.TurnPhaseSubmitted, Origin: agentactivitybiz.TurnOriginUserPrompt, StartedAtUnixMS: 1_000, UpdatedAtUnixMS: 1_000},
			}, nil
		},
	}}
	request := func() (tuttigenerated.SendWorkspaceAgentSessionInputResponseObject, error) {
		return api.SendWorkspaceAgentSessionInput(context.Background(), tuttigenerated.SendWorkspaceAgentSessionInputRequestObject{
			WorkspaceID: "workspace-1", AgentSessionID: "session-1",
			Body: &tuttigenerated.SendWorkspaceAgentSessionInputRequest{
				ClientSubmitId:           "duplicate-submit-1",
				Content:                  []tuttigenerated.AgentPromptContentBlock{{Type: "text", Text: stringPointer("open browser")}},
				TurnCapabilityInvocation: &tuttigenerated.AgentTurnCapabilityInvocation{Semantic: tuttigenerated.BrowserUse},
			},
		})
	}
	if _, err := request(); err != nil {
		t.Fatalf("first SendWorkspaceAgentSessionInput: %v", err)
	}
	if _, err := request(); err != nil {
		t.Fatalf("duplicate SendWorkspaceAgentSessionInput: %v", err)
	}
	if len(captured) != 2 {
		t.Fatalf("service calls = %d, want 2", len(captured))
	}
	for index, input := range captured {
		if input.TurnID != "" || input.ClientSubmitID != "duplicate-submit-1" {
			t.Fatalf("service input %d = %#v, want empty TurnID and stable client submit id", index, input)
		}
	}
}

func TestCreateWorkspaceAgentSessionForwardsAtomicInitialTurnCapabilityInvocation(t *testing.T) {
	t.Parallel()
	var calls int
	var captured agentservice.CreateSessionInput
	createdAt := time.UnixMilli(1_000)
	api := DaemonAPI{AgentSessionService: stubAgentSessionService{
		createFn: func(_ context.Context, workspaceID string, input agentservice.CreateSessionInput) (agentservice.Session, error) {
			calls++
			if workspaceID != "workspace-1" {
				t.Fatalf("workspace = %q", workspaceID)
			}
			captured = input
			return agentservice.Session{
				ID: input.AgentSessionID, AgentTargetID: input.AgentTargetID, Provider: "codex", Visible: true,
				CreatedAt: createdAt, UpdatedAt: &createdAt,
			}, nil
		},
	}}
	response, err := api.CreateWorkspaceAgentSession(context.Background(), tuttigenerated.CreateWorkspaceAgentSessionRequestObject{
		WorkspaceID: "workspace-1",
		Body: &tuttigenerated.CreateWorkspaceAgentSessionRequest{
			AgentSessionId:           uuid.MustParse("11111111-1111-4111-8111-111111111111"),
			AgentTargetId:            "local-codex",
			ClientSubmitId:           "submit-1",
			InitialContent:           []tuttigenerated.AgentPromptContentBlock{{Type: "text", Text: stringPointer("open browser")}},
			InitialDisplayPrompt:     stringPointer("/browser open browser"),
			TurnCapabilityInvocation: &tuttigenerated.AgentTurnCapabilityInvocation{Semantic: tuttigenerated.BrowserUse},
		},
	})
	if err != nil || response == nil || calls != 1 {
		t.Fatalf("response = %#v, error = %v, calls = %d", response, err, calls)
	}
	if captured.ClientSubmitID != "submit-1" || captured.InitialDisplayPrompt != "/browser open browser" ||
		len(captured.InitialContent) != 1 || captured.InitialContent[0].Text != "open browser" ||
		captured.TurnCapabilityInvocation == nil || captured.TurnCapabilityInvocation.Semantic != "browserUse" {
		t.Fatalf("captured create input = %#v", captured)
	}
}

func TestSendWorkspaceAgentSessionInputRejectsGuidanceCapabilityBeforeService(t *testing.T) {
	t.Parallel()
	calls := 0
	api := DaemonAPI{AgentSessionService: stubAgentSessionService{
		sendInputFn: func(context.Context, string, string, agentservice.SendInput) (agentservice.SendInputResult, error) {
			calls++
			return agentservice.SendInputResult{}, nil
		},
	}}
	guidance := true
	response, err := api.SendWorkspaceAgentSessionInput(context.Background(), tuttigenerated.SendWorkspaceAgentSessionInputRequestObject{
		WorkspaceID: "workspace-1", AgentSessionID: "session-1",
		Body: &tuttigenerated.SendWorkspaceAgentSessionInputRequest{
			ClientSubmitId: "submit-1", Content: []tuttigenerated.AgentPromptContentBlock{{Type: "text", Text: stringPointer("guide")}},
			Guidance: &guidance, TurnCapabilityInvocation: &tuttigenerated.AgentTurnCapabilityInvocation{Semantic: tuttigenerated.BrowserUse},
		},
	})
	if err != nil || calls != 0 {
		t.Fatalf("response = %#v, error = %v, calls = %d", response, err, calls)
	}
	if _, ok := response.(tuttigenerated.SendWorkspaceAgentSessionInput400JSONResponse); !ok {
		t.Fatalf("response = %#v, want 400", response)
	}
}
