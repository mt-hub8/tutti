import assert from "node:assert/strict";
import test from "node:test";
import {
  AGENT_ACTIVITY_INVALID_TURN_CAPABILITY_INVOCATION,
  normalizeAgentActivityTurnCapabilityInvocation,
  requireAgentActivityTurnCapabilityInvocation
} from "./turnCapabilityInvocation.ts";

test("normalizes only the narrow supported turn capability invocation", () => {
  assert.deepEqual(
    normalizeAgentActivityTurnCapabilityInvocation({ semantic: "browserUse" }),
    { semantic: "browserUse" }
  );
  assert.deepEqual(
    normalizeAgentActivityTurnCapabilityInvocation({
      semantic: "computerUse",
      consent: "explicitSession"
    }),
    { semantic: "computerUse", consent: "explicitSession" }
  );
});

test("rejects unknown and provider-shaped turn capability values", () => {
  assert.equal(
    normalizeAgentActivityTurnCapabilityInvocation({ semantic: "unknown" }),
    undefined
  );
  assert.equal(
    normalizeAgentActivityTurnCapabilityInvocation({
      semantic: "browserUse",
      pluginId: "not-allowed"
    }),
    undefined
  );
  assert.equal(normalizeAgentActivityTurnCapabilityInvocation(null), undefined);
  assert.equal(
    normalizeAgentActivityTurnCapabilityInvocation({
      semantic: "computerUse",
      consent: "forged"
    }),
    undefined
  );
});

test("preserves absent invocation but rejects a supplied invalid value", () => {
  assert.equal(
    requireAgentActivityTurnCapabilityInvocation(undefined),
    undefined
  );
  assert.throws(
    () =>
      requireAgentActivityTurnCapabilityInvocation({
        semantic: "browserUse",
        pluginId: "not-allowed"
      }),
    new RegExp(AGENT_ACTIVITY_INVALID_TURN_CAPABILITY_INVOCATION)
  );
});
