import type {
  AgentActivityComposerCapabilityPresentation,
  AgentActivityTurnCapabilitySemantic,
  AgentActivityTurnCapabilityState
} from "@tutti-os/agent-activity-core";

export interface TurnCapabilityConsentConfirmation {
  semantic: AgentActivityTurnCapabilitySemantic;
  scope: string;
  snapshot: string;
  modeActive: boolean;
}

// A confirmation authorizes one immutable composer snapshot only. Mode is part
// of that scope because it changes the daemon-selected backend; it must never
// turn a confirmation for one backend into consent for another.
export function confirmedTurnCapabilitySemantic(input: {
  pending: TurnCapabilityConsentConfirmation | null;
  scope: string;
  snapshot: string;
  modeActive: boolean;
}): AgentActivityTurnCapabilitySemantic | null {
  const pending = input.pending;
  if (
    !pending ||
    pending.scope !== input.scope ||
    pending.snapshot !== input.snapshot ||
    pending.modeActive !== input.modeActive
  ) {
    return null;
  }
  return pending.semantic;
}

export function resolveTurnCapabilityInvocationForSlash(input: {
  capability: AgentActivityComposerCapabilityPresentation;
  confirmedSemantic: AgentActivityTurnCapabilitySemantic | null;
  states: readonly AgentActivityTurnCapabilityState[];
}) {
  const requiresConsent =
    input.capability.consentRequirement === "explicitSession" &&
    !input.states.some(
      (state) =>
        state.semantic === input.capability.semantic && state.state === "bound"
    ) &&
    input.confirmedSemantic !== input.capability.semantic;
  return {
    invocation: {
      semantic: input.capability.semantic,
      ...(input.capability.consentRequirement === "explicitSession" &&
      input.confirmedSemantic === input.capability.semantic
        ? { consent: "explicitSession" as const }
        : {})
    },
    requiresConsent
  };
}

export function hasProviderCapabilitySlash(input: {
  capabilities: readonly AgentActivityComposerCapabilityPresentation[];
  draft: string;
}): boolean {
  return input.capabilities.some((capability) =>
    slashCapabilityTask(input.draft, capability.trigger)
  );
}

export function resolveTurnCapabilitySlashSubmission(input: {
  capabilities: readonly AgentActivityComposerCapabilityPresentation[];
  draft: string;
}):
  | {
      kind: "ready";
      capability: AgentActivityComposerCapabilityPresentation;
      displayPrompt: string;
      task: string;
    }
  | {
      kind: "unavailable";
      semantic: AgentActivityTurnCapabilitySemantic;
      status: AgentActivityComposerCapabilityPresentation["status"];
    }
  | null {
  for (const capability of input.capabilities) {
    const task = slashCapabilityTask(input.draft, capability.trigger);
    if (task === null) continue;
    if (
      capability.status !== "available" ||
      capability.invocation !== "promptItem" ||
      capability.invocationScope !== "turn"
    ) {
      return {
        kind: "unavailable",
        semantic: capability.semantic,
        status: capability.status
      };
    }
    return { kind: "ready", capability, displayPrompt: input.draft, task };
  }
  return null;
}

function slashCapabilityTask(draft: string, trigger: string): string | null {
  const normalizedTrigger = trigger.trim();
  if (!normalizedTrigger.startsWith("/")) return null;
  const leading = /^\s*/.exec(draft)?.[0] ?? "";
  const remaining = draft.slice(leading.length);
  if (
    remaining.slice(0, normalizedTrigger.length).toLowerCase() !==
      normalizedTrigger.toLowerCase() ||
    (remaining.length > normalizedTrigger.length &&
      !/\s/.test(remaining[normalizedTrigger.length] ?? ""))
  ) {
    return null;
  }
  return remaining.slice(normalizedTrigger.length).trimStart();
}
