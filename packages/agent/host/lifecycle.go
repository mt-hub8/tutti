package agenthost

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

func (h *Host) CreateSession(ctx context.Context, workspaceID string, input CreateSessionInput) (CreateSessionResult, error) {
	workspaceID, input.AgentSessionID = strings.TrimSpace(workspaceID), strings.TrimSpace(input.AgentSessionID)
	input.Provider, input.AgentTargetID = strings.TrimSpace(input.Provider), strings.TrimSpace(input.AgentTargetID)
	if h == nil || h.runtime == nil || h.store == nil || workspaceID == "" || input.AgentSessionID == "" || input.Provider == "" {
		return CreateSessionResult{}, ErrInvalidArgument
	}
	var err error
	input.RailPlacement, err = normalizeRailPlacement(input.RailPlacement)
	if err != nil {
		return CreateSessionResult{}, err
	}
	ref := SessionRef{WorkspaceID: workspaceID, AgentSessionID: input.AgentSessionID}
	normalized, promptText, err := normalizeOptionalPromptContent(input.InitialContent)
	if err != nil {
		return CreateSessionResult{}, err
	}
	typedGoal, isTypedGoal := ParseTypedGoalControl(normalized, false)
	if input.TurnCapabilityInvocation != nil && (isTypedGoal || len(normalized) == 0) {
		return CreateSessionResult{}, ErrInvalidArgument
	}
	metadata := submissionMetadata(input.Metadata, input.ClientSubmitID)
	goalMetadata := clonePayload(metadata)
	claimMetadata := metadata
	if isTypedGoal || len(normalized) == 0 {
		normalized = nil
		claimMetadata = nil
	}
	if len(normalized) > 0 {
		input.TurnID, err = h.canonicalTurnIDForSubmitClaim(ctx, ref, claimMetadata, input.TurnID)
		if err != nil {
			return CreateSessionResult{}, err
		}
		if strings.TrimSpace(input.TurnID) == "" {
			input.TurnID = uuid.NewString()
		}
	}
	invocation, hasTurnCapability, err := h.validatedTurnCapabilityInvocation(
		input.TurnCapabilityInvocation,
		false,
		input.TurnID,
		input.ClientSubmitID,
	)
	if err != nil {
		return CreateSessionResult{}, err
	}
	claim, claimPending, err := h.prepareSubmitClaim(ctx, ref, claimMetadata, input.TurnID)
	if err != nil {
		if errors.Is(err, storesqlite.ErrSubmitClaimTurnConflict) {
			return CreateSessionResult{}, errors.Join(ErrSubmitDeliveryUnknown, err)
		}
		return CreateSessionResult{}, err
	}
	if claim.ClientSubmitID != "" && !claimPending {
		if claim.Status != "accepted" {
			return CreateSessionResult{}, ErrSubmitDeliveryUnknown
		}
		canonicalSession, _, readErr := h.store.GetSession(ctx, workspaceID, input.AgentSessionID)
		if readErr != nil {
			return CreateSessionResult{}, readErr
		}
		if !railPlacementMatchesSession(input.RailPlacement, canonicalSession) {
			return CreateSessionResult{}, ErrRailPlacementConflict
		}
		runtimeSession, _ := h.runtime.Session(workspaceID, input.AgentSessionID)
		return CreateSessionResult{Session: runtimeSession, Canonical: canonicalSession, TurnID: claim.TurnID}, nil
	}
	defer func() {
		if claimPending {
			h.abandonSubmitClaim(ref, claim.ClientSubmitID)
		}
	}()
	releaseSession, err := h.acquireSession(ctx, ref)
	if err != nil {
		return CreateSessionResult{}, err
	}
	defer releaseSession()
	capabilityPlan := RuntimeTurnCapabilityPlan{}
	if hasTurnCapability {
		if claim.TurnCapabilityPlanJSON != "" {
			capabilityPlan, err = decodeTurnCapabilityPlan(claim.TurnCapabilityPlanJSON)
			if err != nil {
				return CreateSessionResult{}, err
			}
		} else {
			startedAt := h.now()
			plan, err := h.admitTurnCapability(ctx, RuntimeTurnCapabilityAdmissionInput{
				WorkspaceID: workspaceID, AgentSessionID: input.AgentSessionID,
				TurnID: input.TurnID, ClientSubmitID: input.ClientSubmitID, Initial: true,
				AgentTargetID: input.AgentTargetID, Provider: input.Provider,
				ProviderTargetRef: cloneMap(input.ProviderTargetRef), RuntimeContext: cloneMap(input.RuntimeContext),
				Invocation: *invocation,
			})
			h.observeStep(ctx, "session_create", "turn_capability_admitted", input.AgentSessionID, input.Provider, startedAt, err)
			if err != nil {
				if errors.Is(err, ErrSubmitDeliveryUnknown) {
					claimPending = false
				}
				return CreateSessionResult{}, err
			}
			encodedPlan, encodeErr := encodeTurnCapabilityPlan(plan)
			if encodeErr != nil {
				return CreateSessionResult{}, encodeErr
			}
			claim, _, err = h.store.SetSubmitClaimCapabilityPlan(ctx, ref.WorkspaceID, ref.AgentSessionID, claim.ClientSubmitID, encodedPlan, h.now().UnixMilli())
			if err != nil {
				return CreateSessionResult{}, err
			}
			capabilityPlan = plan
		}
	}

	prepared := PreparedRuntime{Cwd: strings.TrimSpace(value(input.Cwd))}
	if h.preparation != nil {
		prepared, err = h.preparation.Prepare(ctx, createPreparationInput(workspaceID, input))
		if err != nil {
			return CreateSessionResult{}, err
		}
	}
	cleanup := func(cause error, started bool, canonicalCreated bool) error {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		var cleanupErrs []error
		cleanupErrs = append(cleanupErrs, cause)
		if started {
			cleanupErrs = append(cleanupErrs, h.runtime.Close(cleanupCtx, RuntimeCloseInput{WorkspaceID: workspaceID, AgentSessionID: input.AgentSessionID}))
		}
		if canonicalCreated {
			_, deleteErr := h.store.RollbackRuntimeSessionInitialization(cleanupCtx, workspaceID, input.AgentSessionID)
			cleanupErrs = append(cleanupErrs, deleteErr)
		}
		if h.preparation != nil {
			cleanupErrs = append(cleanupErrs, h.preparation.Cleanup(cleanupCtx, RuntimeCleanupInput{
				WorkspaceID: workspaceID, AgentSessionID: input.AgentSessionID, Provider: input.Provider,
			}))
		}
		return errors.Join(cleanupErrs...)
	}

	startedAt := h.now()
	release, err := h.acquireStartup(ctx, input.Provider)
	if err != nil {
		h.observeStep(ctx, "session_create", "runtime_started", input.AgentSessionID, input.Provider, startedAt, err)
		return CreateSessionResult{}, cleanup(err, false, false)
	}
	session, err := func() (ProviderRuntimeSession, error) {
		defer release()
		return h.runtime.Start(ctx, RuntimeStartInput{
			WorkspaceID: workspaceID, AgentSessionID: input.AgentSessionID, AgentTargetID: input.AgentTargetID,
			Provider: input.Provider, Cwd: prepared.Cwd, Env: append([]string(nil), prepared.Env...),
			Title: value(input.Title), InitialTitleEstablished: NormalizeTitle(value(input.Title)) != "",
			PermissionModeID: value(input.PermissionModeID), Model: value(input.Model), PlanMode: valueBool(input.PlanMode),
			BrowserUse: input.BrowserUse, ComputerUse: input.ComputerUse,
			ProviderTargetRef: cloneMap(firstMap(prepared.ProviderTargetRef, input.ProviderTargetRef)),
			RuntimeContext:    cloneMap(input.RuntimeContext), ReasoningEffort: value(input.ReasoningEffort),
			Speed: value(input.Speed), ConversationDetailMode: strings.TrimSpace(input.ConversationDetailMode),
			Visible: input.Visible, Provisional: len(normalized) > 0,
		})
	}()
	if err != nil {
		h.observeStep(ctx, "session_create", "runtime_started", input.AgentSessionID, input.Provider, startedAt, err)
		return CreateSessionResult{}, cleanup(err, false, false)
	}
	h.observeStep(ctx, "session_create", "runtime_started", session.ID, session.Provider, startedAt, nil)
	startedAt = h.now()
	canonicalSession, err := h.store.InitializeRuntimeSession(ctx, RuntimeSessionInitialization{
		Session:       session,
		RailPlacement: input.RailPlacement,
	})
	if err != nil {
		h.observeStep(ctx, "session_create", "session_persisted", session.ID, session.Provider, startedAt, err)
		return CreateSessionResult{}, cleanup(err, true, false)
	}
	if strings.TrimSpace(canonicalSession.ID) != strings.TrimSpace(session.ID) || strings.TrimSpace(canonicalSession.WorkspaceID) != workspaceID || strings.TrimSpace(canonicalSession.RailSectionKey) == "" {
		identityErr := fmt.Errorf("initialize workspace agent session: persisted session identity mismatch")
		h.observeStep(ctx, "session_create", "session_persisted", session.ID, session.Provider, startedAt, identityErr)
		return CreateSessionResult{}, cleanup(identityErr, true, true)
	}
	if !railPlacementMatchesSession(input.RailPlacement, canonicalSession) {
		placementErr := ErrRailPlacementConflict
		h.observeStep(ctx, "session_create", "session_persisted", session.ID, session.Provider, startedAt, placementErr)
		return CreateSessionResult{}, cleanup(placementErr, true, true)
	}
	h.observeStep(ctx, "session_create", "session_persisted", session.ID, session.Provider, startedAt, nil)
	if len(normalized) == 0 && !isTypedGoal {
		return CreateSessionResult{Session: session, Canonical: canonicalSession}, nil
	}
	if isTypedGoal {
		goalResult, goalErr := h.goalControl(ctx, GoalControlInput{
			WorkspaceID: workspaceID, AgentSessionID: session.ID,
			Action: typedGoal.Action, Objective: typedGoal.Objective,
			SubmissionMetadata: goalMetadata,
		})
		if goalErr != nil {
			// A typed goal starts from a non-provisional, already published
			// session. Preserve that canonical session on command failure just as
			// the legacy Service did; rolling it back would leave subscribers with
			// an unpaired session-created event.
			return CreateSessionResult{}, cleanup(goalErr, true, false)
		}
		if refreshed, ok := h.runtime.Session(workspaceID, session.ID); ok {
			session = refreshed
		}
		return CreateSessionResult{
			Session: session, Canonical: goalResult.Canonical,
			Kind: "goalControl", GoalControl: &goalResult,
		}, nil
	}
	capabilityApplied := false
	if hasTurnCapability {
		startedAt = h.now()
		if err := h.runtime.ValidatePromptContent(ctx, RuntimeExecInput{
			WorkspaceID: workspaceID, AgentSessionID: session.ID, Content: normalized,
		}); err != nil {
			h.observeStep(ctx, "session_create", "prompt_base_validated", session.ID, session.Provider, startedAt, err)
			return CreateSessionResult{}, cleanup(err, true, true)
		}
		h.observeStep(ctx, "session_create", "prompt_base_validated", session.ID, session.Provider, startedAt, nil)
		startedAt = h.now()
		capabilityResult, ensureErr := h.turnCapabilities.EnsureTurnCapability(ctx, RuntimeTurnCapabilityInput{
			WorkspaceID: workspaceID, AgentSessionID: session.ID,
			TurnID: strings.TrimSpace(input.TurnID), ClientSubmitID: strings.TrimSpace(input.ClientSubmitID),
			Invocation: *invocation, Plan: capabilityPlan,
		})
		if ensureErr != nil {
			h.observeStep(ctx, "session_create", "turn_capability_ensured", session.ID, session.Provider, startedAt, ensureErr)
			claimPending = false
			return CreateSessionResult{}, turnCapabilityDeliveryUnknown(ensureErr)
		}
		switch capabilityResult.Disposition {
		case RuntimeTurnCapabilityRejected:
			h.observeStep(ctx, "session_create", "turn_capability_ensured", session.ID, session.Provider, startedAt, ErrTurnCapabilityRejected)
			if capabilityResult.Outcome == nil || !validTurnCapabilityOutcome(*capabilityResult.Outcome) {
				return CreateSessionResult{}, cleanup(ErrTurnCapabilityRejected, true, true)
			}
			// The runtime and canonical Session now exist. A pre-Exec capability
			// rejection rejects only the initial Turn: rolling the Session back
			// would leave an already-observable Session identity dangling and make
			// a later retry or mode change fail as "session not found".
			return CreateSessionResult{Session: session, Canonical: canonicalSession, TurnID: strings.TrimSpace(input.TurnID)}, turnCapabilityOutcomeError(ErrTurnCapabilityRejected, capabilityResult.Outcome)
		case RuntimeTurnCapabilityUnknown:
			if capabilityResult.Retryable {
				h.observeStep(ctx, "session_create", "turn_capability_ensured", session.ID, session.Provider, startedAt, ErrTurnCapabilityUnavailable)
				if capabilityResult.Outcome == nil || !validTurnCapabilityOutcome(*capabilityResult.Outcome) {
					return CreateSessionResult{}, cleanup(ErrTurnCapabilityUnavailable, true, true)
				}
				// This is also pre-Exec and independently retryable. Keep the
				// initialized Session for the same reason as a rejection above; the
				// deferred claim cleanup releases only the unsubmitted initial Turn.
				return CreateSessionResult{Session: session, Canonical: canonicalSession, TurnID: strings.TrimSpace(input.TurnID)}, turnCapabilityOutcomeError(ErrTurnCapabilityUnavailable, capabilityResult.Outcome)
			}
			h.observeStep(ctx, "session_create", "turn_capability_ensured", session.ID, session.Provider, startedAt, ErrSubmitDeliveryUnknown)
			claimPending = false
			return CreateSessionResult{}, ErrSubmitDeliveryUnknown
		case RuntimeTurnCapabilityApplied, RuntimeTurnCapabilityAlreadyBound:
			capabilityApplied = capabilityResult.Disposition == RuntimeTurnCapabilityApplied
			merged, mergedPromptText, mergeErr := mergeTurnCapabilityPromptContent(normalized, capabilityResult.PromptAugmentation)
			if mergeErr != nil {
				h.observeStep(ctx, "session_create", "turn_capability_ensured", session.ID, session.Provider, startedAt, mergeErr)
				if capabilityApplied {
					claimPending = false
					return CreateSessionResult{}, turnCapabilityDeliveryUnknown(mergeErr)
				}
				return CreateSessionResult{}, cleanup(mergeErr, true, true)
			}
			normalized, promptText = merged, mergedPromptText
			h.observeStep(ctx, "session_create", "turn_capability_ensured", session.ID, session.Provider, startedAt, nil)
		default:
			err := ErrSubmitDeliveryUnknown
			h.observeStep(ctx, "session_create", "turn_capability_ensured", session.ID, session.Provider, startedAt, err)
			claimPending = false
			return CreateSessionResult{}, err
		}
	}
	startedAt = h.now()
	if err := h.runtime.ValidatePromptContent(ctx, RuntimeExecInput{WorkspaceID: workspaceID, AgentSessionID: session.ID, Content: normalized}); err != nil {
		h.observeStep(ctx, "session_create", "prompt_validated", session.ID, session.Provider, startedAt, err)
		if capabilityApplied {
			claimPending = false
			return CreateSessionResult{}, turnCapabilityDeliveryUnknown(err)
		}
		return CreateSessionResult{}, cleanup(err, true, true)
	}
	h.observeStep(ctx, "session_create", "prompt_validated", session.ID, session.Provider, startedAt, nil)
	startedAt = h.now()
	content, preparedDisplay, err := h.prepareContent(workspaceID, session.ID, normalized)
	if err != nil {
		h.observeStep(ctx, "session_create", "prompt_prepared", session.ID, session.Provider, startedAt, err)
		if capabilityApplied {
			claimPending = false
			return CreateSessionResult{}, turnCapabilityDeliveryUnknown(err)
		}
		return CreateSessionResult{}, cleanup(err, true, true)
	}
	h.observeStep(ctx, "session_create", "prompt_prepared", session.ID, session.Provider, startedAt, nil)
	displayPrompt := strings.TrimSpace(input.InitialDisplayPrompt)
	initialTitle := ""
	if !session.InitialTitleEstablished {
		initialTitle = DeriveInitialTitle(session.Title, firstNonEmpty(displayPrompt, promptText, preparedDisplay))
	}
	startedAt = h.now()
	turnID := strings.TrimSpace(input.TurnID)
	if turnID == "" {
		turnID = uuid.NewString()
	}
	execResult, err := h.runtime.Exec(ctx, RuntimeExecInput{
		WorkspaceID: workspaceID, AgentSessionID: session.ID, TurnID: turnID,
		ClientSubmitID: claim.ClientSubmitID, CanonicalSubmitOccurredAtUnixMS: claim.CreatedAtUnixMS,
		CapabilityRefs: append([]CapabilityReference(nil), input.CapabilityRefs...), Content: content,
		DisplayPrompt: displayPrompt, InitialTitle: initialTitle, InitialTitleBase: session.Title,
		Metadata: cloneMap(metadata), TuttiModeSnapshot: input.TuttiModeSnapshot,
	})
	if err != nil {
		h.observeStep(ctx, "session_create", "runtime_exec", session.ID, session.Provider, startedAt, err)
		if hasTurnCapability {
			claimPending = false
			return CreateSessionResult{}, turnCapabilityDeliveryUnknown(err)
		}
		return CreateSessionResult{}, cleanup(err, true, true)
	}
	turnID = strings.TrimSpace(execResult.TurnID)
	if turnID == "" {
		h.observeStep(ctx, "session_create", "runtime_exec", session.ID, session.Provider, startedAt, ErrSubmitDeliveryUnknown)
		if hasTurnCapability {
			claimPending = false
			return CreateSessionResult{}, ErrSubmitDeliveryUnknown
		}
		return CreateSessionResult{}, cleanup(ErrSubmitDeliveryUnknown, true, true)
	}
	if expectedTurnID := strings.TrimSpace(input.TurnID); expectedTurnID != "" && turnID != expectedTurnID {
		claimPending = false
		return CreateSessionResult{}, ErrSubmitDeliveryUnknown
	}
	if reporter, ok := h.runtime.(RuntimeSubmitProvenanceReporter); ok {
		if err := reporter.DurablyReportSubmitProvenance(ctx, RuntimeSubmitProvenanceInput{
			WorkspaceID: workspaceID, AgentSessionID: session.ID, TurnID: turnID,
			ClientSubmitID: claim.ClientSubmitID, CanonicalSubmitOccurredAtUnixMS: claim.CreatedAtUnixMS,
			Content: content, DisplayPrompt: displayPrompt,
		}); err != nil {
			// Provider acceptance is already possible. Keep the runtime, canonical
			// session, and prepared claim intact so a retry cannot dispatch twice.
			claimPending = false
			return CreateSessionResult{}, errors.Join(ErrSubmitDeliveryUnknown, err)
		}
	}
	if claim.ClientSubmitID != "" {
		claimPending = false
		if err := h.acceptSubmitClaim(ref, claim.ClientSubmitID, turnID); err != nil {
			return CreateSessionResult{}, errors.Join(ErrSubmitDeliveryUnknown, err)
		}
	}
	if refreshed, ok := h.runtime.Session(workspaceID, session.ID); ok {
		session = refreshed
	}
	if refreshed, ok, readErr := h.store.GetSession(ctx, workspaceID, session.ID); readErr == nil && ok {
		canonicalSession = refreshed
	}
	h.observeStep(ctx, "session_create", "runtime_exec", session.ID, session.Provider, startedAt, nil)
	return CreateSessionResult{Session: session, Canonical: canonicalSession, TurnID: turnID}, nil
}

