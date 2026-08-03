/** A provider-neutral capability requested for an existing agent turn. */
export type AgentActivityTurnCapabilitySemantic =
  | "sites"
  | "browserUse"
  | "computerUse";

export type AgentActivityTurnCapabilityConsent = "explicitSession";

/** Durable, provider-neutral session fact for a successfully bound capability. */
export interface AgentActivityTurnCapabilityState {
  semantic: AgentActivityTurnCapabilitySemantic;
  state: "bound";
}

/**
 * Deliberately narrow turn-scoped capability intent. Provider configuration
 * remains behind the runtime boundary.
 */
export interface AgentActivityTurnCapabilityInvocation {
  semantic: AgentActivityTurnCapabilitySemantic;
  consent?: AgentActivityTurnCapabilityConsent;
}

export const AGENT_ACTIVITY_INVALID_TURN_CAPABILITY_INVOCATION =
  "agent_activity.turn_capability_invocation_invalid";

/**
 * Normalizes an untrusted transport value before it can enter the prompt
 * queue. Reject extra fields so arbitrary provider data cannot cross the
 * activity-core boundary with a valid semantic.
 */
export function normalizeAgentActivityTurnCapabilityInvocation(
  value: unknown
): AgentActivityTurnCapabilityInvocation | undefined {
  if (
    value === null ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    Object.keys(value).some((key) => key !== "semantic" && key !== "consent") ||
    !Object.hasOwn(value, "semantic")
  ) {
    return undefined;
  }
  const semantic = (value as { semantic?: unknown }).semantic;
  switch (semantic) {
    case "sites":
    case "browserUse":
    case "computerUse":
      if (
        Object.hasOwn(value, "consent") &&
        (value as { consent?: unknown }).consent !== "explicitSession"
      ) {
        return undefined;
      }
      return {
        semantic,
        ...((value as { consent?: unknown }).consent === "explicitSession"
          ? { consent: "explicitSession" as const }
          : {})
      };
    default:
      return undefined;
  }
}

/**
 * Preserves the backward-compatible absent case while fail-closing a supplied
 * value that cannot cross the activity boundary as a turn capability.
 */
export function requireAgentActivityTurnCapabilityInvocation(
  value: unknown
): AgentActivityTurnCapabilityInvocation | undefined {
  if (value === undefined) return undefined;
  const normalized = normalizeAgentActivityTurnCapabilityInvocation(value);
  if (normalized) return normalized;
  throw new Error(AGENT_ACTIVITY_INVALID_TURN_CAPABILITY_INVOCATION);
}
