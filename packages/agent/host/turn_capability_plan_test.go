package agenthost

import "testing"

func TestDecodeTurnCapabilityPlanRejectsUnknownOrSensitiveFields(t *testing.T) {
	for _, encoded := range []string{
		`{"Key":"native plan"}`,
		`{"Key":"codex_native","task":"private user task"}`,
		`{"Key":"codex_native","pluginPath":"plugin://private"}`,
		`{"Key":"codex_native","consent":"explicitSession"}`,
	} {
		if _, err := decodeTurnCapabilityPlan(encoded); err == nil {
			t.Fatalf("decodeTurnCapabilityPlan(%s) unexpectedly succeeded", encoded)
		}
	}
}

func TestDecodeTurnCapabilityPlanAcceptsMinimalOpaqueSnapshot(t *testing.T) {
	plan, err := decodeTurnCapabilityPlan(`{"Key":"codex_native"}`)
	if err != nil || plan.Key != "codex_native" {
		t.Fatalf("decodeTurnCapabilityPlan() = %#v, %v", plan, err)
	}
}
