import { describe, expect, it } from "vitest";
import {
  confirmedTurnCapabilitySemantic,
  resolveTurnCapabilityInvocationForSlash,
  resolveTurnCapabilitySlashSubmission
} from "./turnCapabilitySlashSubmission";

const browser = {
  name: "Browser",
  label: "Browser",
  trigger: "/browser",
  status: "available" as const,
  invocation: "promptItem" as const,
  invocationScope: "turn" as const,
  semantic: "browserUse" as const
};

describe("resolveTurnCapabilitySlashSubmission", () => {
  it("keeps the slash text for display while sending only the task", () => {
    expect(
      resolveTurnCapabilitySlashSubmission({
        capabilities: [browser],
        draft: "/browser inspect the release page"
      })
    ).toEqual({
      kind: "ready",
      capability: browser,
      displayPrompt: "/browser inspect the release page",
      task: "inspect the release page"
    });
  });

  it("reports create-only Computer as unavailable instead of silently submitting", () => {
    expect(
      resolveTurnCapabilitySlashSubmission({
        capabilities: [
          {
            ...browser,
            name: "Computer",
            label: "Computer",
            trigger: "/computer",
            semantic: "computerUse",
            invocationScope: "createOnly"
          }
        ],
        draft: "/computer open settings"
      })
    ).toEqual({
      kind: "unavailable",
      semantic: "computerUse",
      status: "available"
    });
  });

  it("allows an available turn-scoped descriptor on a new conversation", () => {
    expect(
      resolveTurnCapabilitySlashSubmission({
        capabilities: [browser],
        draft: "/browser inspect the release page"
      })
    ).toEqual({
      kind: "ready",
      capability: browser,
      displayPrompt: "/browser inspect the release page",
      task: "inspect the release page"
    });
  });

  it("keeps a Sites task separate from its slash display prompt", () => {
    const sites = {
      ...browser,
      name: "Sites",
      label: "Sites",
      semantic: "sites" as const,
      trigger: "/sites"
    };
    expect(
      resolveTurnCapabilitySlashSubmission({
        capabilities: [sites],
        draft: "/sites build a landing page"
      })
    ).toEqual({
      kind: "ready",
      capability: sites,
      displayPrompt: "/sites build a landing page",
      task: "build a landing page"
    });
  });

  it.each([
    "disabled",
    "disabledByAdmin",
    "authRequired",
    "setupRequired",
    "notInstalled",
    "unsupported",
    "unknown",
    "error"
  ] as const)("reports %s as an explicit unavailable result", (status) => {
    expect(
      resolveTurnCapabilitySlashSubmission({
        capabilities: [{ ...browser, status }],
        draft: "/browser inspect the release page"
      })
    ).toEqual({ kind: "unavailable", semantic: "browserUse", status });
  });
});

describe("resolveTurnCapabilityInvocationForSlash", () => {
  const computer = {
    ...browser,
    name: "Computer",
    label: "Computer",
    trigger: "/computer",
    semantic: "computerUse" as const,
    consentRequirement: "explicitSession" as const
  };

  it("requires confirmation until a canonical ready binding exists", () => {
    expect(
      resolveTurnCapabilityInvocationForSlash({
        capability: computer,
        confirmedSemantic: null,
        states: []
      })
    ).toEqual({
      invocation: { semantic: "computerUse" },
      requiresConsent: true
    });
  });

  it("attaches consent only after this dialog confirmation", () => {
    expect(
      resolveTurnCapabilityInvocationForSlash({
        capability: computer,
        confirmedSemantic: "computerUse",
        states: []
      })
    ).toEqual({
      invocation: { semantic: "computerUse", consent: "explicitSession" },
      requiresConsent: false
    });
  });

  it.each([false, true])(
    "requires an explicit Computer confirmation before a %s-mode submit",
    (modeActive) => {
      const scope = "session-a";
      const snapshot = "/computer inspect the settings";
      const beforeConfirmation = confirmedTurnCapabilitySemantic({
        pending: null,
        scope,
        snapshot,
        modeActive
      });
      expect(
        resolveTurnCapabilityInvocationForSlash({
          capability: computer,
          confirmedSemantic: beforeConfirmation,
          states: []
        })
      ).toEqual({
        invocation: { semantic: "computerUse" },
        requiresConsent: true
      });

      const afterConfirmation = confirmedTurnCapabilitySemantic({
        pending: { semantic: "computerUse", scope, snapshot, modeActive },
        scope,
        snapshot,
        modeActive
      });
      expect(
        resolveTurnCapabilityInvocationForSlash({
          capability: computer,
          confirmedSemantic: afterConfirmation,
          states: []
        })
      ).toEqual({
        invocation: { semantic: "computerUse", consent: "explicitSession" },
        requiresConsent: false
      });
    }
  );

  it.each([false, true])(
    "does not require a new Computer consent for a bound session in %s mode",
    (modeActive) => {
      expect(
        resolveTurnCapabilityInvocationForSlash({
          capability: computer,
          confirmedSemantic: confirmedTurnCapabilitySemantic({
            pending: null,
            scope: "session-a",
            snapshot: "/computer inspect",
            modeActive
          }),
          states: [{ semantic: "computerUse", state: "bound" }]
        })
      ).toEqual({
        invocation: { semantic: "computerUse" },
        requiresConsent: false
      });
    }
  );

  it("invalidates a dialog confirmation when mode, scope, or draft changes", () => {
    const pending = {
      semantic: "computerUse" as const,
      scope: "session-a",
      snapshot: "/computer inspect",
      modeActive: false
    };
    expect(
      confirmedTurnCapabilitySemantic({
        pending,
        scope: "session-a",
        snapshot: "/computer inspect",
        modeActive: true
      })
    ).toBeNull();
    expect(
      confirmedTurnCapabilitySemantic({
        pending,
        scope: "session-b",
        snapshot: "/computer inspect",
        modeActive: false
      })
    ).toBeNull();
    expect(
      confirmedTurnCapabilitySemantic({
        pending,
        scope: "session-a",
        snapshot: "/computer edited",
        modeActive: false
      })
    ).toBeNull();
  });

  it("does not repeat consent for a canonical bound session", () => {
    expect(
      resolveTurnCapabilityInvocationForSlash({
        capability: computer,
        confirmedSemantic: null,
        states: [{ semantic: "computerUse", state: "bound" }]
      })
    ).toEqual({
      invocation: { semantic: "computerUse" },
      requiresConsent: false
    });
  });
});
