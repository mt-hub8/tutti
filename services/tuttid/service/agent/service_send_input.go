package agent

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	agentactivitybiz "github.com/tutti-os/tutti/services/tuttid/biz/agentactivity"
	agentproviderbiz "github.com/tutti-os/tutti/services/tuttid/biz/agentprovider"
	agenttargetbiz "github.com/tutti-os/tutti/services/tuttid/biz/agenttarget"
	tuttimodeactivationbiz "github.com/tutti-os/tutti/services/tuttid/biz/tuttimodeactivation"
)

func (s *Service) SendInput(ctx context.Context, workspaceID string, agentSessionID string, input SendInput) (SendInputResult, error) {
	input.ClientSubmitID = strings.TrimSpace(input.ClientSubmitID)
	if input.ClientSubmitID == "" {
		legacyClientSubmitID, _ := input.Metadata["clientSubmitId"].(string)
		input.ClientSubmitID = strings.TrimSpace(legacyClientSubmitID)
	}
	if input.ClientSubmitID == "" {
		input.ClientSubmitID = uuid.NewString()
	}
	if invocation := input.TurnCapabilityInvocation; invocation != nil {
		if input.Guidance || validateTurnCapabilityInvocationForService(invocation, false) != nil {
			return SendInputResult{}, ErrInvalidArgument
		}
		// Binding a frozen Tutti-mode snapshot is a durable product effect. Do
		// the exact built-in target check before that binding so a non-Codex
		// session (including a custom target that merely calls itself Codex)
		// cannot enter the capability lifecycle at all.
		if err := s.validateExistingCodexTurnCapabilityTarget(ctx, workspaceID, agentSessionID); err != nil {
			return SendInputResult{}, err
		}
	}
	logAgentSubmitTrace("service.send.entered", workspaceID, agentSessionID, input.ClientSubmitID, input.Metadata, nil)
	nodeStartedAt := time.Now()
	normalizedContent, _, err := normalizePromptContent(input.Content)
	if err != nil {
		s.reportAgentServiceNodeFailure(ctx, agentSessionID, "message_send", "content_normalized", "", nodeStartedAt, err)
		return SendInputResult{}, err
	}
	s.reportAgentServiceNodeSuccess(ctx, agentSessionID, "message_send", "content_normalized", "", nodeStartedAt)
	logAgentSubmitTrace("service.send.content_normalized", workspaceID, agentSessionID, input.ClientSubmitID, input.Metadata, map[string]any{
		"content_block_count": len(normalizedContent),
	})
	hostInput := agenthost.SendInput{
		CapabilityRefs: append([]CapabilityReference(nil), input.CapabilityRefs...),
		Content:        normalizedContent, DisplayPrompt: input.DisplayPrompt,
		Metadata: cloneMetadata(input.Metadata), ClientSubmitID: input.ClientSubmitID, Guidance: input.Guidance,
		TurnID: input.TurnID, TurnCapabilityInvocation: input.TurnCapabilityInvocation,
	}
	var preparedTurnID string
	var preparedSnapshot tuttimodeactivationbiz.TurnSnapshot
	preparedSnapshotBound := false
	if _, typedGoal := agenthost.ParseTypedGoalControl(normalizedContent, input.Guidance); !typedGoal {
		runtimeSession, _ := s.controller().Session(workspaceID, agentSessionID)
		existingCanonicalTurnID, claimErr := s.existingSubmitCanonicalTurnID(ctx, workspaceID, agentSessionID, input.ClientSubmitID, input.Metadata)
		if claimErr != nil {
			return SendInputResult{}, claimErr
		}
		if existingCanonicalTurnID != "" {
			preparedTurnID = existingCanonicalTurnID
			hostInput.TurnID = existingCanonicalTurnID
		} else {
			preparedTurnID, preparedSnapshot, err = s.prepareTuttiModeExec(ctx, workspaceID, agentSessionID, input.Guidance, runtimeSession, input.TurnID)
			if err != nil {
				return SendInputResult{}, err
			}
			preparedSnapshotBound = !input.Guidance && s.TuttiModeActivations != nil
			hostInput.TurnID = preparedTurnID
			hostInput.TuttiModeSnapshot = runtimeTuttiModeTurnSnapshot(preparedSnapshot)
		}
	}
	hostResult, err := s.ApplicationHost().SendInput(ctx,
		agenthost.SessionRef{WorkspaceID: workspaceID, AgentSessionID: agentSessionID},
		hostInput,
	)
	if err != nil {
		if preparedSnapshotBound && !errors.Is(err, agenthost.ErrSubmitDeliveryUnknown) && !errors.Is(err, ErrSubmitDeliveryUnknown) {
			if abandonErr := s.abandonPreparedTuttiModeExec(context.WithoutCancel(ctx), workspaceID, agentSessionID, preparedTurnID, preparedSnapshot, input.Guidance); abandonErr != nil {
				return SendInputResult{}, deliveryUnknownError(abandonErr)
			}
		}
		if recovered := turnCapabilityRecoveryError(err); recovered != err {
			return SendInputResult{}, recovered
		}
		if errors.Is(err, agenthost.ErrTurnCapabilityRejected) || errors.Is(err, agenthost.ErrTurnCapabilityUnsupported) {
			return SendInputResult{}, ErrInvalidArgument
		}
		return SendInputResult{}, err
	}
	if hostResult.Kind == "goalControl" && hostResult.GoalControl != nil {
		session, getErr := s.Get(ctx, workspaceID, agentSessionID)
		if getErr != nil {
			return SendInputResult{}, getErr
		}
		goal := GoalControlSessionResult{
			Session: session, Goal: clonePayload(hostResult.GoalControl.Goal),
			OperationID: hostResult.GoalControl.OperationID, GoalState: hostResult.GoalControl.GoalState,
		}
		return SendInputResult{Session: session, Kind: "goalControl", GoalControl: &goal}, nil
	}
	if preparedTurnID != "" && strings.TrimSpace(hostResult.TurnID) != preparedTurnID {
		return SendInputResult{}, ErrSubmitDeliveryUnknown
	}
	if preparedSnapshotBound {
		if _, acceptErr := s.TuttiModeActivations.AcceptTurnSnapshot(ctx, workspaceID, agentSessionID, preparedTurnID); acceptErr != nil {
			return SendInputResult{}, deliveryUnknownError(acceptErr)
		}
	}
	turnID := hostResult.TurnID
	provider := strings.TrimSpace(hostResult.Session.Provider)
	logAgentSubmitTrace("service.send.runtime_session_ready", workspaceID, agentSessionID, input.ClientSubmitID, input.Metadata, nil)
	logAgentSubmitTrace("service.send.prompt_validated", workspaceID, agentSessionID, input.ClientSubmitID, input.Metadata, nil)
	logAgentSubmitTrace("service.send.prompt_prepared", workspaceID, agentSessionID, input.ClientSubmitID, input.Metadata, map[string]any{"content_block_count": len(normalizedContent)})
	logAgentSubmitTrace("service.send.exec_resolved", workspaceID, agentSessionID, input.ClientSubmitID, input.Metadata, map[string]any{
		"turn_id": turnID, "session_status": hostResult.Session.Status, "turn_phase": hostResult.TurnLifecycle.Phase,
	})
	nodeStartedAt = time.Now()
	session, err := s.Get(ctx, workspaceID, agentSessionID)
	if err != nil {
		s.reportAgentServiceNodeFailure(ctx, agentSessionID, "message_send", "session_refreshed", provider, nodeStartedAt, err)
		return SendInputResult{}, err
	}
	turn, err := s.exactSubmittedTurn(ctx, workspaceID, agentSessionID, turnID, session)
	if err != nil {
		s.reportAgentServiceNodeFailure(ctx, agentSessionID, "message_send", "turn_refreshed", provider, nodeStartedAt, err)
		return SendInputResult{}, err
	}
	s.reportAgentServiceNodeSuccess(ctx, agentSessionID, "message_send", "session_refreshed", provider, nodeStartedAt)
	s.observeTuttiModeSourceUserTurn(
		ctx, workspaceID, agentSessionID,
		input.ClientSubmitID, input.Metadata, turn,
	)
	return SendInputResult{
		Session:            session,
		Kind:               "turn",
		TurnID:             turnID,
		Turn:               turn,
		TurnLifecycle:      hostResult.TurnLifecycle,
		SubmitAvailability: hostResult.SubmitAvailability,
	}, nil
}

