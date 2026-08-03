import {
  act,
  fireEvent,
  render,
  renderHook,
  screen
} from "@testing-library/react";
import type { AgentActivityComposerCapabilityPresentation } from "@tutti-os/agent-activity-core";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import { AgentChromeNotice } from "../AgentSessionChrome";
import { useCapabilityUnavailableNotice } from "./useCapabilityUnavailableNotice";

const unavailableBrowser: AgentActivityComposerCapabilityPresentation = {
  semantic: "browserUse" as const,
  name: "Browser",
  label: "Browser",
  trigger: "/browser",
  status: "notInstalled" as const,
  invocation: "promptItem" as const,
  invocationScope: "turn" as const
};

describe("useCapabilityUnavailableNotice", () => {
  it("keeps an unchanged unavailable draft visible but clears after edit or successful submission", () => {
    const rendered = renderHook(
      ({ draftSnapshot }) =>
        useCapabilityUnavailableNotice({
          capabilities: [unavailableBrowser],
          draftSnapshot,
          modeActive: false,
          provider: "provider-a",
          scope: "session-a"
        }),
      { initialProps: { draftSnapshot: "/browser task" } }
    );
    act(() => {
      rendered.result.current.showCapabilityUnavailableNotice({
        message: "Install Browser",
        semantic: "browserUse",
        status: "notInstalled",
        origin: "descriptorAvailability"
      });
    });
    expect(rendered.result.current.capabilityUnavailableNotice).toBe(
      "Install Browser"
    );
    rendered.rerender({ draftSnapshot: "/browser task" });
    expect(rendered.result.current.capabilityUnavailableNotice).toBe(
      "Install Browser"
    );
    rendered.rerender({ draftSnapshot: "/browser edited task" });
    expect(rendered.result.current.capabilityUnavailableNotice).toBeNull();

    act(() => {
      rendered.result.current.showCapabilityUnavailableNotice({
        message: "Install Browser",
        semantic: "browserUse",
        status: "notInstalled",
        origin: "descriptorAvailability"
      });
      rendered.result.current.clearCapabilityUnavailableNotice();
    });
    expect(rendered.result.current.capabilityUnavailableNotice).toBeNull();
  });

  it("clears on session, mode, provider, or recovered capability changes", () => {
    const rendered = renderHook(
      ({ capabilities, modeActive, provider, scope }) =>
        useCapabilityUnavailableNotice({
          capabilities,
          draftSnapshot: "/browser task",
          modeActive,
          provider,
          scope
        }),
      {
        initialProps: {
          capabilities: [unavailableBrowser],
          modeActive: false,
          provider: "provider-a",
          scope: "session-a"
        }
      }
    );
    const show = (message: string) =>
      act(() => {
        rendered.result.current.showCapabilityUnavailableNotice({
          message,
          semantic: "browserUse",
          status: "notInstalled",
          origin: "descriptorAvailability"
        });
      });

    show("first reason");
    show("second reason");
    expect(rendered.result.current.capabilityUnavailableNotice).toBe(
      "second reason"
    );
    rendered.rerender({
      capabilities: [unavailableBrowser],
      modeActive: true,
      provider: "provider-a",
      scope: "session-a"
    });
    expect(rendered.result.current.capabilityUnavailableNotice).toBeNull();

    show("session reason");
    rendered.rerender({
      capabilities: [unavailableBrowser],
      modeActive: true,
      provider: "provider-a",
      scope: "session-b"
    });
    expect(rendered.result.current.capabilityUnavailableNotice).toBeNull();

    show("provider reason");
    rendered.rerender({
      capabilities: [unavailableBrowser],
      modeActive: true,
      provider: "provider-b",
      scope: "session-b"
    });
    expect(rendered.result.current.capabilityUnavailableNotice).toBeNull();

    show("recovery reason");
    rendered.rerender({
      capabilities: [{ ...unavailableBrowser, status: "available" }],
      modeActive: true,
      provider: "provider-b",
      scope: "session-b"
    });
    expect(rendered.result.current.capabilityUnavailableNotice).toBeNull();
  });

  it("keeps a Tutti backend-unavailable Sites notice visible while its descriptor remains available", () => {
    const sites = {
      ...unavailableBrowser,
      semantic: "sites" as const,
      status: "available" as const
    };
    const rendered = renderHook(
      ({ draftSnapshot, modeActive }) =>
        useCapabilityUnavailableNotice({
          capabilities: [sites],
          draftSnapshot,
          modeActive,
          provider: "provider-a",
          scope: "session-a"
        }),
      { initialProps: { draftSnapshot: "/sites build", modeActive: true } }
    );
    act(() => {
      rendered.result.current.showCapabilityUnavailableNotice({
        message: "Sites is unavailable in this mode",
        semantic: "sites",
        status: "unsupported",
        origin: "backendUnavailable"
      });
    });
    expect(rendered.result.current.capabilityUnavailableNotice).toBe(
      "Sites is unavailable in this mode"
    );
    rendered.rerender({ draftSnapshot: "/sites build", modeActive: true });
    expect(rendered.result.current.capabilityUnavailableNotice).toBe(
      "Sites is unavailable in this mode"
    );
    rendered.rerender({ draftSnapshot: "/sites build", modeActive: false });
    expect(rendered.result.current.capabilityUnavailableNotice).toBeNull();
  });

  it("renders the backend-unavailable alert without clearing the Sites draft", () => {
    const rendered = render(<CapabilityUnavailableNoticeHarness modeActive />);
    fireEvent.click(screen.getByRole("button", { name: "submit sites" }));
    expect(screen.getByRole("alert")).toHaveAttribute(
      "data-testid",
      "agent-gui-capability-unavailable-notice"
    );
    expect(screen.getByDisplayValue("/sites build")).toBeInTheDocument();

    rendered.rerender(<CapabilityUnavailableNoticeHarness modeActive />);
    expect(screen.getByRole("alert")).toBeInTheDocument();
    fireEvent.change(screen.getByDisplayValue("/sites build"), {
      target: { value: "/sites edited" }
    });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByDisplayValue("/sites edited")).toBeInTheDocument();

    rendered.rerender(<CapabilityUnavailableNoticeHarness modeActive />);
    fireEvent.click(screen.getByRole("button", { name: "submit sites" }));
    expect(screen.getByRole("alert")).toBeInTheDocument();
    rendered.rerender(
      <CapabilityUnavailableNoticeHarness modeActive={false} />
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});

function CapabilityUnavailableNoticeHarness(input: { modeActive: boolean }) {
  const [draft, setDraft] = useState("/sites build");
  const { capabilityUnavailableNotice, showCapabilityUnavailableNotice } =
    useCapabilityUnavailableNotice({
      capabilities: [
        {
          ...unavailableBrowser,
          semantic: "sites",
          status: "available"
        }
      ],
      draftSnapshot: draft,
      modeActive: input.modeActive,
      provider: "provider-a",
      scope: "session-a"
    });
  return (
    <div>
      {capabilityUnavailableNotice ? (
        <AgentChromeNotice
          tone="danger"
          role="alert"
          testId="agent-gui-capability-unavailable-notice"
          title={capabilityUnavailableNotice}
        />
      ) : null}
      <input
        aria-label="draft"
        onChange={(event) => setDraft(event.target.value)}
        value={draft}
      />
      <button
        onClick={() =>
          showCapabilityUnavailableNotice({
            message: "Sites is unavailable in this mode",
            semantic: "sites",
            status: "unsupported",
            origin: "backendUnavailable"
          })
        }
        type="button"
      >
        submit sites
      </button>
    </div>
  );
}
