package agenthost

import (
	"context"
	"strings"
	"unicode/utf8"

	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

func (h *Host) UpdateTitle(ctx context.Context, input UpdateTitleInput) (UpdateTitleResult, error) {
	input.WorkspaceID, input.AgentSessionID = strings.TrimSpace(input.WorkspaceID), strings.TrimSpace(input.AgentSessionID)
	input.Title = strings.TrimSpace(input.Title)
	if h == nil || h.store == nil || h.runtime == nil || input.WorkspaceID == "" || input.AgentSessionID == "" {
		return UpdateTitleResult{}, ErrInvalidArgument
	}
	if utf8.RuneCountInString(input.Title) > MaxSessionTitleRunes {
		return UpdateTitleResult{}, ErrSessionTitleTooLong
	}
	canonicalSession, updated, err := h.store.UpdateSessionTitle(ctx, input.WorkspaceID, input.AgentSessionID, input.Title)
	if err != nil {
		return UpdateTitleResult{}, err
	}
	if !updated {
		return UpdateTitleResult{}, ErrSessionNotFound
	}
	result := UpdateTitleResult{Canonical: canonicalSession}
	if _, ok := h.runtime.Session(input.WorkspaceID, input.AgentSessionID); !ok {
		return result, nil
	}
	runtimeSession, err := h.runtime.SetTitle(ctx, RuntimeSetTitleInput{
		WorkspaceID: input.WorkspaceID, AgentSessionID: input.AgentSessionID, Title: canonicalSession.Title,
	})
	if err != nil {
		return UpdateTitleResult{}, err
	}
	result.Session = runtimeSession
	return result, nil
}

func (h *Host) acceptedSubmitResult(ctx context.Context, ref SessionRef, claim storesqlite.SubmitClaim) (SendInputResult, error) {
	canonicalSession, ok, err := h.store.GetSession(ctx, ref.WorkspaceID, ref.AgentSessionID)
	if err != nil {
		return SendInputResult{}, err
	}
	if !ok {
		if _, live := h.runtime.Session(ref.WorkspaceID, ref.AgentSessionID); !live {
			return SendInputResult{}, ErrSessionNotFound
		}
	}
	turn, ok, err := h.store.GetTurn(ctx, ref.WorkspaceID, ref.AgentSessionID, claim.TurnID)
	if err != nil {
		return SendInputResult{}, err
	}
	if !ok {
		return SendInputResult{}, ErrSubmitDeliveryUnknown
	}
	live, _ := h.runtime.Session(ref.WorkspaceID, ref.AgentSessionID)
	availability := SubmitAvailability{State: "available"}
	if strings.TrimSpace(canonicalSession.ActiveTurnID) != "" {
		availability = SubmitAvailability{State: "blocked", Reason: "active_turn"}
	}
	return SendInputResult{
		Session: live, Canonical: canonicalSession, Turn: &turn, TurnID: claim.TurnID,
		TurnLifecycle: lifecycleFromTurn(turn), SubmitAvailability: availability,
	}, nil
}

func (h *Host) prepareContent(workspaceID, sessionID string, content []PromptContentBlock) ([]PromptContentBlock, string, error) {
	if h.attachments == nil {
		return append([]PromptContentBlock(nil), content...), "", nil
	}
	persisted, err := h.attachments.PersistRequestContent(workspaceID, sessionID, content)
	if err != nil {
		return nil, "", err
	}
	hydrated, err := h.attachments.HydrateRuntimeContent(workspaceID, sessionID, persisted)
	if err != nil {
		return nil, "", err
	}
	return hydrated, imageOnlyDisplayText(persisted), nil
}

func (h *Host) acquireSession(ctx context.Context, ref SessionRef) (func(), error) {
	if h.locker == nil {
		return func() {}, nil
	}
	return h.locker.Acquire(ctx, ref)
}

func (h *Host) acquireStartup(ctx context.Context, provider string) (func(), error) {
	if h.startupGate == nil {
		return func() {}, nil
	}
	return h.startupGate.Acquire(ctx, provider)
}

func normalizeOptionalPromptContent(content []PromptContentBlock) ([]PromptContentBlock, string, error) {
	if len(content) == 0 {
		return nil, "", nil
	}
	return normalizePromptContent(content)
}

func createPreparationInput(workspaceID string, input CreateSessionInput) RuntimePreparationInput {
	return RuntimePreparationInput{
		WorkspaceID: workspaceID, AgentSessionID: input.AgentSessionID, AgentTargetID: input.AgentTargetID,
		Provider: input.Provider, Cwd: value(input.Cwd), Title: value(input.Title), PermissionModeID: value(input.PermissionModeID),
		PlanMode: valueBool(input.PlanMode), BrowserUse: valueBoolDefault(input.BrowserUse, true), ComputerUse: valueBoolDefault(input.ComputerUse, true),
		ProviderTargetRef: cloneMap(input.ProviderTargetRef), Model: value(input.Model), ReasoningEffort: value(input.ReasoningEffort),
		ConversationDetailMode: input.ConversationDetailMode, Metadata: cloneMap(input.Metadata), RuntimeContext: cloneMap(input.RuntimeContext),
	}
}

func resumePreparationInput(session storesqlite.Session, settings ComposerSettings) RuntimePreparationInput {
	return RuntimePreparationInput{
		WorkspaceID: session.WorkspaceID, AgentSessionID: session.ID, AgentTargetID: session.AgentTargetID,
		Provider: session.Provider, Cwd: session.Cwd, Title: session.Title, PermissionModeID: settings.PermissionModeID,
		PlanMode: settings.PlanMode, BrowserUse: valueBoolDefault(settings.BrowserUse, true), ComputerUse: valueBoolDefault(settings.ComputerUse, true),
		Model: settings.Model, ReasoningEffort: settings.ReasoningEffort, ConversationDetailMode: settings.ConversationDetailMode,
		RuntimeContext: cloneMap(session.InternalRuntimeContext), SessionOrigin: session.Origin,
		ProviderSessionID: session.ProviderSessionID, CreatedAtUnixMS: session.CreatedAtUnixMS,
		UpdatedAtUnixMS: session.UpdatedAtUnixMS, Visible: session.Metadata.Visible, Settings: settings,
		SessionMetadata: session.Metadata,
	}
}

func composerSettingsFromMap(values map[string]any) ComposerSettings {
	result := ComposerSettings{}
	result.Model, _ = values["model"].(string)
	result.PermissionModeID, _ = values["permissionModeId"].(string)
	result.PlanMode, _ = values["planMode"].(bool)
	if value, ok := values["browserUse"].(bool); ok {
		result.BrowserUse = &value
	}
	if value, ok := values["computerUse"].(bool); ok {
		result.ComputerUse = &value
	}
	result.ReasoningEffort, _ = values["reasoningEffort"].(string)
	result.Speed, _ = values["speed"].(string)
	result.ConversationDetailMode, _ = values["conversationDetailMode"].(string)
	return result
}

func lifecycleFromTurn(turn storesqlite.Turn) TurnLifecycle {
	result := TurnLifecycle{Phase: turn.Phase}
	if turnID := strings.TrimSpace(turn.TurnID); turnID != "" && turn.Phase != "settled" {
		result.ActiveTurnID = &turnID
	}
	if turn.Outcome != "" {
		outcome := turn.Outcome
		result.Outcome = &outcome
	}
	if turn.CompletedCommandKind != "" || turn.CompletedCommandStatus != "" {
		result.CompletedCommand = &CompletedCommand{Kind: turn.CompletedCommandKind, Status: turn.CompletedCommandStatus}
	}
	return result
}

func imageOnlyDisplayText(content []PromptContentBlock) string {
	count := 0
	for _, block := range content {
		if block.Type == "image" {
			count++
		}
	}
	if count == 1 {
		return "[Image]"
	}
	if count > 1 {
		return "[Images]"
	}
	return ""
}

func persistedRuntimeStatus(activeTurnID string) string {
	if strings.TrimSpace(activeTurnID) != "" {
		return "working"
	}
	return "ready"
}

func value(input *string) string {
	if input == nil {
		return ""
	}
	return strings.TrimSpace(*input)
}

func valueBool(input *bool) bool { return input != nil && *input }

func valueBoolDefault(input *bool, fallback bool) bool {
	if input == nil {
		return fallback
	}
	return *input
}

func boolPointer(value bool) *bool { return &value }

func firstMap(values ...map[string]any) map[string]any {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}
