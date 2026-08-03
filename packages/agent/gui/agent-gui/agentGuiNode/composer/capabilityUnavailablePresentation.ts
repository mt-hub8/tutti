import type { AgentActivityComposerCapabilityPresentation } from "@tutti-os/agent-activity-core";
import type { AgentComposerProps } from "./AgentComposer.types";
import { getAgentComposerTriggerQueryMatch } from "../model/agentComposerTriggerQueries";

export function capabilityUnavailableMessage(
  status: AgentActivityComposerCapabilityPresentation["status"],
  labels: AgentComposerProps["labels"]
): string {
  switch (status) {
    case "notInstalled":
    case "setupRequired":
      return labels.providerCapabilitySetupRequiredDescription;
    case "disabled":
      return labels.providerCapabilityDisabledDescription;
    case "disabledByAdmin":
      return labels.providerCapabilityDisabledByAdminDescription;
    case "unsupported":
      return labels.providerCapabilityUnsupportedDescription;
    default:
      return labels.providerCapabilityAvailabilityUnknownDescription;
  }
}

export function providerCapabilityDraftForSelection(
  draft: string,
  capability: AgentActivityComposerCapabilityPresentation
): string | null {
  if (
    capability.status !== "available" ||
    capability.invocation !== "promptItem" ||
    capability.invocationScope !== "turn"
  )
    return null;
  const trigger = capability.trigger.trim();
  if (!trigger.startsWith("/")) return null;
  const match = getAgentComposerTriggerQueryMatch(draft);
  return match?.prefix === "/"
    ? `${draft.slice(0, match.start)}${trigger} ${draft.slice(match.end)}`
    : `${trigger} `;
}
