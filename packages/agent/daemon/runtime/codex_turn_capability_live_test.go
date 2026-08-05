package agentruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

func TestCodexTurnCapabilityLiveExecutionUsesCurrentClientAndThread(t *testing.T) {
	transport := newScriptedAppServerTransport()
	adapter := NewCodexAppServerAdapter(transport)
	session := testAppServerSession()
	session.ProviderSessionID = "codex-thread-1"
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	conn := transport.conn

	// A normal first Turn must not make the current Runtime ineligible for a
	// later plugin Turn.
	if _, err := adapter.Exec(context.Background(), session, []PromptContentBlock{{Type: "text", Text: "ordinary"}}, "ordinary", "turn-1", nil, nil); err != nil {
		t.Fatal(err)
	}
	before := liveCapabilityIdentity(adapter, session)
	result, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityBrowser, "")
	if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound {
		t.Fatalf("Browser admission = %#v, %v", result, err)
	}
	if result.PromptItem != (runtimeprep.CodexTurnCapabilityPromptItem{Type: "mention", Name: runtimeprep.CodexNativePluginBrowser, Path: "plugin://" + runtimeprep.CodexNativePluginBrowser}) {
		t.Fatalf("Browser prompt item = %#v", result.PromptItem)
	}
	if _, err := adapter.Exec(context.Background(), session, []PromptContentBlock{
		{Type: result.PromptItem.Type, Name: result.PromptItem.Name, Path: result.PromptItem.Path},
		{Type: "text", Text: "browse"},
	}, "browse", "turn-2", nil, nil); err != nil {
		t.Fatal(err)
	}
	after := liveCapabilityIdentity(adapter, session)
	if before.client != after.client || before.threadID != after.threadID || transportStartCount(transport) != 1 || connClosed(conn) {
		t.Fatalf("plugin turn replaced current runtime: before=%#v after=%#v starts=%d closed=%v", before, after, transportStartCount(transport), connClosed(conn))
	}
	if got := len(appServerRequestParamsList(t, conn, appServerMethodThreadResume)); got != 0 {
		t.Fatalf("thread/resume calls = %d, want 0", got)
	}
	assertNoCapabilityReadinessProtocolCalls(t, conn)
	if got := len(appServerRequestParamsList(t, conn, appServerMethodTurnStart)); got != 2 {
		t.Fatalf("turn/start calls = %d, want 2", got)
	}
}

func TestCodexTurnCapabilityLiveExecutionSwitchesOfficialPluginsWithoutReplacement(t *testing.T) {
	transport := newScriptedAppServerTransport()
	adapter := NewCodexAppServerAdapter(transport)
	session := testAppServerSession()
	session.ProviderSessionID = "codex-thread-1"
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	conn := transport.conn
	before := liveCapabilityIdentity(adapter, session)

	for _, test := range []struct {
		semantic string
		pluginID string
		consent  runtimeprep.CodexTurnCapabilityConsent
	}{
		{runtimeprep.CodexNativeCapabilityBrowser, runtimeprep.CodexNativePluginBrowser, ""},
		{runtimeprep.CodexNativeCapabilitySites, runtimeprep.CodexNativePluginSites, ""},
		{runtimeprep.CodexNativeCapabilityComputer, runtimeprep.CodexNativePluginComputerUse, runtimeprep.CodexTurnCapabilityConsentExplicitSession},
	} {
		result, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, test.semantic, test.consent)
		if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound || result.Mention.Path != "plugin://"+test.pluginID {
			t.Fatalf("%s admission = %#v, %v", test.semantic, result, err)
		}
		if _, err := adapter.Exec(context.Background(), session, []PromptContentBlock{
			{Type: result.PromptItem.Type, Name: result.PromptItem.Name, Path: result.PromptItem.Path},
			{Type: "text", Text: test.semantic},
		}, test.semantic, "turn-"+test.semantic, nil, nil); err != nil {
			t.Fatalf("%s Exec: %v", test.semantic, err)
		}
	}

	after := liveCapabilityIdentity(adapter, session)
	if before.client != after.client || before.threadID != after.threadID || transportStartCount(transport) != 1 || connClosed(conn) {
		t.Fatalf("plugin switch replaced current runtime: before=%#v after=%#v", before, after)
	}
	assertNoCapabilityReadinessProtocolCalls(t, conn)
	turns := appServerRequestParamsList(t, conn, appServerMethodTurnStart)
	if len(turns) != 3 {
		t.Fatalf("turn/start calls = %d, want one per plugin turn", len(turns))
	}
	for index, pluginID := range []string{runtimeprep.CodexNativePluginBrowser, runtimeprep.CodexNativePluginSites, runtimeprep.CodexNativePluginComputerUse} {
		encoded, err := json.Marshal(turns[index])
		if err != nil || !strings.Contains(string(encoded), `"type":"mention"`) || !strings.Contains(string(encoded), `"path":"plugin://`+pluginID+`"`) || strings.Contains(string(encoded), `"type":"skill"`) {
			t.Fatalf("turn %d did not carry only the official plugin mention: %s, %v", index, encoded, err)
		}
	}
}