func (h *Host) EnsureRuntimeSession(ctx context.Context, ref SessionRef) (ProviderRuntimeSession, error) {
	ref.WorkspaceID, ref.AgentSessionID = strings.TrimSpace(ref.WorkspaceID), strings.TrimSpace(ref.AgentSessionID)
	if h == nil || h.runtime == nil || h.store == nil || ref.WorkspaceID == "" || ref.AgentSessionID == "" {
		return ProviderRuntimeSession{}, ErrSessionNotFound
	}
	release, err := h.acquireSession(ctx, ref)
	if err != nil {
		return ProviderRuntimeSession{}, err
	}
	defer release()
	return h.ensureRuntimeSessionLocked(ctx, ref)
}

func (h *Host) ensureRuntimeSessionLocked(ctx context.Context, ref SessionRef) (ProviderRuntimeSession, error) {
	deleted, err := h.store.SessionDeleted(ctx, ref.WorkspaceID, ref.AgentSessionID)
	if err != nil {
		return ProviderRuntimeSession{}, err
	}
	if deleted {
		return ProviderRuntimeSession{}, ErrSessionNotFound
	}
	canonicalSession, found, err := h.store.GetSession(ctx, ref.WorkspaceID, ref.AgentSessionID)
	if err != nil {
		return ProviderRuntimeSession{}, err
	}
	if found && ResolveResumePolicy(canonicalSession).Mode == ResumeModeReject {
		return ProviderRuntimeSession{}, ErrSessionNotFound
	}
	policy := ResolveResumePolicy(canonicalSession)
	evidence := storesqlite.ProviderSessionResumeEvidence{}
	if found && policy.Mode != ResumeModeRecreate {
		evidence, err = h.store.GetProviderSessionResumeEvidence(ctx, ref.WorkspaceID, ref.AgentSessionID)
		if err != nil {
			return ProviderRuntimeSession{}, err
		}
	}
	if live, ok := h.runtime.Session(ref.WorkspaceID, ref.AgentSessionID); ok {
		if !ExternalImportResumeSupported(live.RuntimeContext) {
			return ProviderRuntimeSession{}, ErrSessionNotFound
		}
		if policy.Mode != ResumeModeRecreate &&
			!runtimeSessionHasActiveTurn(live) &&
			strings.TrimSpace(canonicalSession.ActiveTurnID) == "" &&
			evidence.HasSettledTurn && !evidence.Established {
			return ProviderRuntimeSession{}, ErrProviderSessionNotEstablished
		}
		live.Resumable = live.Resumable || evidence.Established
		// Controller may retain the Session record after releasing an idle
		// provider connection. Controller's registry handles connection
		// replacement; clearing this Host marker additionally refreshes its
		// retained set from the durable store before Ensure returns.
		if !h.runtimeSessionLive(ref.WorkspaceID, ref.AgentSessionID) {
			h.goalFencesRestored.Delete(ref.WorkspaceID + "\x00" + ref.AgentSessionID)
		}
		if err := h.restoreGoalGenerationFencesOnce(ctx, ref); err != nil {
			return ProviderRuntimeSession{}, err
		}
		return live, nil
	}
	if !found || strings.TrimSpace(canonicalSession.Provider) == "" {
		return ProviderRuntimeSession{}, ErrSessionNotFound
	}
	if policy.Mode != ResumeModeRecreate &&
		!evidence.Established &&
		(!evidence.HasTurns || evidence.HasSettledTurn) {
		return ProviderRuntimeSession{}, ErrProviderSessionNotEstablished
	}
	prepared := PreparedRuntime{Cwd: strings.TrimSpace(canonicalSession.Cwd)}
	settings := composerSettingsFromMap(canonicalSession.Settings)
	if h.preparation != nil {
		prepared, err = h.preparation.Prepare(ctx, resumePreparationInput(canonicalSession, settings))
		if err != nil {
			return ProviderRuntimeSession{}, err
		}
	}
	if prepared.Settings != nil {
		settings = *prepared.Settings
	}
	release, err := h.acquireStartup(ctx, canonicalSession.Provider)
	if err != nil {
		return ProviderRuntimeSession{}, err
	}
	defer release()
	result, err := h.runtime.Resume(ctx, RuntimeResumeInput{
		WorkspaceID: ref.WorkspaceID, AgentSessionID: ref.AgentSessionID,
		AgentTargetID: strings.TrimSpace(canonicalSession.AgentTargetID), Provider: strings.TrimSpace(canonicalSession.Provider),
		ProviderSessionID: strings.TrimSpace(canonicalSession.ProviderSessionID), Resumable: evidence.Established, Cwd: prepared.Cwd,
		Env: append([]string(nil), prepared.Env...), Title: strings.TrimSpace(canonicalSession.Title),
		Status: persistedRuntimeStatus(canonicalSession.ActiveTurnID), Settings: settings,
		CreatedAtUnixMS: canonicalSession.CreatedAtUnixMS, UpdatedAtUnixMS: canonicalSession.UpdatedAtUnixMS,
		Visible: boolPointer(canonicalSession.Metadata.Visible), RuntimeContext: cloneMap(firstMap(prepared.RuntimeContext, canonicalSession.InternalRuntimeContext)),
		ProviderTargetRef: cloneMap(prepared.ProviderTargetRef), Metadata: canonicalSession.Metadata,
		InternalRuntimeContext: cloneMap(canonicalSession.InternalRuntimeContext), RecreateIfMissing: policy.Mode == ResumeModeRecreate,
	})
	if err != nil {
		return ProviderRuntimeSession{}, err
	}
	if err := h.restoreGoalGenerationFences(ctx, ref); err != nil {
		return ProviderRuntimeSession{}, err
	}
	h.goalFencesRestored.Store(ref.WorkspaceID+"\x00"+ref.AgentSessionID, struct{}{})
	return result, nil
}

