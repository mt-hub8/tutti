import { useMemo, type RefObject } from "react";
import type { AgentActivityComposerCapabilityPresentation } from "@tutti-os/agent-activity-core";
import type { AgentSessionCommand } from "../../../shared/agentSessionTypes";
import type { UiLanguage } from "../../../contexts/settings/domain/agentSettings";
import type {
  AgentGUIComposerSettingsVM,
  AgentGUIProviderSkillOption
} from "../model/agentGuiNodeTypes";
import type { AgentRichTextEditorHandle } from "../agentRichText/AgentRichTextEditor";
import type { AgentCapabilityTokenOption } from "../agentRichText/agentCapabilityTokenExtension";
import type {
  AgentComposerCapabilityMenuState,
  AgentComposerProps
} from "./AgentComposer.types";
import {
  filterSlashCommands,
  labelForSlashCommand
} from "../model/agentSlashCommands";
import {
  labelForProviderSkill,
  skillDescriptionForDisplay,
  skillTriggerForPrefix
} from "../model/agentSkillOptions";
import {
  filterProviderSkillsForTrigger,
  getAgentComposerTriggerQueryMatch,
  getPromptStartSlashCommandQuery
} from "../model/agentComposerTriggerQueries";
import {
  resolveSlashCommandsForProvider,
  type AgentSlashCommand,
  type AgentSlashCommandCapability
} from "../model/agentSlashCommandProviderPolicy";
import {
  slashCommandDescriptionForDisplay,
  slashCommandLabelForDisplay
} from "./slashCommandDisplay";
import type { AgentSlashPaletteEntry } from "../AgentSlashCommandPalette";

interface UseComposerPaletteCatalogInput {
  provider: string;
  isGoalModeActive: boolean;
  goalSupported: boolean;
  paletteDraftPrompt: string;
  availableCommands: readonly AgentSessionCommand[];
  availableSkills: readonly AgentGUIProviderSkillOption[];
  capabilityPresentations?: readonly AgentActivityComposerCapabilityPresentation[];
  hasCompactableContext: boolean;
  compactSupported: boolean | null;
  composerSettings: AgentGUIComposerSettingsVM;
  capabilityMenuState?: AgentComposerCapabilityMenuState;
  capabilityControlsReadOnly: boolean;
  tuttiModeActive?: boolean;
  labels: AgentComposerProps["labels"];
  uiLanguage: UiLanguage;
  editorHandleRef: RefObject<AgentRichTextEditorHandle | null>;
}

function isSlashCommandCapability(
  command: AgentSlashCommand
): command is AgentSlashCommandCapability {
  return "kind" in command && command.kind === "capability";
}

