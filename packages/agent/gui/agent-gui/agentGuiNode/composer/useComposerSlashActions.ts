import { useCallback, useMemo, useRef, useState, type FormEvent } from "react";
import type { AgentSessionCommand } from "../../../shared/agentSessionTypes";
import type { AgentActivityTurnCapabilitySemantic } from "@tutti-os/agent-activity-core";
import type {
  AgentComposerDraft,
  AgentGUIProviderSkillOption
} from "../model/agentGuiNodeTypes";
import { agentComposerFileMentionReferences } from "../agentRichText/agentMentionMarkdown";
import { useOptionalAgentActivityRuntime } from "../../../agentActivityRuntime";
import type {
  AgentSlashCommandCapability,
  SlashCommandSelectionEffect
} from "../model/agentSlashCommandProviderPolicy";
import {
  resolveSlashCommandSelectionEffect,
  resolveSlashCommandSubmitEffect
} from "../model/agentSlashCommandProviderPolicy";
import {
  draftForProviderSkillTrigger,
  getAgentComposerTriggerQueryMatch
} from "../model/agentComposerTriggerQueries";
import { skillTriggerForPrefix } from "../model/agentSkillOptions";
import { moveSlashCommandHighlight } from "../model/agentSlashCommands";
import {
  agentComposerDraftHasContent,
  agentComposerDraftToPromptContent,
  buildAgentComposerDraft,
  emptyAgentComposerDraft,
  projectAgentComposerDraftSubmission,
  textPromptContent,
  updateAgentComposerDraft
} from "../model/agentComposerDraft";
import { resolvePermissionModeControlsDisabled } from "../model/composerModeSelection";
import { GOAL_MODE_SLASH_COMMAND } from "./AgentComposerChrome";
import { reportAgentComposerDiagnostic } from "./agentComposerDiagnostics";
import {
  confirmedTurnCapabilitySemantic,
  hasProviderCapabilitySlash,
  resolveTurnCapabilityInvocationForSlash,
  resolveTurnCapabilitySlashSubmission,
  type TurnCapabilityConsentConfirmation
} from "./turnCapabilitySlashSubmission";
import type { UseComposerSlashActionsInput } from "./useComposerSlashActions.types";
import { capabilityUnavailableMessage } from "./capabilityUnavailablePresentation";
import { useCapabilityUnavailableNotice } from "./useCapabilityUnavailableNotice";
import { useProviderCapabilitySelection } from "./useProviderCapabilitySelection";
export type TriggerMatch = ReturnType<typeof getAgentComposerTriggerQueryMatch>;
function capabilityConsentSnapshot(draft: AgentComposerDraft): string {
  return JSON.stringify(draft);
}
function useStableEventCallback<Args extends unknown[], Result>(
  callback: (...args: Args) => Result
): (...args: Args) => Result {
  const callbackRef = useRef(callback);
  callbackRef.current = callback;
  return useCallback((...args: Args) => callbackRef.current(...args), []);
}
export function useComposerSlashActions(input: UseComposerSlashActionsInput) {
  const agentActivityRuntime = useOptionalAgentActivityRuntime();
  const {
    workspaceId,
    provider,
    disabled,
    submitDisabled,
    canQueueWhileBusy,
    isSendingTurn,
    isSubmittingPrompt,
    showStopButton,
    promptImagesSupported,
    availableSkills = [],
    capabilityPresentations = [],
    turnCapabilityStates = [],
    tuttiModeActive,
    agentSessionId,
    composerSettings,
    tuttiModeSupported,
    capabilityControlsReadOnly = false,
    onDraftContentChange,
    onSettingsChange,
    onSubmit,
    onSubmitEmpty,
    onSubmitGuidance,
    onCapabilitySettingsRequest,
    onTuttiModeActivate,
    onSlashStatusOpen,
    onSlashStatusClose,
    onPromptImagesUnsupported,
    labels,
    onRequestGitBranches,
    draftContent,
    selectedProjectPath,
    slashStatusAgentSessionId,
    isSlashStatusPanelOpen,
    slashCommandPolicy,
    skillQueryMatch,
    promptBeforeSelection,
    resolvedSlashCommands,
    slashPaletteEntries,
    activeHighlight,
    showSlashPalette,
    showCommandMenuPanel,
    isSelectedProjectMissing,
    editorHandleRef,
    draftPromptRef,
    draftImagesRef,
    draftFilesRef,
    draftLargeTextsRef,
    setPaletteDraftPrompt,
    setIsPaletteOpen,
    setIsReviewPickerOpen,
    setIsSlashStatusPanelOpen,
    setHighlightedIndex
  } = input;
  const draftConsentScopeRef = useRef<string>(
    `draft:${workspaceId}:${Math.random().toString(36).slice(2)}`
  );
  const {
    capabilityUnavailableNotice,
    clearCapabilityUnavailableNotice,
    showCapabilityUnavailableNotice
  } = useCapabilityUnavailableNotice({
    capabilities: capabilityPresentations,
    draftSnapshot: capabilityConsentSnapshot(draftContent),
    modeActive: tuttiModeActive,
    provider,
    scope: agentSessionId || draftConsentScopeRef.current
  });
  const pendingCapabilityConsentRef =
    useRef<TurnCapabilityConsentConfirmation | null>(null);
  const [capabilityConsentDialog, setCapabilityConsentDialog] = useState<{
    semantic: AgentActivityTurnCapabilitySemantic;
    label: string;
    scope: string;
    snapshot: string;
    modeActive: boolean;
  } | null>(null);
  const clearSlashCommandDraft = useCallback((): void => {
    clearCapabilityUnavailableNotice();
    draftPromptRef.current = "";
    setPaletteDraftPrompt("");
    setIsPaletteOpen(false);
    onDraftContentChange(emptyAgentComposerDraft());
  }, [clearCapabilityUnavailableNotice, onDraftContentChange]);

  const closeSlashStatusPanel = useCallback((): void => {
    setIsSlashStatusPanelOpen(false);
    onSlashStatusClose?.();
  }, [onSlashStatusClose, setIsSlashStatusPanelOpen]);

  const settingsControlsDisabled =
    isSendingTurn || isSubmittingPrompt || showStopButton;
  const permissionModeControlsDisabled = resolvePermissionModeControlsDisabled({
    isSendingTurn,
    isSubmittingPrompt,
    showStopButton
  });
  const composerControlsHardDisabled =
    isSelectedProjectMissing ||
    isSubmittingPrompt ||
    (disabled && !isSendingTurn && !showStopButton);

  const closeReviewPicker = useCallback((): void => {
    setIsReviewPickerOpen(false);
  }, []);

  const closeSlashFloatingMenu = useCallback((): void => {
    if (isSlashStatusPanelOpen) {
      onSlashStatusClose?.();
    }
    setIsSlashStatusPanelOpen(false);
    setIsReviewPickerOpen(false);
    setIsPaletteOpen(false);
  }, [
    isSlashStatusPanelOpen,
    onSlashStatusClose,
    setIsPaletteOpen,
    setIsReviewPickerOpen,
    setIsSlashStatusPanelOpen
  ]);

  const submitReviewCommand = useCallback(
    (command: string): void => {
      setIsReviewPickerOpen(false);
      clearSlashCommandDraft();
      onSubmit(textPromptContent(command));
    },
    [clearSlashCommandDraft, onSubmit]
  );

  const reviewBranchLoader = useMemo(() => {
    if (!onRequestGitBranches) {
      return null;
    }
    if (slashStatusAgentSessionId) {
      return () =>
        onRequestGitBranches({ agentSessionId: slashStatusAgentSessionId });
    }
    if (selectedProjectPath) {
      return () =>
        onRequestGitBranches({ workingDirectory: selectedProjectPath });
    }
    return null;
  }, [onRequestGitBranches, selectedProjectPath, slashStatusAgentSessionId]);

  const executeSlashCommandEffect = useCallback(
    (effect: SlashCommandSelectionEffect): void => {
      if (effect.kind === "submitPrompt") {
        clearSlashCommandDraft();
        const submitOptions = effect.requiredSettingsPatch
          ? { requiredSettingsPatch: effect.requiredSettingsPatch }
          : undefined;
        const content = agentComposerDraftToPromptContent({
          draft: buildAgentComposerDraft({ prompt: effect.prompt }),
          skills: availableSkills
        });
        if (effect.displayPrompt) {
          onSubmit(content, effect.displayPrompt, submitOptions);
        } else {
          onSubmit(content, undefined, submitOptions);
        }
        return;
      }
      if (effect.kind === "showStatus") {
        clearSlashCommandDraft();
        setIsReviewPickerOpen(false);
        if (!isSlashStatusPanelOpen) {
          onSlashStatusOpen?.();
        } else {
          onSlashStatusClose?.();
        }
        setIsSlashStatusPanelOpen((current) => !current);
        return;
      }
      if (effect.kind === "showReviewPicker") {
        clearSlashCommandDraft();
        if (isSlashStatusPanelOpen) {
          onSlashStatusClose?.();
          setIsSlashStatusPanelOpen(false);
        }
        setIsReviewPickerOpen(true);
        return;
      }
      if (effect.kind === "activateGoalMode") {
        if (isSlashStatusPanelOpen) {
          onSlashStatusClose?.();
        }
        draftPromptRef.current = GOAL_MODE_SLASH_COMMAND;
        setPaletteDraftPrompt("");
        setIsSlashStatusPanelOpen(false);
        setIsReviewPickerOpen(false);
        setIsPaletteOpen(false);
        onDraftContentChange(
          updateAgentComposerDraft(draftContent, {
            prompt: GOAL_MODE_SLASH_COMMAND
          })
        );
        return;
      }
      if (effect.kind === "enablePlanMode") {
        clearSlashCommandDraft();
        if (!settingsControlsDisabled) {
          onSettingsChange({ planMode: true });
        }
        return;
      }
      if (effect.kind === "activateTuttiMode") {
        clearSlashCommandDraft();
        onTuttiModeActivate?.();
        return;
      }
      if (effect.kind === "enableBrowserUse") {
        const nextDraft = effect.draft;
        draftPromptRef.current = nextDraft;
        setPaletteDraftPrompt(nextDraft);
        onDraftContentChange(
          updateAgentComposerDraft(draftContent, { prompt: nextDraft })
        );
        setIsPaletteOpen(false);
        if (!settingsControlsDisabled) {
          onSettingsChange({ browserUse: true });
        }
        return;
      }
      if (effect.kind === "enableComputerUse") {
        const nextDraft = effect.draft;
        draftPromptRef.current = nextDraft;
        setPaletteDraftPrompt(nextDraft);
        onDraftContentChange(
          updateAgentComposerDraft(draftContent, { prompt: nextDraft })
        );
        setIsPaletteOpen(false);
        if (!settingsControlsDisabled) {
          onSettingsChange({ computerUse: true });
        }
        return;
      }
      if (effect.kind === "toggleSpeed") {
        clearSlashCommandDraft();
        if (composerSettings.supportsSpeed) {
          const currentSpeed =
            composerSettings.selectedSpeedValue ??
            composerSettings.draftSettings.speed ??
            "standard";
          onSettingsChange({
            speed: currentSpeed === "fast" ? "standard" : "fast"
          });
        }
        return;
      }
      const nextDraft = effect.draft;
      draftPromptRef.current = nextDraft;
      setPaletteDraftPrompt(nextDraft);
      onDraftContentChange(
        updateAgentComposerDraft(draftContent, { prompt: nextDraft })
      );
      setIsPaletteOpen(false);
    },
    [
      availableSkills,
      clearSlashCommandDraft,
      composerSettings.draftSettings.planMode,
      composerSettings.draftSettings.speed,
      composerSettings.selectedSpeedValue,
      composerSettings.supportsSpeed,
      draftContent,
      isSlashStatusPanelOpen,
      onDraftContentChange,
      onSlashStatusClose,
      onTuttiModeActivate,
      onSlashStatusOpen,
      onSettingsChange,
      onSubmit,
      settingsControlsDisabled
    ]
  );

  const selectCommand = useCallback(
    (command: AgentSessionCommand): void => {
      const selectionEffect = resolveSlashCommandSelectionEffect({
        provider,
        policy: slashCommandPolicy,
        command,
        currentDraft: draftPromptRef.current
      });
      if (selectionEffect) {
        executeSlashCommandEffect(selectionEffect);
      }
    },
    [executeSlashCommandEffect, provider, slashCommandPolicy]
  );

  const selectCapability = useCallback(
    (capability: AgentSlashCommandCapability): void => {
      if (capabilityControlsReadOnly) {
        return;
      }
      const selectionEffect = resolveSlashCommandSelectionEffect({
        provider,
        policy: slashCommandPolicy,
        command: capability,
        currentDraft: draftPromptRef.current
      });
      if (selectionEffect) {
        executeSlashCommandEffect(selectionEffect);
      }
    },
    [
      capabilityControlsReadOnly,
      executeSlashCommandEffect,
      provider,
      slashCommandPolicy
    ]
  );

  const selectCapabilitySettings = useCallback(
    (capability: AgentSlashCommandCapability): void => {
      if (capabilityControlsReadOnly) {
        return;
      }
      if (capability.capability !== "tutti") {
        onCapabilitySettingsRequest?.(capability.capability);
      }
      setIsPaletteOpen(false);
    },
    [capabilityControlsReadOnly, onCapabilitySettingsRequest]
  );

  const selectSkill = useCallback(
    (skill: AgentGUIProviderSkillOption): void => {
      const trigger = skillTriggerForPrefix(skill, skillQueryMatch?.prefix);
      const replacedDraft =
        trigger && skillQueryMatch && promptBeforeSelection !== ""
          ? editorHandleRef.current?.replaceTextBeforeSelection(
              skillQueryMatch.end - skillQueryMatch.start,
              `${trigger} `
            )
          : null;
      const nextDraft =
        replacedDraft ??
        draftForProviderSkillTrigger({
          skill,
          currentDraft: draftPromptRef.current,
          match: skillQueryMatch
        });
      draftPromptRef.current = nextDraft;
      setPaletteDraftPrompt(nextDraft);
      onDraftContentChange(
        updateAgentComposerDraft(draftContent, { prompt: nextDraft })
      );
      setIsPaletteOpen(false);
    },
    [draftContent, onDraftContentChange, promptBeforeSelection, skillQueryMatch]
  );

  const { selectPluginSettings, selectProviderCapability } =
    useProviderCapabilitySelection({
      capabilityControlsReadOnly,
      clearUnavailableNotice: clearCapabilityUnavailableNotice,
      draftContent,
      draftPromptRef,
      onCapabilitySettingsRequest,
      onDraftContentChange,
      setIsPaletteOpen,
      setPaletteDraftPrompt
    });

  const submitCurrentPrompt = useStableEventCallback(
    (options?: { guidance?: boolean }): void => {
      const canSubmitWhileSending = canQueueWhileBusy && isSendingTurn;
      const currentDraftImages = draftImagesRef.current;
      const currentDraftFiles = draftFilesRef.current;
      const currentDraftLargeTexts = draftLargeTextsRef.current;
      const hasUploadingImages = currentDraftImages.some(
        (image) => image.uploading
      );
      const hasFailedImages = currentDraftImages.some(
        (image) => image.uploadError
      );
      const hasUploadingFiles = currentDraftFiles.some(
        (file) => file.uploading
      );
      const hasFailedFiles = currentDraftFiles.some((file) => file.uploadError);
      const hasUploadingLargeTexts = currentDraftLargeTexts.some(
        (item) => item.uploading
      );
      const hasFailedLargeTexts = currentDraftLargeTexts.some(
        (item) => item.uploadError
      );
      if (
        isSelectedProjectMissing ||
        submitDisabled ||
        hasUploadingImages ||
        hasFailedImages ||
        hasUploadingFiles ||
        hasFailedFiles ||
        hasUploadingLargeTexts ||
        hasFailedLargeTexts ||
        (disabled && !canQueueWhileBusy) ||
        (isSendingTurn && !canSubmitWhileSending)
      ) {
        return;
      }
      const nextPrompt = draftPromptRef.current;
      const nextDraftContent = buildAgentComposerDraft({
        prompt: nextPrompt,
        images: currentDraftImages,
        files: currentDraftFiles,
        largeTexts: currentDraftLargeTexts
      });
      const nextDraftSnapshot = capabilityConsentSnapshot(nextDraftContent);
      if (!agentComposerDraftHasContent(nextDraftContent)) {
        if (options?.guidance !== true) {
          onSubmitEmpty?.();
        }
        return;
      }
      if (currentDraftImages.length > 0 && !promptImagesSupported) {
        onPromptImagesUnsupported?.();
        return;
      }
      if (options?.guidance !== true) {
        const nativeCapabilitySubmission = resolveTurnCapabilitySlashSubmission(
          {
            capabilities: capabilityPresentations,
            draft: nextPrompt
          }
        );
        // The descriptor only identifies a semantic capability. Backend
        // selection is daemon product policy at the Host admission boundary;
        // the renderer never converts this request into a legacy prompt.
        const capabilitySubmission = nativeCapabilitySubmission;
        if (capabilitySubmission) {
          if (capabilitySubmission.kind === "unavailable") {
            showCapabilityUnavailableNotice({
              message: capabilityUnavailableMessage(
                capabilitySubmission.status,
                labels
              ),
              semantic: capabilitySubmission.semantic,
              status: capabilitySubmission.status,
              origin: "descriptorAvailability"
            });
            return;
          }
          if (!capabilitySubmission.task) return;
          const consentScope = agentSessionId || draftConsentScopeRef.current;
          const confirmedSemantic = confirmedTurnCapabilitySemantic({
            pending: pendingCapabilityConsentRef.current,
            scope: consentScope,
            snapshot: nextDraftSnapshot,
            modeActive: tuttiModeActive
          });
          if (
            pendingCapabilityConsentRef.current &&
            confirmedSemantic === null
          ) {
            pendingCapabilityConsentRef.current = null;
          }
          const invocationDecision = resolveTurnCapabilityInvocationForSlash({
            capability: capabilitySubmission.capability,
            confirmedSemantic,
            states: turnCapabilityStates
          });
          if (invocationDecision.requiresConsent) {
            setCapabilityConsentDialog({
              semantic: capabilitySubmission.capability.semantic,
              label: capabilitySubmission.capability.label,
              scope: consentScope,
              snapshot: nextDraftSnapshot,
              modeActive: tuttiModeActive
            });
            return;
          }
          const submission = projectAgentComposerDraftSubmission({
            draft: buildAgentComposerDraft({
              prompt: capabilitySubmission.task,
              images: currentDraftImages,
              files: currentDraftFiles,
              largeTexts: currentDraftLargeTexts
            }),
            skills: availableSkills
          });
          clearCapabilityUnavailableNotice();
          onSubmit(submission.content, capabilitySubmission.displayPrompt, {
            turnCapabilityInvocation: {
              ...invocationDecision.invocation
            }
          });
          pendingCapabilityConsentRef.current = null;
          return;
        }
        const slashCommandEffect = resolveSlashCommandSubmitEffect({
          browserSupported: Boolean(composerSettings.supportsBrowser),
          computerSupported: Boolean(composerSettings.supportsComputerUse),
          tuttiSupported: tuttiModeSupported,
          commands: resolvedSlashCommands,
          draft: nextPrompt,
          provider,
          policy: slashCommandPolicy
        });
        if (slashCommandEffect) {
          if (
            capabilityControlsReadOnly &&
            slashCommandEffect.kind === "submitPrompt" &&
            (slashCommandEffect.requiredSettingsPatch?.browserUse !==
              undefined ||
              slashCommandEffect.requiredSettingsPatch?.computerUse !==
                undefined)
          ) {
            return;
          }
          executeSlashCommandEffect(slashCommandEffect);
          return;
        }
        if (
          hasProviderCapabilitySlash({
            capabilities: capabilityPresentations,
            draft: nextPrompt
          })
        ) {
          return;
        }
      }
      setIsPaletteOpen(false);
      const submission = projectAgentComposerDraftSubmission({
        draft: nextDraftContent,
        skills: availableSkills
      });
      const fileReferences = agentComposerFileMentionReferences(nextPrompt);
      const draftFileIds = new Set(currentDraftFiles.map((file) => file.id));
      reportAgentComposerDiagnostic(agentActivityRuntime, {
        details: {
          contentBlockCount: submission.content.length,
          contentFileBlockCount: submission.content.filter(
            (block) => block.type === "file" && block.kind === "file"
          ).length,
          contentTypes: submission.content.map((block) => block.type),
          draftFileCount: currentDraftFiles.length,
          failedDraftFileCount: currentDraftFiles.filter((file) =>
            Boolean(file.uploadError)
          ).length,
          fileMentionCount: fileReferences.length,
          missingDraftFileCount: fileReferences.filter(
            (reference) => !draftFileIds.has(reference.id)
          ).length,
          uploadingDraftFileCount: currentDraftFiles.filter(
            (file) => file.uploading
          ).length
        },
        event: "agent.gui.composer.file_preparation.submission_projection",
        level: "info",
        source: "agent-gui",
        workspaceId
      });
      if (options?.guidance === true) {
        if (!onSubmitGuidance) {
          return;
        }
        if (submission.displayPrompt) {
          onSubmitGuidance(submission.content, submission.displayPrompt);
        } else {
          onSubmitGuidance(submission.content);
        }
      } else {
        clearCapabilityUnavailableNotice();
        if (submission.displayPrompt) {
          onSubmit(submission.content, submission.displayPrompt);
        } else {
          onSubmit(submission.content);
        }
      }
    }
  );

  const submit = useCallback(
    (event: FormEvent<HTMLFormElement>): void => {
      event.preventDefault();
      submitCurrentPrompt();
    },
    [submitCurrentPrompt]
  );

  const confirmCapabilityConsent = useCallback((): void => {
    if (!capabilityConsentDialog) return;
    const currentScope = agentSessionId || draftConsentScopeRef.current;
    if (
      currentScope !== capabilityConsentDialog.scope ||
      tuttiModeActive !== capabilityConsentDialog.modeActive ||
      capabilityConsentSnapshot(
        buildAgentComposerDraft({
          prompt: draftPromptRef.current,
          images: draftImagesRef.current,
          files: draftFilesRef.current,
          largeTexts: draftLargeTextsRef.current
        })
      ) !== capabilityConsentDialog.snapshot
    ) {
      setCapabilityConsentDialog(null);
      return;
    }
    pendingCapabilityConsentRef.current = {
      semantic: capabilityConsentDialog.semantic,
      scope: capabilityConsentDialog.scope,
      snapshot: capabilityConsentDialog.snapshot,
      modeActive: capabilityConsentDialog.modeActive
    };
    setCapabilityConsentDialog(null);
    submitCurrentPrompt();
  }, [
    agentSessionId,
    capabilityConsentDialog,
    draftFilesRef,
    draftImagesRef,
    draftLargeTextsRef,
    draftPromptRef,
    submitCurrentPrompt,
    tuttiModeActive
  ]);

  const dismissCapabilityConsent = useCallback((): void => {
    pendingCapabilityConsentRef.current = null;
    setCapabilityConsentDialog(null);
  }, []);

  const handleSlashPaletteKeyDown = useStableEventCallback(
    (event: KeyboardEvent): boolean => {
      if (!showSlashPalette) {
        return false;
      }
      if (event.key === "ArrowDown") {
        event.preventDefault();
        setHighlightedIndex((current) =>
          moveSlashCommandHighlight(current, slashPaletteEntries.length, 1)
        );
        return true;
      }
      if (event.key === "ArrowUp") {
        event.preventDefault();
        setHighlightedIndex((current) =>
          moveSlashCommandHighlight(current, slashPaletteEntries.length, -1)
        );
        return true;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        setIsPaletteOpen(false);
        return true;
      }
      if (event.key === "Tab" || event.key === "Enter") {
        event.preventDefault();
        const activeEntry = slashPaletteEntries[activeHighlight];
        if (
          (activeEntry?.type === "capability" ||
            activeEntry?.type === "plugin" ||
            activeEntry?.type === "providerCapability") &&
          activeEntry.disabled
        ) {
          return true;
        }
        if (activeEntry?.type === "command") {
          selectCommand(activeEntry.command);
        } else if (activeEntry?.type === "capability") {
          if (activeEntry.selectAction === "settings") {
            selectCapabilitySettings(activeEntry.capability);
          } else {
            selectCapability(activeEntry.capability);
          }
        } else if (activeEntry?.type === "providerCapability") {
          selectProviderCapability(activeEntry.capability);
        } else if (activeEntry?.type === "plugin") {
          if (activeEntry.selectAction === "settings") {
            selectPluginSettings(activeEntry.plugin);
          } else {
            selectSkill(activeEntry.plugin);
          }
        } else if (activeEntry?.type === "skill") {
          selectSkill(activeEntry.skill);
        }
        return true;
      }
      return false;
    }
  );

  const handleSlashCommandMenuKeyDown = useStableEventCallback(
    (event: KeyboardEvent): boolean => {
      if (!showCommandMenuPanel || event.key !== "Escape") {
        return false;
      }
      event.preventDefault();
      closeSlashFloatingMenu();
      return true;
    }
  );

  return {
    clearSlashCommandDraft,
    capabilityConsentDialog,
    capabilityUnavailableNotice,
    closeReviewPicker,
    closeSlashFloatingMenu,
    closeSlashStatusPanel,
    dismissCapabilityConsent,
    composerControlsHardDisabled,
    confirmCapabilityConsent,
    executeSlashCommandEffect,
    handleSlashCommandMenuKeyDown,
    handleSlashPaletteKeyDown,
    permissionModeControlsDisabled,
    reviewBranchLoader,
    selectCapability,
    selectCapabilitySettings,
    selectCommand,
    selectPluginSettings,
    selectProviderCapability,
    selectSkill,
    settingsControlsDisabled,
    submit,
    submitCurrentPrompt,
    submitReviewCommand
  };
}
