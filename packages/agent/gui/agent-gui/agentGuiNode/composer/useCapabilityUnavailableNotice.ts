import { useCallback, useEffect, useRef, useState } from "react";
import type {
  AgentActivityComposerCapabilityPresentation,
  AgentActivityTurnCapabilitySemantic
} from "@tutti-os/agent-activity-core";

type CapabilityStatus = AgentActivityComposerCapabilityPresentation["status"];

interface CapabilityUnavailableContext {
  draftSnapshot: string;
  modeActive: boolean;
  provider: string;
  scope: string;
  semantic: AgentActivityTurnCapabilitySemantic;
  origin: "backendUnavailable" | "descriptorAvailability";
}

export function useCapabilityUnavailableNotice(input: {
  capabilities: readonly AgentActivityComposerCapabilityPresentation[];
  draftSnapshot: string;
  modeActive: boolean;
  provider: string;
  scope: string;
}) {
  const [notice, setNotice] = useState<string | null>(null);
  const contextRef = useRef<CapabilityUnavailableContext | null>(null);
  const clear = useCallback(() => {
    contextRef.current = null;
    setNotice(null);
  }, []);
  const show = useCallback(
    (value: {
      message: string;
      semantic: AgentActivityTurnCapabilitySemantic;
      status: CapabilityStatus;
      origin: "backendUnavailable" | "descriptorAvailability";
    }) => {
      contextRef.current = {
        draftSnapshot: input.draftSnapshot,
        modeActive: input.modeActive,
        provider: input.provider,
        scope: input.scope,
        semantic: value.semantic,
        origin: value.origin
      };
      setNotice(value.message);
    },
    [input.draftSnapshot, input.modeActive, input.provider, input.scope]
  );

  useEffect(() => {
    const context = contextRef.current;
    if (!context) return;
    const currentCapability = input.capabilities.find(
      (capability) => capability.semantic === context.semantic
    );
    if (
      context.draftSnapshot !== input.draftSnapshot ||
      context.modeActive !== input.modeActive ||
      context.provider !== input.provider ||
      context.scope !== input.scope ||
      (context.origin === "descriptorAvailability" &&
        currentCapability?.status === "available" &&
        currentCapability.invocation === "promptItem" &&
        currentCapability.invocationScope === "turn")
    ) {
      clear();
    }
  }, [
    clear,
    input.capabilities,
    input.draftSnapshot,
    input.modeActive,
    input.provider,
    input.scope
  ]);

  return {
    capabilityUnavailableNotice: notice,
    clearCapabilityUnavailableNotice: clear,
    showCapabilityUnavailableNotice: show
  };
}
