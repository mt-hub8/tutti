import {
  useCallback,
  type Dispatch,
  type RefObject,
  type SetStateAction
} from "react";
import type { AgentActivityComposerCapabilityPresentation } from "@tutti-os/agent-activity-core";
import type {
  AgentComposerDraft,
  AgentGUIProviderSkillOption
} from "../model/agentGuiNodeTypes";
import { updateAgentComposerDraft } from "../model/agentComposerDraft";
import { providerCapabilityDraftForSelection } from "./capabilityUnavailablePresentation";

export function useProviderCapabilitySelection(input: {
  capabilityControlsReadOnly: boolean;
  clearUnavailableNotice: () => void;
  draftContent: AgentComposerDraft;
  draftPromptRef: RefObject<string>;
  onCapabilitySettingsRequest?: (capability: "computerUse") => void;
  onDraftContentChange: (draft: AgentComposerDraft) => void;
  setIsPaletteOpen: Dispatch<SetStateAction<boolean>>;
  setPaletteDraftPrompt: Dispatch<SetStateAction<string>>;
}) {
  const selectProviderCapability = useCallback(
    (capability: AgentActivityComposerCapabilityPresentation): void => {
      const nextDraft = providerCapabilityDraftForSelection(
        input.draftPromptRef.current,
        capability
      );
      if (nextDraft === null) return;
      input.clearUnavailableNotice();
      input.draftPromptRef.current = nextDraft;
      input.setPaletteDraftPrompt(nextDraft);
      input.onDraftContentChange(
        updateAgentComposerDraft(input.draftContent, { prompt: nextDraft })
      );
      input.setIsPaletteOpen(false);
    },
    [input]
  );
  const selectPluginSettings = useCallback(
    (plugin: AgentGUIProviderSkillOption): void => {
      if (
        input.capabilityControlsReadOnly ||
        plugin.semantic !== "computerUse"
      ) {
        return;
      }
      input.onCapabilitySettingsRequest?.("computerUse");
      input.setIsPaletteOpen(false);
    },
    [input]
  );
  return { selectPluginSettings, selectProviderCapability };
}