func TestCodexTurnCapabilityLiveExecutionRequiresComputerConsentBeforeDispatch(t *testing.T) {
	transport := newScriptedAppServerTransport()
	adapter := NewCodexAppServerAdapter(transport)
	session := testAppServerSession()
	session.ProviderSessionID = "codex-thread-1"
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatal(err)
	}

	result, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityComputer, "")
	if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityRejected || result.NextAction != "authorize_required" || result.ReasonCode != "consent_required" {
		t.Fatalf("unconsented Computer admission = %#v, %v", result, err)
	}
	if got := len(appServerRequestParamsList(t, transport.conn, appServerMethodTurnStart)); got != 0 {
		t.Fatalf("unconsented Computer dispatched %d turns", got)
	}

	result, err = adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityComputer, runtimeprep.CodexTurnCapabilityConsentExplicitSession)
	if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound || !result.Binding.Authorized {
		t.Fatalf("consented Computer admission = %#v, %v", result, err)
	}
	if _, err := adapter.Exec(context.Background(), session, []PromptContentBlock{
		{Type: result.PromptItem.Type, Name: result.PromptItem.Name, Path: result.PromptItem.Path},
		{Type: "text", Text: "open settings"},
	}, "open settings", "turn-computer", nil, nil); err != nil {
		t.Fatal(err)
	}
	assertNoCapabilityReadinessProtocolCalls(t, transport.conn)
	if got := len(appServerRequestParamsList(t, transport.conn, appServerMethodTurnStart)); got != 1 {
		t.Fatalf("turn/start calls = %d, want 1", got)
	}
}

func TestCodexTurnCapabilityLiveExecutionRejectsMissingCurrentRuntime(t *testing.T) {
	adapter := NewCodexAppServerAdapter(newScriptedAppServerTransport())
	session := testAppServerSession()
	session.ProviderSessionID = "codex-thread-1"
	result, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityBrowser, "")
	if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityRejected || result.NextAction != "retry" || result.ReasonCode != "runtime_unavailable" {
		t.Fatalf("missing runtime admission = %#v, %v", result, err)
	}
}

func assertNoCapabilityReadinessProtocolCalls(t *testing.T, conn *scriptedAppServerConnection) {
	t.Helper()
	for _, method := range []string{"plugin/list", "mcpServerStatus/list", "app/list", "config/mcpServer/reload", "skills/list"} {
		if got := len(appServerRequestParamsList(t, conn, method)); got != 0 {
			t.Fatalf("%s calls = %d, want no execution hard precheck", method, got)
		}
	}
}

type liveCapabilitySessionIdentity struct {
	client   *codexAppServerClient
	threadID string
}

func liveCapabilityIdentity(adapter *CodexAppServerAdapter, session Session) liveCapabilitySessionIdentity {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	live := adapter.sessions[session.AgentSessionID]
	return liveCapabilitySessionIdentity{client: live.client, threadID: live.threadID}
}

func transportStartCount(transport *scriptedAppServerTransport) int {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return len(transport.specs)
}
