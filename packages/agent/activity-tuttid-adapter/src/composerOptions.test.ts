import assert from "node:assert/strict";
import test from "node:test";
import type { AgentProviderComposerOptionsResponse } from "@tutti-os/client-tuttid-ts";
import { agentActivityComposerOptionsFromTuttidResult } from "./composerOptions.ts";

test("maps daemon composer options into the canonical activity contract", () => {
  const options = agentActivityComposerOptionsFromTuttidResult("codex", {
    behavior: {
      collapseModelOptionsToLatest: false,
      modelOptionsAuthoritative: true,
      planModeExclusiveWithPermissionMode: false,
      prewarmDraftSession: false,
      refreshModelOptionsAfterSettings: true
    },
    effectiveSettings: { model: "gpt-5" },
    modelConfig: {
      configurable: true,
      effectiveValue: "claude-haiku-4-5-20251001",
      options: [{ id: "gpt-5", label: "GPT-5", value: "gpt-5" }]
    },
    permissionConfig: { configurable: false, modes: [] },
    reasoningConfig: { configurable: false, options: [] },
    reasoningOptionsByModel: {},
    runtimeContext: {},
    commands: [],
    skills: [
      {
        name: "sites:sites-building",
        trigger: "$sites:sites-building",
        sourceKind: "plugin"
      }
    ],
    capabilityCatalog: [],
    reservedTurnCapabilityAliases: [
      {
        alias: "/browser",
        name: "browser",
        label: "Browser",
        status: "available",
        invocation: "promptItem",
        invocationScope: "turn",
        semantic: "browserUse",
        nextAction: "use"
      }
    ],
    provider: "codex"
  } satisfies AgentProviderComposerOptionsResponse);

  assert.equal(options.provider, "codex");
  assert.equal(options.modelConfigurable, true);
  assert.equal(options.effectiveModel, "claude-haiku-4-5-20251001");
  assert.deepEqual(options.models, [{ label: "GPT-5", value: "gpt-5" }]);
  assert.equal(options.effectiveSettings?.model, "gpt-5");
  assert.deepEqual(options.capabilityCatalog, []);
  assert.deepEqual(options.capabilityPresentations, [
    {
      name: "browser",
      label: "Browser",
      trigger: "/browser",
      status: "available",
      invocation: "promptItem",
      invocationScope: "turn",
      semantic: "browserUse",
      nextAction: "use"
    }
  ]);
});

test("maps explicit session consent as presentation policy rather than session state", () => {
  const options = agentActivityComposerOptionsFromTuttidResult("codex", {
    behavior: {
      collapseModelOptionsToLatest: false,
      modelOptionsAuthoritative: true,
      planModeExclusiveWithPermissionMode: false,
      prewarmDraftSession: false,
      refreshModelOptionsAfterSettings: false
    },
    capabilityCatalog: [],
    reservedTurnCapabilityAliases: [
      {
        alias: "/computer",
        name: "computer",
        label: "Computer",
        status: "available",
        invocation: "promptItem",
        semantic: "computerUse",
        invocationScope: "turn",
        consentRequirement: "explicitSession"
      }
    ],
    commands: [],
    effectiveSettings: {},
    modelConfig: { configurable: false, options: [] },
    permissionConfig: { configurable: false, modes: [] },
    provider: "codex",
    reasoningConfig: { configurable: false, options: [] },
    reasoningOptionsByModel: {},
    runtimeContext: {},
    skills: []
  } as unknown as AgentProviderComposerOptionsResponse);

  assert.deepEqual(options.capabilityPresentations, [
    {
      name: "computer",
      label: "Computer",
      trigger: "/computer",
      status: "available",
      invocation: "promptItem",
      invocationScope: "turn",
      semantic: "computerUse",
      consentRequirement: "explicitSession"
    }
  ]);
});

test("does not turn a legacy plugin catalog row into a reserved slash alias", () => {
  const options = agentActivityComposerOptionsFromTuttidResult("codex", {
    behavior: {
      collapseModelOptionsToLatest: false,
      modelOptionsAuthoritative: true,
      planModeExclusiveWithPermissionMode: false,
      prewarmDraftSession: false,
      refreshModelOptionsAfterSettings: false
    },
    capabilityCatalog: [
      {
        id: "plugin:browser@openai-bundled",
        kind: "plugin",
        name: "browser",
        label: "Browser",
        status: "available",
        invocation: "promptItem",
        trigger: "$browser",
        semantic: "browserUse"
      }
    ],
    commands: [],
    effectiveSettings: {},
    modelConfig: { configurable: false, options: [] },
    permissionConfig: { configurable: false, modes: [] },
    provider: "codex",
    reasoningConfig: { configurable: false, options: [] },
    reasoningOptionsByModel: {},
    runtimeContext: {},
    skills: []
  } as unknown as AgentProviderComposerOptionsResponse);

  assert.deepEqual(options.capabilityPresentations, []);
  assert.equal(options.capabilityCatalog?.[0]?.trigger, "$browser");
});

test("keeps fallback slash commands when effects are absent", () => {
  const response = {
    behavior: {
      collapseModelOptionsToLatest: false,
      modelOptionsAuthoritative: false,
      planModeExclusiveWithPermissionMode: false,
      prewarmDraftSession: false,
      refreshModelOptionsAfterSettings: false
    },
    capabilityCatalog: [],
    commands: [],
    effectiveSettings: {},
    modelConfig: { configurable: false, options: [] },
    permissionConfig: { configurable: false, modes: [] },
    provider: "acp:hermes",
    reasoningConfig: { configurable: false, options: [] },
    reasoningOptionsByModel: {},
    runtimeContext: {},
    skills: [],
    slashCommandPolicy: {
      fallbackCommands: ["compact", "help"]
    }
  } as unknown as AgentProviderComposerOptionsResponse;
  const options = agentActivityComposerOptionsFromTuttidResult(
    "acp:hermes",
    response
  );

  assert.deepEqual(options.slashCommandPolicy, {
    fallbackCommands: ["compact", "help"],
    commandEffects: []
  });
  assert.equal(options.behavior.nativePluginCatalogAuthoritative, undefined);
});