export function useComposerPaletteCatalog({
  provider,
  isGoalModeActive,
  goalSupported,
  paletteDraftPrompt,
  availableCommands,
  availableSkills,
  capabilityPresentations = [],
  hasCompactableContext,
  compactSupported,
  composerSettings,
  capabilityMenuState,
  capabilityControlsReadOnly,
  tuttiModeActive = false,
  labels,
  uiLanguage,
  editorHandleRef
}: UseComposerPaletteCatalogInput) {
  const slashQuery = isGoalModeActive
    ? null
    : getPromptStartSlashCommandQuery(paletteDraftPrompt);
  const slashCommandPolicy = composerSettings.slashCommandPolicy;
  const promptBeforeSelection =
    editorHandleRef.current?.getPromptTextBeforeSelection() ?? "";
  const skillQueryDraft = promptBeforeSelection || paletteDraftPrompt;
  const triggerQueryMatch = getAgentComposerTriggerQueryMatch(skillQueryDraft);
  // `$` is reserved for native Composer plugins. `/` remains the command and
  // capability surface; do not synthesize a plugin alias into it.
  const skillQueryMatch =
    triggerQueryMatch?.prefix === "$" ? triggerQueryMatch : null;
  const resolvedSlashCommands = useMemo(
    () =>
      resolveSlashCommandsForProvider({
        provider,
        policy: slashCommandPolicy,
        commands: availableCommands,
        hasCompactableContext,
        compactSupported,
        planSupported: composerSettings.supportsPlanMode,
        browserSupported: Boolean(composerSettings.supportsBrowser),
        computerSupported: Boolean(composerSettings.supportsComputerUse),
        tuttiSupported: capabilityMenuState?.tuttiMode?.enabled === true
      }).filter(
        (command) =>
          (goalSupported || command.name.trim().toLowerCase() !== "goal") &&
          (!isSlashCommandCapability(command) ||
            !capabilityPresentations.some(
              (capability) => capability.semantic === command.capability
            ))
      ),
    [
      availableCommands,
      capabilityMenuState?.tuttiMode?.enabled,
      capabilityPresentations,
      compactSupported,
      composerSettings.supportsBrowser,
      composerSettings.supportsComputerUse,
      composerSettings.supportsPlanMode,
      goalSupported,
      hasCompactableContext,
      provider,
      slashCommandPolicy,
      tuttiModeActive
    ]
  );
  const filteredCommands = useMemo(
    () =>
      slashQuery === null
        ? []
        : filterSlashCommands(resolvedSlashCommands, slashQuery),
    [resolvedSlashCommands, slashQuery]
  );
  const filteredCapabilityPresentations = useMemo(
    () =>
      slashQuery === null
        ? []
        : filterSlashCommands(
            capabilityPresentations.filter((capability) =>
              capability.trigger.startsWith("/")
            ),
            slashQuery
          ),
    [capabilityPresentations, slashQuery]
  );
  const filteredSkills = useMemo(
    () =>
      skillQueryMatch === null
        ? []
        : filterProviderSkillsForTrigger({
            skills: availableSkills,
            query: skillQueryMatch.query,
            triggerPrefix: "$"
          }),
    [availableSkills, skillQueryMatch]
  );
  const availableCapabilities = useMemo<AgentCapabilityTokenOption[]>(() => {
    if (capabilityControlsReadOnly) return [];
    const descriptorSemantics = new Set(
      capabilityPresentations.map((capability) => capability.semantic)
    );
    const entries: AgentCapabilityTokenOption[] = [];
    if (
      composerSettings.supportsBrowser &&
      !descriptorSemantics.has("browserUse")
    ) {
      entries.push({
        capability: "browserUse",
        label: labels.browserUseCapabilityLabel,
        name: "browser",
        trigger: "/browser"
      });
    }
    if (
      composerSettings.supportsComputerUse &&
      !descriptorSemantics.has("computerUse")
    ) {
      entries.push({
        capability: "computerUse",
        label: labels.computerUseCapabilityLabel,
        name: "computer",
        trigger: "/computer"
      });
    }
    return entries;
  }, [
    capabilityControlsReadOnly,
    capabilityPresentations,
    composerSettings.supportsBrowser,
    composerSettings.supportsComputerUse,
    labels.browserUseCapabilityLabel,
    labels.computerUseCapabilityLabel,
    tuttiModeActive
  ]);
  const slashPaletteEntries = useMemo<AgentSlashPaletteEntry[]>(() => {
    const commandEntries = filteredCommands.flatMap<AgentSlashPaletteEntry>(
      (command) => {
        if (isSlashCommandCapability(command)) {
          const browserConnectionMode =
            capabilityMenuState?.browserUse?.connectionMode ?? null;
          const computerUseInstalled =
            capabilityMenuState?.computerUse?.installed ?? null;
          const computerUseAuthorization =
            capabilityMenuState?.computerUse?.authorization ?? null;
          const capLabel =
            command.capability === "tutti"
              ? labels.tuttiModeLabel
              : command.capability === "computerUse"
                ? labels.computerUseCapabilityLabel
                : labels.browserUseCapabilityLabel;
          const capDescription =
            command.capability === "tutti"
              ? labels.tuttiModeDescription
              : command.capability === "computerUse"
                ? computerUseInstalled === false
                  ? labels.computerUseCapabilitySetupRequiredDescription
                  : computerUseAuthorization === "needs-authorization"
                    ? labels.computerUseCapabilityAuthorizationRequiredDescription
                    : computerUseAuthorization === "unknown"
                      ? labels.computerUseCapabilityAuthorizationUnknownDescription
                      : labels.computerUseCapabilityDescription
                : browserConnectionMode === "autoConnect"
                  ? labels.browserUseCapabilityDescriptionAutoConnect
                  : browserConnectionMode === "isolated"
                    ? labels.browserUseCapabilityDescriptionIsolated
                    : labels.browserUseCapabilityDescription;
          return [
            {
              type: "capability",
              key: `capability:${command.capability}`,
              label: capLabel,
              description: capDescription,
              settingsAriaLabel:
                command.capability === "computerUse"
                  ? labels.computerUseCapabilitySettingsLabel
                  : labels.browserUseCapabilitySettingsLabel,
              settingsLabel: labels.capabilityInlineSettingsLabel,
              disabled: capabilityControlsReadOnly,
              selectAction:
                command.capability === "computerUse" &&
                (computerUseInstalled === false ||
                  (computerUseInstalled === true &&
                    (computerUseAuthorization === "needs-authorization" ||
                      computerUseAuthorization === "unknown")))
                  ? "settings"
                  : "capability",
              capability: command
            }
          ];
        }
        const commandDescription = slashCommandDescriptionForDisplay(
          command,
          labels
        );
        return [
          {
            type: "command",
            key: `command:${command.name}`,
            label: labelForSlashCommand(command),
            ...slashCommandLabelForDisplay(command, labels, uiLanguage),
            ...(commandDescription ? { description: commandDescription } : {}),
            command
          }
        ];
      }
    );
    const capabilityEntries: AgentSlashPaletteEntry[] =
      filteredCapabilityPresentations.map((capability) => ({
        type: "providerCapability",
        key: `capability:${capability.semantic}`,
        label: capability.label,
        ...(providerCapabilityDescription(capability, labels)
          ? { description: providerCapabilityDescription(capability, labels) }
          : {}),
        disabled:
          capability.status !== "available" ||
          capability.invocation !== "promptItem" ||
          capability.invocationScope !== "turn",
        capability
      }));
    const skillEntries: AgentSlashPaletteEntry[] = filteredSkills.map(
      (skill) => {
        if (skill.kind === "plugin" && skill.semantic !== undefined) {
          return {
            type: "plugin",
            key: `plugin:${skill.semantic ?? skill.pluginName ?? skill.name}`,
            label: nativePluginLabel(skill, labels),
            ...(skillDescriptionForDisplay(skill.description)
              ? { description: skillDescriptionForDisplay(skill.description) }
              : {}),
            selectAction: skill.status === "available" ? "insert" : "settings",
            disabled:
              skill.status !== "available" && skill.semantic !== "computerUse",
            plugin: skill
          };
        }
        const trigger = skillTriggerForPrefix(skill, skillQueryMatch?.prefix);
        return {
          type: "skill",
          key: `skill:${trigger}`,
          label: labelForProviderSkill(skill, skillQueryMatch?.prefix),
          ...(skillDescriptionForDisplay(skill.description)
            ? { description: skillDescriptionForDisplay(skill.description) }
            : {}),
          skill
        };
      }
    );
    return [...commandEntries, ...capabilityEntries, ...skillEntries];
  }, [
    capabilityControlsReadOnly,
    capabilityMenuState?.browserUse?.connectionMode,
    capabilityMenuState?.computerUse?.authorization,
    capabilityMenuState?.computerUse?.installed,
    filteredCapabilityPresentations,
    filteredCommands,
    filteredSkills,
    labels,
    skillQueryMatch?.prefix,
    uiLanguage
  ]);
  return {
    availableCapabilities,
    filteredSkills,
    resolvedSlashCommands,
    skillQueryMatch,
    slashPaletteEntries,
    slashQuery,
    slashCommandPolicy,
    promptBeforeSelection
  };
}

function nativePluginLabel(
  plugin: AgentGUIProviderSkillOption,
  labels: AgentComposerProps["labels"]
): string {
  switch (plugin.semantic) {
    case "browserUse":
      return labels.browserUseCapabilityLabel;
    case "computerUse":
      return labels.computerUseCapabilityLabel;
    default:
      return plugin.name;
  }
}

function providerCapabilityDescription(
  capability: AgentActivityComposerCapabilityPresentation,
  labels: AgentComposerProps["labels"]
): string | undefined {
  switch (capability.status) {
    case "setupRequired":
    case "notInstalled":
      return labels.providerCapabilitySetupRequiredDescription;
    case "disabled":
      return labels.providerCapabilityDisabledDescription;
    case "disabledByAdmin":
      return labels.providerCapabilityDisabledByAdminDescription;
    case "unsupported":
      return labels.providerCapabilityUnsupportedDescription;
    case "unknown":
    case "error":
      return labels.providerCapabilityAvailabilityUnknownDescription;
    default:
      return capability.description;
  }
}
