import type { Dispatch, RefObject, SetStateAction } from "react";
import type {
  AgentComposerDraft,
  AgentComposerDraftFile,
  AgentComposerDraftImage,
  AgentComposerDraftLargeText
} from "../model/agentGuiNodeTypes";
import type { AgentRichTextEditorHandle } from "../agentRichText/AgentRichTextEditor";
import type { AgentSlashPaletteEntry } from "../AgentSlashCommandPalette";
import type { AgentSlashCommand } from "../model/agentSlashCommandProviderPolicy";
import type { AgentComposerProps } from "./AgentComposer.types";
import type { TriggerMatch } from "./useComposerSlashActions";

type Props = Pick<
  AgentComposerProps,
  | "workspaceId"
  | "provider"
  | "isSendingTurn"
  | "isSubmittingPrompt"
  | "showStopButton"
  | "promptImagesSupported"
  | "availableSkills"
  | "capabilityPresentations"
  | "turnCapabilityStates"
  | "agentSessionId"
  | "composerSettings"
  | "capabilityControlsReadOnly"
  | "onDraftContentChange"
  | "onSettingsChange"
  | "onSubmit"
  | "onSubmitEmpty"
  | "onSubmitGuidance"
  | "onCapabilitySettingsRequest"
  | "onSlashStatusOpen"
  | "onSlashStatusClose"
  | "onPromptImagesUnsupported"
  | "labels"
  | "onRequestGitBranches"
> & { disabled: boolean; submitDisabled: boolean; canQueueWhileBusy: boolean };

export interface UseComposerSlashActionsInput extends Props {
  tuttiModeActive: boolean;
  onTuttiModeActivate?: () => void;
  tuttiModeSupported: boolean;
  draftContent: AgentComposerDraft;
  selectedProjectPath: string;
  slashStatusAgentSessionId: string | null;
  isSlashStatusPanelOpen: boolean;
  slashCommandPolicy: AgentComposerProps["composerSettings"]["slashCommandPolicy"];
  skillQueryMatch: TriggerMatch;
  promptBeforeSelection: string;
  resolvedSlashCommands: readonly AgentSlashCommand[];
  slashPaletteEntries: readonly AgentSlashPaletteEntry[];
  activeHighlight: number;
  showSlashPalette: boolean;
  showCommandMenuPanel: boolean;
  isSelectedProjectMissing: boolean;
  editorHandleRef: RefObject<AgentRichTextEditorHandle | null>;
  draftPromptRef: RefObject<string>;
  draftImagesRef: RefObject<AgentComposerDraftImage[]>;
  draftFilesRef: RefObject<AgentComposerDraftFile[]>;
  draftLargeTextsRef: RefObject<AgentComposerDraftLargeText[]>;
  setPaletteDraftPrompt: Dispatch<SetStateAction<string>>;
  setIsPaletteOpen: Dispatch<SetStateAction<boolean>>;
  setIsReviewPickerOpen: Dispatch<SetStateAction<boolean>>;
  setIsSlashStatusPanelOpen: Dispatch<SetStateAction<boolean>>;
  setHighlightedIndex: Dispatch<SetStateAction<number>>;
}