// validateExistingCodexTurnCapabilityTarget resolves the persisted launch
// identity without preparing a runtime. It is deliberately limited to the
// capability-only admission precondition; ordinary sends retain their legacy
// path unchanged.
func (s *Service) validateExistingCodexTurnCapabilityTarget(ctx context.Context, workspaceID, agentSessionID string) error {
	if s == nil || s.SessionReader == nil {
		return ErrInvalidArgument
	}
	persisted, found := s.SessionReader.GetSession(strings.TrimSpace(workspaceID), strings.TrimSpace(agentSessionID))
	if !found {
		return ErrInvalidArgument
	}
	providerTargetRef, err := s.resolveProviderTargetRefForResume(ctx, persisted)
	if err != nil {
		return ErrInvalidArgument
	}
	if !isAuthoritativeCodexTurnCapabilityTarget(persisted.AgentTargetID, persisted.Provider, providerTargetRef) {
		return ErrInvalidArgument
	}
	return nil
}

// isAuthoritativeCodexTurnCapabilityTarget accepts only the built-in local
// Codex launch target resolved by the Agent Target authority. Provider text on
// its own is descriptive metadata and is intentionally insufficient here.
func isAuthoritativeCodexTurnCapabilityTarget(harnessTargetID string, provider string, providerTargetRef map[string]any) bool {
	refProvider, _ := providerTargetRef["provider"].(string)
	refTargetID, _ := providerTargetRef["targetId"].(string)
	canonicalProvider := agentproviderbiz.Normalize(provider)
	resolvedProvider := agentproviderbiz.Normalize(refProvider)
	return strings.TrimSpace(harnessTargetID) == agenttargetbiz.IDLocalCodex &&
		providerTargetRefKind(providerTargetRef) == agenttargetbiz.LaunchRefTypeBuiltinLocal &&
		canonicalProvider != "" && canonicalProvider == resolvedProvider &&
		strings.TrimSpace(refTargetID) == agenttargetbiz.IDLocalCodex
}