func runtimeSessionHasActiveTurn(session ProviderRuntimeSession) bool {
	return session.TurnLifecycle != nil &&
		session.TurnLifecycle.ActiveTurnID != nil &&
		strings.TrimSpace(*session.TurnLifecycle.ActiveTurnID) != ""
}

func (h *Host) SendInput(ctx context.Context, ref SessionRef, input SendInput) (SendInputResult, error) {
	ref.WorkspaceID, ref.AgentSessionID = strings.TrimSpace(ref.WorkspaceID), strings.TrimSpace(ref.AgentSessionID)
	if h == nil || h.runtime == nil || h.store == nil || ref.WorkspaceID == "" || ref.AgentSessionID == "" {
		return SendInputResult{}, ErrInvalidArgument
	}
	normalized, promptText, err := normalizePromptContent(input.Content)
	if err != nil {
		return SendInputResult{}, err
	}
	metadata := submissionMetadata(input.Metadata, input.ClientSubmitID)
	// A capability send may omit TurnID at the transport boundary. Host owns
	// restoring the previously claimed canonical ID (or allocating one) before
	// validating the capability's stable submit identity. Keep the nil path's
	// existing ordering untouched.
	if input.TurnCapabilityInvocation != nil && !input.Guidance {
		input.TurnID, err = h.canonicalTurnIDForSubmitClaim(ctx, ref, metadata, input.TurnID)
		if err != nil {
			return SendInputResult{}, err
		}
		if strings.TrimSpace(input.TurnID) == "" {
			input.TurnID = uuid.NewString()
		}
	}
	invocation, hasTurnCapability, err := h.validatedTurnCapabilityInvocation(
		input.TurnCapabilityInvocation,
		input.Guidance,
		input.TurnID,
		input.ClientSubmitID,
	)
	if err != nil {
		return SendInputResult{}, err
	}
	if typedGoal, ok := ParseTypedGoalControl(normalized, input.Guidance); ok {
		if hasTurnCapability {
			return SendInputResult{}, ErrInvalidArgument
		}
		goalResult, goalErr := h.goalControl(ctx, GoalControlInput{
			WorkspaceID: ref.WorkspaceID, AgentSessionID: ref.AgentSessionID,
			Action: typedGoal.Action, Objective: typedGoal.Objective,
			SubmissionMetadata: metadata,
		})
		if goalErr != nil {
			return SendInputResult{}, goalErr
		}
		session, _ := h.runtime.Session(ref.WorkspaceID, ref.AgentSessionID)
		return SendInputResult{
			Session: session, Canonical: goalResult.Canonical,
			Kind: "goalControl", GoalControl: &goalResult,
		}, nil
	}
	// Every non-goal prompt, including guidance, must have a Host-owned
	// canonical TurnID before claim preparation. Guidance has no capability
	// admission, but it still uses the same durable exactly-once claim as an
	// ordinary input and must not rely on a service-created ID.
	if input.TurnCapabilityInvocation == nil {
		input.TurnID, err = h.canonicalTurnIDForSubmitClaim(ctx, ref, metadata, input.TurnID)
		if err != nil {
			return SendInputResult{}, err
		}
		if strings.TrimSpace(input.TurnID) == "" {
			input.TurnID = uuid.NewString()
		}
	}
	claim, claimPending, err := h.prepareSubmitClaim(ctx, ref, metadata, input.TurnID)
	if err != nil {
		if errors.Is(err, storesqlite.ErrSubmitClaimTurnConflict) {
			return SendInputResult{}, errors.Join(ErrSubmitDeliveryUnknown, err)
		}
		return SendInputResult{}, err
	}
	if claim.ClientSubmitID != "" && !claimPending {
		if claim.Status != "accepted" {
			return SendInputResult{}, ErrSubmitDeliveryUnknown
		}
		return h.acceptedSubmitResult(ctx, ref, claim)
	}
	defer func() {
		if claimPending {
			h.abandonSubmitClaim(ref, claim.ClientSubmitID)
		}
	}()
	release, err := h.acquireSession(ctx, ref)
	if err != nil {
		return SendInputResult{}, err
	}
	defer release()
	capabilityPlan := RuntimeTurnCapabilityPlan{}
	if hasTurnCapability {
		canonical, found, readErr := h.store.GetSession(ctx, ref.WorkspaceID, ref.AgentSessionID)
		if readErr != nil || !found {
			if readErr != nil {
				return SendInputResult{}, readErr
			}
			return SendInputResult{}, ErrSessionNotFound
		}
		if claim.TurnCapabilityPlanJSON != "" {
			capabilityPlan, err = decodeTurnCapabilityPlan(claim.TurnCapabilityPlanJSON)
			if err != nil {
				return SendInputResult{}, err
			}
		} else {
			startedAt := h.now()
			plan, err := h.admitTurnCapability(ctx, RuntimeTurnCapabilityAdmissionInput{
				WorkspaceID: ref.WorkspaceID, AgentSessionID: ref.AgentSessionID,
				TurnID: input.TurnID, ClientSubmitID: input.ClientSubmitID,
				AgentTargetID: canonical.AgentTargetID, Provider: canonical.Provider,
				RuntimeContext: cloneMap(canonical.InternalRuntimeContext), Invocation: *invocation,
			})
			h.observeStep(ctx, "message_send", "turn_capability_admitted", ref.AgentSessionID, canonical.Provider, startedAt, err)
			if err != nil {
				if errors.Is(err, ErrSubmitDeliveryUnknown) {
					claimPending = false
				}
				return SendInputResult{}, err
			}
			encodedPlan, encodeErr := encodeTurnCapabilityPlan(plan)
			if encodeErr != nil {
				return SendInputResult{}, encodeErr
			}
			claim, _, err = h.store.SetSubmitClaimCapabilityPlan(ctx, ref.WorkspaceID, ref.AgentSessionID, claim.ClientSubmitID, encodedPlan, h.now().UnixMilli())
			if err != nil {
				return SendInputResult{}, err
			}
			capabilityPlan = plan
		}
	}
	startedAt := h.now()
	session, err := h.ensureRuntimeSessionLocked(ctx, ref)
	if err != nil {
		h.observeStep(ctx, "message_send", "runtime_session_ready", ref.AgentSessionID, "", startedAt, err)
		return SendInputResult{}, err
	}
	h.observeStep(ctx, "message_send", "runtime_session_ready", ref.AgentSessionID, session.Provider, startedAt, nil)
	capabilityApplied := false
	if hasTurnCapability {
		startedAt = h.now()
		if err := h.runtime.ValidatePromptContent(ctx, RuntimeExecInput{
			WorkspaceID: ref.WorkspaceID, AgentSessionID: ref.AgentSessionID, Content: normalized,
		}); err != nil {
			h.observeStep(ctx, "message_send", "prompt_base_validated", ref.AgentSessionID, session.Provider, startedAt, err)
			return SendInputResult{}, err
		}
		h.observeStep(ctx, "message_send", "prompt_base_validated", ref.AgentSessionID, session.Provider, startedAt, nil)
		startedAt = h.now()
		capabilityResult, ensureErr := h.turnCapabilities.EnsureTurnCapability(ctx, RuntimeTurnCapabilityInput{
			WorkspaceID: ref.WorkspaceID, AgentSessionID: ref.AgentSessionID,
			TurnID: strings.TrimSpace(input.TurnID), ClientSubmitID: strings.TrimSpace(input.ClientSubmitID),
			Invocation: *invocation, Plan: capabilityPlan,
		})
		if ensureErr != nil {
			h.observeStep(ctx, "message_send", "turn_capability_ensured", ref.AgentSessionID, session.Provider, startedAt, ensureErr)
			claimPending = false
			return SendInputResult{}, turnCapabilityDeliveryUnknown(ensureErr)
		}
		switch capabilityResult.Disposition {
		case RuntimeTurnCapabilityRejected:
			h.observeStep(ctx, "message_send", "turn_capability_ensured", ref.AgentSessionID, session.Provider, startedAt, ErrTurnCapabilityRejected)
			return SendInputResult{}, turnCapabilityOutcomeError(ErrTurnCapabilityRejected, capabilityResult.Outcome)
		case RuntimeTurnCapabilityUnknown:
			if capabilityResult.Retryable {
				h.observeStep(ctx, "message_send", "turn_capability_ensured", ref.AgentSessionID, session.Provider, startedAt, ErrTurnCapabilityUnavailable)
				return SendInputResult{}, turnCapabilityOutcomeError(ErrTurnCapabilityUnavailable, capabilityResult.Outcome)
			}
			h.observeStep(ctx, "message_send", "turn_capability_ensured", ref.AgentSessionID, session.Provider, startedAt, ErrSubmitDeliveryUnknown)
			claimPending = false
			return SendInputResult{}, ErrSubmitDeliveryUnknown
		case RuntimeTurnCapabilityApplied, RuntimeTurnCapabilityAlreadyBound:
			capabilityApplied = capabilityResult.Disposition == RuntimeTurnCapabilityApplied
			merged, mergedPromptText, mergeErr := mergeTurnCapabilityPromptContent(normalized, capabilityResult.PromptAugmentation)
			if mergeErr != nil {
				h.observeStep(ctx, "message_send", "turn_capability_ensured", ref.AgentSessionID, session.Provider, startedAt, mergeErr)
				if capabilityApplied {
					claimPending = false
					return SendInputResult{}, turnCapabilityDeliveryUnknown(mergeErr)
				}
				return SendInputResult{}, mergeErr
			}
			normalized, promptText = merged, mergedPromptText
			h.observeStep(ctx, "message_send", "turn_capability_ensured", ref.AgentSessionID, session.Provider, startedAt, nil)
		default:
			err := ErrSubmitDeliveryUnknown
			h.observeStep(ctx, "message_send", "turn_capability_ensured", ref.AgentSessionID, session.Provider, startedAt, err)
			claimPending = false
			return SendInputResult{}, err
		}
	}
	startedAt = h.now()
	if err := h.runtime.ValidatePromptContent(ctx, RuntimeExecInput{WorkspaceID: ref.WorkspaceID, AgentSessionID: ref.AgentSessionID, Content: normalized}); err != nil {
		h.observeStep(ctx, "message_send", "prompt_validated", ref.AgentSessionID, session.Provider, startedAt, err)
		if capabilityApplied {
			claimPending = false
			return SendInputResult{}, turnCapabilityDeliveryUnknown(err)
		}
		return SendInputResult{}, err
	}
	h.observeStep(ctx, "message_send", "prompt_validated", ref.AgentSessionID, session.Provider, startedAt, nil)
	startedAt = h.now()
	content, preparedDisplay, err := h.prepareContent(ref.WorkspaceID, ref.AgentSessionID, normalized)
	if err != nil {
		h.observeStep(ctx, "message_send", "prompt_prepared", ref.AgentSessionID, session.Provider, startedAt, err)
		if capabilityApplied {
			claimPending = false
			return SendInputResult{}, turnCapabilityDeliveryUnknown(err)
		}
		return SendInputResult{}, err
	}
	h.observeStep(ctx, "message_send", "prompt_prepared", ref.AgentSessionID, session.Provider, startedAt, nil)
	displayPrompt, initialTitle := strings.TrimSpace(input.DisplayPrompt), ""
	if !input.Guidance && !session.InitialTitleEstablished {
		initialTitle = DeriveInitialTitle(session.Title, firstNonEmpty(displayPrompt, promptText, preparedDisplay))
	}
	startedAt = h.now()
	releaseStartup, err := h.acquireStartup(ctx, session.Provider)
	if err != nil {
		h.observeStep(ctx, "message_send", "runtime_exec", ref.AgentSessionID, session.Provider, startedAt, err)
		if hasTurnCapability {
			claimPending = false
			return SendInputResult{}, turnCapabilityDeliveryUnknown(err)
		}
		return SendInputResult{}, err
	}
	execResult, err := func() (RuntimeExecResult, error) {
		defer releaseStartup()
		turnID := strings.TrimSpace(input.TurnID)
		if turnID == "" && !input.Guidance {
			turnID = uuid.NewString()
		}
		return h.runtime.Exec(ctx, RuntimeExecInput{
			WorkspaceID: ref.WorkspaceID, AgentSessionID: ref.AgentSessionID,
			TurnID: turnID, ClientSubmitID: claim.ClientSubmitID,
			CanonicalSubmitOccurredAtUnixMS: claim.CreatedAtUnixMS,
			CapabilityRefs:                  append([]CapabilityReference(nil), input.CapabilityRefs...), Content: content,
			DisplayPrompt: displayPrompt, InitialTitle: initialTitle, InitialTitleBase: session.Title,
			Guidance: input.Guidance, Metadata: cloneMap(metadata), TuttiModeSnapshot: input.TuttiModeSnapshot,
		})
	}()
	if err != nil {
		h.observeStep(ctx, "message_send", "runtime_exec", ref.AgentSessionID, session.Provider, startedAt, err)
		if hasTurnCapability {
			claimPending = false
			return SendInputResult{}, turnCapabilityDeliveryUnknown(err)
		}
		if input.Guidance {
			// Guidance targets an already-live turn and transport failure cannot
			// prove rejection. Preserve the claim as a replay fence.
			claimPending = false
			return SendInputResult{}, errors.Join(ErrSubmitDeliveryUnknown, err)
		}
		return SendInputResult{}, err
	}
	turnID := strings.TrimSpace(execResult.TurnID)
	if turnID == "" {
		h.observeStep(ctx, "message_send", "runtime_exec", ref.AgentSessionID, session.Provider, startedAt, ErrSubmitDeliveryUnknown)
		if hasTurnCapability {
			claimPending = false
		}
		return SendInputResult{}, ErrSubmitDeliveryUnknown
	}
	if expectedTurnID := strings.TrimSpace(input.TurnID); !input.Guidance && expectedTurnID != "" && turnID != expectedTurnID {
		claimPending = false
		return SendInputResult{}, ErrSubmitDeliveryUnknown
	}
	if reporter, ok := h.runtime.(RuntimeSubmitProvenanceReporter); ok {
		if err := reporter.DurablyReportSubmitProvenance(ctx, RuntimeSubmitProvenanceInput{
			WorkspaceID: ref.WorkspaceID, AgentSessionID: ref.AgentSessionID, TurnID: turnID,
			ClientSubmitID: claim.ClientSubmitID, CanonicalSubmitOccurredAtUnixMS: claim.CreatedAtUnixMS,
			Content: content, DisplayPrompt: displayPrompt, Guidance: input.Guidance,
		}); err != nil {
			claimPending = false
			return SendInputResult{}, errors.Join(ErrSubmitDeliveryUnknown, err)
		}
	}
	if claim.ClientSubmitID != "" {
		claimPending = false
		if err := h.acceptSubmitClaim(ref, claim.ClientSubmitID, turnID); err != nil {
			return SendInputResult{}, errors.Join(ErrSubmitDeliveryUnknown, err)
		}
	}
	h.observeStep(ctx, "message_send", "runtime_exec", ref.AgentSessionID, session.Provider, startedAt, nil)
	canonicalSession, ok, err := h.store.GetSession(ctx, ref.WorkspaceID, ref.AgentSessionID)
	if err != nil {
		return SendInputResult{}, err
	}
	_ = ok
	turn, ok, err := h.store.GetTurn(ctx, ref.WorkspaceID, ref.AgentSessionID, turnID)
	if err != nil {
		return SendInputResult{}, err
	}
	var turnPtr *storesqlite.Turn
	if ok {
		turnPtr = &turn
	}
	return SendInputResult{
		Session: session, Canonical: canonicalSession, Turn: turnPtr, TurnID: turnID,
		TurnLifecycle: execResult.TurnLifecycle, SubmitAvailability: execResult.SubmitAvailability,
	}, nil
}