func (s *Service) observeTuttiModeSourceUserTurn(
	ctx context.Context,
	workspaceID string,
	agentSessionID string,
	clientSubmitID string,
	metadata map[string]any,
	turn *agentactivitybiz.Turn,
) {
	if s == nil || s.TuttiModeSourceActivity == nil || turn == nil ||
		strings.TrimSpace(turn.TurnID) == "" {
		return
	}
	internalWake, _ := metadata["tuttiModeExecutionWake"].(bool)
	if internalWake {
		return
	}
	message, ok := s.canonicalSubmittedUserMessage(
		workspaceID, agentSessionID, turn.TurnID, clientSubmitID, metadata,
	)
	if !ok || message.OccurredAtUnixMS <= 0 {
		return
	}
	if err := s.TuttiModeSourceActivity.ObserveTuttiModeSourceActivity(
		ctx,
		TuttiModeSourceActivity{
			WorkspaceID:      strings.TrimSpace(workspaceID),
			SessionID:        strings.TrimSpace(agentSessionID),
			Kind:             "user_turn",
			ActivityID:       strings.TrimSpace(message.MessageID),
			OccurredAtUnixMS: message.OccurredAtUnixMS,
		},
	); err != nil {
		slog.WarnContext(
			ctx,
			"observe Tutti mode source user Turn failed",
			"event", "tutti_mode_execution.source_user_turn_observation_failed",
			"workspaceId", workspaceID,
			"agentSessionId", agentSessionID,
			"error", err,
		)
	}
}

func (s *Service) canonicalSubmittedUserMessage(
	workspaceID string,
	agentSessionID string,
	turnID string,
	clientSubmitID string,
	metadata map[string]any,
) (SessionMessage, bool) {
	if s == nil || s.MessageReader == nil {
		return SessionMessage{}, false
	}
	clientSubmitID = strings.TrimSpace(clientSubmitID)
	if clientSubmitID == "" {
		legacyClientSubmitID, _ := metadata["clientSubmitId"].(string)
		clientSubmitID = strings.TrimSpace(legacyClientSubmitID)
	}
	page, ok := s.MessageReader.ListSessionMessages(
		agentactivitybiz.ListSessionMessagesInput{
			WorkspaceID:    strings.TrimSpace(workspaceID),
			AgentSessionID: strings.TrimSpace(agentSessionID),
			TurnID:         strings.TrimSpace(turnID),
			Limit:          defaultListMessagesLimit,
			Order:          agentactivitybiz.MessageOrderDesc,
		},
	)
	if !ok {
		return SessionMessage{}, false
	}
	for _, message := range page.Messages {
		if strings.TrimSpace(message.TurnID) != strings.TrimSpace(turnID) ||
			strings.TrimSpace(message.Role) != "user" ||
			message.OccurredAtUnixMS <= 0 {
			continue
		}
		if clientSubmitID != "" {
			messageClientSubmitID, _ := message.Payload["clientSubmitId"].(string)
			if strings.TrimSpace(messageClientSubmitID) != clientSubmitID {
				continue
			}
		}
		return message, true
	}
	return SessionMessage{}, false
}

func (s *Service) exactSubmittedTurn(
	ctx context.Context,
	workspaceID string,
	agentSessionID string,
	turnID string,
	session Session,
) (*agentactivitybiz.Turn, error) {
	if s.TurnStore != nil {
		turn, ok, err := s.TurnStore.GetTurn(ctx, workspaceID, agentSessionID, turnID)
		if err != nil {
			return nil, err
		}
		if !ok || strings.TrimSpace(turn.TurnID) != turnID {
			return nil, ErrSubmitDeliveryUnknown
		}
		return &turn, nil
	}
	// Standalone service tests may omit the durable store. Prefer an exact
	// entity already attached to the session, but never synthesize one.
	for _, turn := range []*agentactivitybiz.Turn{session.ActiveTurn, session.LatestTurn} {
		if turn != nil && strings.TrimSpace(turn.TurnID) == turnID {
			return turn, nil
		}
	}
	return nil, nil
}
