package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

func TestCodexTurnCapabilityLiveValidationFacts(t *testing.T) {
	for _, installPolicy := range []string{"AVAILABLE", "INSTALLED_BY_DEFAULT"} {
		fixture := json.RawMessage(`{"marketplaces":[{"plugins":[{"id":"browser@openai-bundled","installed":true,"enabled":true,"availability":"AVAILABLE","installPolicy":"` + installPolicy + `"}]}]}`)
		if !codexPluginAvailable(fixture, runtimeprep.CodexNativePluginBrowser) {
			t.Fatalf("available Browser plugin with installPolicy %s was rejected", installPolicy)
		}
	}
	for _, fixture := range []string{
		`{"marketplaces":[{"plugins":[{"id":"browser@openai-bundled","installed":false,"enabled":true,"availability":"AVAILABLE"}]}]}`,
		`{"marketplaces":[{"plugins":[{"id":"browser@openai-bundled","installed":true,"enabled":false,"availability":"AVAILABLE"}]}]}`,
		`{"marketplaces":[{"plugins":[{"id":"browser@openai-bundled","installed":true,"enabled":true,"availability":"UNKNOWN"}]}]}`,
		`{"marketplaces":[{"plugins":[{"id":"other","installed":true,"enabled":true,"availability":"AVAILABLE","message":"browser@openai-bundled"}]}]}`,
	} {
		if codexPluginAvailable(json.RawMessage(fixture), runtimeprep.CodexNativePluginBrowser) {
			t.Fatalf("unavailable Browser plugin was accepted: %s", fixture)
		}
	}
	for _, authStatus := range []string{"bearerToken", "oAuth", "unsupported"} {
		fixture := json.RawMessage(`{"data":[{"name":"node_repl","authStatus":"` + authStatus + `","tools":{"open":{}}}]}`)
		if !codexMCPAvailable(fixture, "node_repl") {
			t.Fatalf("runnable MCP auth status was rejected: %s", authStatus)
		}
	}
	for _, fixture := range []string{
		`{"data":[{"name":"node_repl","authStatus":"notLoggedIn","tools":{"open":{}}}]}`,
		`{"data":[{"name":"node_repl","authStatus":"bearerToken","tools":{}}]}`,
	} {
		if codexMCPAvailable(json.RawMessage(fixture), "node_repl") {
			t.Fatalf("unrunnable MCP was accepted: %s", fixture)
		}
	}
	if !codexSitesAppVisible(json.RawMessage(`{"data":[{"id":"sites","isEnabled":true,"isAccessible":true}]}`)) {
		t.Fatal("Sites app runtime was rejected")
	}
	for _, fixture := range []string{
		`{"data":[{"id":"sites","isEnabled":false,"isAccessible":true}]}`,
		`{"data":[{"id":"sites","isEnabled":true,"isAccessible":false}]}`,
		`{"data":[{"id":"other","isEnabled":true,"isAccessible":true,"message":"sites"}]}`,
	} {
		if codexSitesAppVisible(json.RawMessage(fixture)) {
			t.Fatalf("unavailable Sites app was accepted: %s", fixture)
		}
	}
}

func TestCodexPluginAvailableVersionClassifiesLivePluginState(t *testing.T) {
	plugin := runtimeprep.CodexNativePluginBrowser
	pluginResponse := func(fields string) json.RawMessage {
		return json.RawMessage(`{"marketplaces":[{"plugins":[{"id":"` + plugin + `",` + fields + `}]}]}`)
	}
	tests := []struct {
		name        string
		raw         json.RawMessage
		wantVersion string
		nextAction  string
		reasonCode  string
		unknown     bool
	}{
		{
			name:        "available install policy is ready",
			raw:         pluginResponse(`"installed":true,"enabled":true,"availability":"AVAILABLE","installPolicy":"AVAILABLE","packageVersion":"1.2.3"`),
			wantVersion: "1.2.3",
		},
		{
			name:        "installed by default policy is ready",
			raw:         pluginResponse(`"installed":true,"enabled":true,"availability":"AVAILABLE","installPolicy":"INSTALLED_BY_DEFAULT"`),
			wantVersion: plugin,
		},
		{
			name:       "not installed needs setup",
			raw:        pluginResponse(`"installed":false,"enabled":true,"availability":"AVAILABLE","installPolicy":"AVAILABLE"`),
			nextAction: "setup_required", reasonCode: "plugin_not_installed",
		},
		{
			name:       "disabled needs enable",
			raw:        pluginResponse(`"installed":true,"enabled":false,"availability":"AVAILABLE","installPolicy":"AVAILABLE"`),
			nextAction: "enable_required", reasonCode: "plugin_disabled",
		},
		{
			name:       "admin disabled is blocked",
			raw:        pluginResponse(`"installed":true,"enabled":true,"availability":"DISABLED_BY_ADMIN","installPolicy":"AVAILABLE"`),
			nextAction: "blocked", reasonCode: "plugin_blocked",
		},
		{
			name:       "not available policy is blocked",
			raw:        pluginResponse(`"installed":true,"enabled":true,"availability":"AVAILABLE","installPolicy":"NOT_AVAILABLE"`),
			nextAction: "blocked", reasonCode: "plugin_blocked",
		},
		{
			name:    "missing install policy is unknown",
			raw:     pluginResponse(`"installed":true,"enabled":true,"availability":"AVAILABLE"`),
			unknown: true,
		},
		{
			name:    "future install policy is unknown",
			raw:     pluginResponse(`"installed":true,"enabled":true,"availability":"AVAILABLE","installPolicy":"FUTURE"`),
			unknown: true,
		},
		{
			name:    "unknown availability is unknown",
			raw:     pluginResponse(`"installed":true,"enabled":true,"availability":"UNKNOWN","installPolicy":"AVAILABLE"`),
			unknown: true,
		},
		{
			name:    "malformed response is unknown",
			raw:     json.RawMessage(`{`),
			unknown: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			version, err := codexPluginAvailableVersionResult(test.raw, plugin)
			if test.wantVersion != "" {
				if err != nil || version != test.wantVersion {
					t.Fatalf("version = %q, error = %v; want %q", version, err, test.wantVersion)
				}
				return
			}
			if err == nil {
				t.Fatalf("error = nil, want classified failure")
			}
			if test.unknown {
				var unknown codexTurnCapabilityReadinessUnknownError
				if !errors.As(err, &unknown) {
					t.Fatalf("error = %T %v, want availability unknown", err, err)
				}
				return
			}
			var readiness codexPluginReadinessError
			if !errors.As(err, &readiness) || readiness.nextAction != test.nextAction || readiness.reasonCode != test.reasonCode {
				t.Fatalf("error = %#v, want %s/%s", err, test.nextAction, test.reasonCode)
			}
		})
	}
}

func TestCodexTurnCapabilityLiveReadinessUsesCurrentClientAndThread(t *testing.T) {
	transport := newScriptedAppServerTransport()
	adapter := NewCodexAppServerAdapter(transport)
	session := testAppServerSession()
	session.ProviderSessionID = "codex-thread-1"
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	conn := transport.conn
	configureLiveCapability(conn, "browser@openai-bundled", "node_repl", false)

	// Turn 1 is ordinary. Turn 2 then uses Browser without altering the client
	// generation or provider thread.
	if _, err := adapter.Exec(context.Background(), session, []PromptContentBlock{{Type: "text", Text: "ordinary"}}, "ordinary", "turn-1", nil, nil); err != nil {
		t.Fatal(err)
	}
	before := liveCapabilityIdentity(adapter, session)
	browser, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityBrowser, "")
	if err != nil || browser.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound || browser.Mention.Path != "plugin://browser@openai-bundled" {
		t.Fatalf("Browser readiness = %#v, %v", browser, err)
	}
	if _, err := adapter.Exec(context.Background(), session, []PromptContentBlock{{Type: "mention", Name: browser.Mention.Name, Path: browser.Mention.Path}, {Type: "text", Text: "browse"}}, "browse", "turn-2", nil, nil); err != nil {
		t.Fatal(err)
	}
	after := liveCapabilityIdentity(adapter, session)
	if before.client != after.client || before.threadID != after.threadID || transportStartCount(transport) != 1 || connClosed(conn) {
		t.Fatalf("live readiness changed runtime: before=%#v after=%#v starts=%d closed=%v", before, after, transportStartCount(transport), connClosed(conn))
	}
	if got := len(appServerRequestParamsList(t, conn, appServerMethodThreadResume)); got != 0 {
		t.Fatalf("thread/resume calls = %d, want 0", got)
	}
	if got := len(appServerRequestParamsList(t, conn, "config/mcpServer/reload")); got != 0 {
		t.Fatalf("ready Browser reload calls = %d, want 0", got)
	}
	if got := len(appServerRequestParamsList(t, conn, "skills/list")); got != 0 {
		t.Fatalf("ready Browser skills reload calls = %d, want 0", got)
	}
	if got := len(appServerRequestParamsList(t, conn, appServerMethodTurnStart)); got != 2 {
		t.Fatalf("turn/start calls = %d, want exactly 2", got)
	}
}

func TestCodexTuttiTurnCapabilityUsesCurrentManagedSkillOnly(t *testing.T) {
	transport := newScriptedAppServerTransport()
	adapter := NewCodexAppServerAdapter(transport)
	codexHome := t.TempDir()
	skillPath := writeManagedTuttiTurnSkill(t, codexHome, "browser-use", "tutti/browser-use")
	session := testAppServerSession()
	session.ProviderSessionID = "codex-thread-1"
	session.Env = []string{"CODEX_HOME=" + codexHome}
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	transport.conn.mu.Lock()
	transport.conn.capabilitySkills = []any{map[string]any{"name": "browser-use", "path": skillPath, "enabled": true}}
	transport.conn.mu.Unlock()
	configureLiveCapability(transport.conn, "browser@openai-bundled", "node_repl", false)

	// The exact same runtime first handles an ordinary turn, then one native
	// Browser turn, one Tutti Browser turn, and a native Browser turn again.
	// The only delivery difference is the structured input attached to each
	// turn; neither backend selection resumes nor replaces the runtime.
	if _, err := adapter.Exec(context.Background(), session, []PromptContentBlock{{Type: "text", Text: "ordinary"}}, "ordinary", "turn-ordinary", nil, nil); err != nil {
		t.Fatal(err)
	}
	before := liveCapabilityIdentity(adapter, session)
	native, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityBrowser, "")
	if err != nil || native.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound || native.PromptItem.Type != "mention" {
		t.Fatalf("native Browser readiness = %#v, %v", native, err)
	}
	if _, err := adapter.Exec(context.Background(), session, []PromptContentBlock{{Type: native.PromptItem.Type, Name: native.PromptItem.Name, Path: native.PromptItem.Path}, {Type: "text", Text: "native browse"}}, "/browser native browse", "turn-native-before", nil, nil); err != nil {
		t.Fatal(err)
	}
	result, err := adapter.EnsureLiveCodexTuttiTurnCapability(context.Background(), session, runtimeprep.CodexTurnCapabilitySemanticBrowserUse, "")
	if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound || result.PromptItem.Type != "skill" || result.PromptItem.Name != "browser-use" || result.PromptItem.Path != skillPath {
		t.Fatalf("Tutti Browser readiness = %#v, %v", result, err)
	}
	if _, err := adapter.Exec(context.Background(), session, []PromptContentBlock{{Type: "text", Text: "browse"}, {Type: result.PromptItem.Type, Name: result.PromptItem.Name, Path: result.PromptItem.Path}}, "/browser browse", "turn-tutti-browser", nil, nil); err != nil {
		t.Fatal(err)
	}
	nativeAgain, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityBrowser, "")
	if err != nil || nativeAgain.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound || nativeAgain.PromptItem.Type != "mention" {
		t.Fatalf("native Browser after Tutti = %#v, %v", nativeAgain, err)
	}
	if _, err := adapter.Exec(context.Background(), session, []PromptContentBlock{{Type: nativeAgain.PromptItem.Type, Name: nativeAgain.PromptItem.Name, Path: nativeAgain.PromptItem.Path}, {Type: "text", Text: "native browse again"}}, "/browser native browse again", "turn-native-after", nil, nil); err != nil {
		t.Fatal(err)
	}
	after := liveCapabilityIdentity(adapter, session)
	if before.client != after.client || before.threadID != after.threadID || transportStartCount(transport) != 1 || connClosed(transport.conn) {
		t.Fatalf("Tutti skill changed runtime: before=%#v after=%#v", before, after)
	}
	if got := len(appServerRequestParamsList(t, transport.conn, appServerMethodThreadResume)); got != 0 {
		t.Fatalf("thread/resume calls = %d, want 0", got)
	}
	turnStarts := appServerRequestParamsList(t, transport.conn, appServerMethodTurnStart)
	if got := len(turnStarts); got != 4 {
		t.Fatalf("turn/start calls = %d, want one per ordinary/native/Tutti/native turn", got)
	}
	for _, index := range []int{1, 3} {
		encoded, err := json.Marshal(turnStarts[index])
		if err != nil || !containsJSONStructuredBlock(encoded, "mention", "plugin://browser@openai-bundled") || containsJSONStructuredBlock(encoded, "skill", skillPath) {
			t.Fatalf("native turn %d did not exclusively carry the Browser plugin mention: %s, %v", index, encoded, err)
		}
	}
	encoded, err := json.Marshal(turnStarts[2])
	if err != nil || !containsJSONStructuredBlock(encoded, "skill", skillPath) || containsJSONStructuredBlock(encoded, "mention", "plugin://browser@openai-bundled") {
		t.Fatalf("Tutti turn did not exclusively carry the managed browser-use skill: %s, %v", encoded, err)
	}
}

func containsJSONStructuredBlock(encoded []byte, typ, path string) bool {
	return strings.Contains(string(encoded), `"type":"`+typ+`"`) && strings.Contains(string(encoded), `"path":"`+path+`"`)
}

func TestCodexTuttiTurnCapabilityForceReloadsThenFailsClosed(t *testing.T) {
	transport := newScriptedAppServerTransport()
	adapter := NewCodexAppServerAdapter(transport)
	codexHome := t.TempDir()
	session := testAppServerSession()
	session.ProviderSessionID = "codex-thread-1"
	session.Env = []string{"CODEX_HOME=" + codexHome}
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	result, err := adapter.EnsureLiveCodexTuttiTurnCapability(context.Background(), session, runtimeprep.CodexTurnCapabilitySemanticBrowserUse, "")
	if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityRejected || result.NextAction != "setup_required" {
		t.Fatalf("missing Tutti skill = %#v, %v", result, err)
	}
	if got := len(appServerRequestParamsList(t, transport.conn, "skills/list")); got != 2 {
		t.Fatalf("skills/list calls = %d, want initial + forceReload", got)
	}
	if got := len(appServerRequestParamsList(t, transport.conn, appServerMethodTurnStart)); got != 0 {
		t.Fatalf("turn/start calls = %d, want 0", got)
	}
}

func TestCodexTuttiTurnCapabilityForceReloadFindsManagedSkill(t *testing.T) {
	transport := newScriptedAppServerTransport()
	adapter := NewCodexAppServerAdapter(transport)
	codexHome := t.TempDir()
	skillPath := writeManagedTuttiTurnSkill(t, codexHome, "computer-use", "tutti/computer-use")
	session := testAppServerSession()
	session.ProviderSessionID = "codex-thread-1"
	session.Env = []string{"CODEX_HOME=" + codexHome}
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	transport.conn.mu.Lock()
	transport.conn.capabilitySkills = []any{map[string]any{"name": "computer-use", "path": skillPath, "enabled": true}}
	transport.conn.capabilitySkillsAfterForceReload = true
	transport.conn.mu.Unlock()
	denied, err := adapter.EnsureLiveCodexTuttiTurnCapability(context.Background(), session, runtimeprep.CodexTurnCapabilitySemanticComputerUse, "")
	if err != nil || denied.Disposition != runtimeprep.CodexTurnCapabilityRejected || denied.NextAction != "authorize_required" {
		t.Fatalf("unconsented Tutti Computer = %#v, %v", denied, err)
	}
	if got := len(appServerRequestParamsList(t, transport.conn, "skills/list")); got != 0 {
		t.Fatalf("unconsented Computer skills/list calls = %d, want 0", got)
	}
	result, err := adapter.EnsureLiveCodexTuttiTurnCapability(context.Background(), session, runtimeprep.CodexTurnCapabilitySemanticComputerUse, runtimeprep.CodexTurnCapabilityConsentExplicitSession)
	if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound || result.PromptItem.Type != "skill" || !result.Binding.Authorized {
		t.Fatalf("forceReload Tutti Computer = %#v, %v", result, err)
	}
	if got := len(appServerRequestParamsList(t, transport.conn, "skills/list")); got != 2 {
		t.Fatalf("skills/list calls = %d, want initial + forceReload", got)
	}
	if _, err := adapter.Exec(context.Background(), session, []PromptContentBlock{{Type: result.PromptItem.Type, Name: result.PromptItem.Name, Path: result.PromptItem.Path}, {Type: "text", Text: "control the computer"}}, "/computer control the computer", "turn-tutti-computer", nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := len(appServerRequestParamsList(t, transport.conn, appServerMethodTurnStart)); got != 1 {
		t.Fatalf("turn/start calls = %d, want exactly one consented Tutti Computer turn", got)
	}
	if got := len(appServerRequestParamsList(t, transport.conn, appServerMethodThreadResume)); got != 0 {
		t.Fatalf("thread/resume calls = %d, want 0", got)
	}
}

func writeManagedTuttiTurnSkill(t *testing.T, codexHome, name, managedID string) string {
	t.Helper()
	directory := filepath.Join(codexHome, "skills", name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(directory, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("---\nname: "+name+"\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, ".tutti-managed-skill"), []byte(managedID+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return skillPath
}

func TestCodexTurnCapabilityLiveReadinessSwitchesSemanticsWithoutReplacement(t *testing.T) {
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
		plugin   string
		mcp      string
		sites    bool
		consent  runtimeprep.CodexTurnCapabilityConsent
	}{
		{runtimeprep.CodexNativeCapabilityBrowser, "browser@openai-bundled", "node_repl", false, ""},
		{runtimeprep.CodexNativeCapabilitySites, "sites@openai-bundled", "", true, ""},
		{runtimeprep.CodexNativeCapabilityComputer, "computer-use@openai-bundled", "computer-use", false, runtimeprep.CodexTurnCapabilityConsentExplicitSession},
	} {
		configureLiveCapability(conn, test.plugin, test.mcp, test.sites)
		result, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, test.semantic, test.consent)
		if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound || result.Mention.Path != "plugin://"+test.plugin {
			t.Fatalf("%s readiness = %#v, %v", test.semantic, result, err)
		}
		if _, err := adapter.Exec(context.Background(), session, []PromptContentBlock{{Type: "mention", Name: result.Mention.Name, Path: result.Mention.Path}, {Type: "text", Text: test.semantic}}, test.semantic, "turn-"+test.semantic, nil, nil); err != nil {
			t.Fatalf("%s Exec: %v", test.semantic, err)
		}
	}
	after := liveCapabilityIdentity(adapter, session)
	if before.client != after.client || before.threadID != after.threadID || transportStartCount(transport) != 1 || connClosed(conn) {
		t.Fatalf("semantic switch replaced runtime: before=%#v after=%#v", before, after)
	}
	if got := len(appServerRequestParamsList(t, conn, appServerMethodTurnStart)); got != 3 {
		t.Fatalf("turn/start calls = %d, want one per Browser/Sites/Computer turn", got)
	}
}

func TestCodexTurnCapabilityLiveReadinessRejectsStaleOrIncompleteRuntime(t *testing.T) {
	tests := []struct {
		name     string
		semantic string
		plugin   string
		mcp      string
		sites    bool
		consent  runtimeprep.CodexTurnCapabilityConsent
		want     runtimeprep.CodexTurnCapabilityDisposition
	}{
		{"plugin missing", runtimeprep.CodexNativeCapabilityBrowser, "", "node_repl", false, "", runtimeprep.CodexTurnCapabilityUnknown},
		{"browser mcp mismatch", runtimeprep.CodexNativeCapabilityBrowser, "browser@openai-bundled", "other", false, "", runtimeprep.CodexTurnCapabilityRejected},
		{"browser tools empty", runtimeprep.CodexNativeCapabilityBrowser, "browser@openai-bundled", "", false, "", runtimeprep.CodexTurnCapabilityRejected},
		{"sites app missing", runtimeprep.CodexNativeCapabilitySites, "sites@openai-bundled", "", false, "", runtimeprep.CodexTurnCapabilityRejected},
		{"computer without consent", runtimeprep.CodexNativeCapabilityComputer, "computer-use@openai-bundled", "computer-use", false, "", runtimeprep.CodexTurnCapabilityRejected},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := newScriptedAppServerTransport()
			adapter := NewCodexAppServerAdapter(transport)
			session := testAppServerSession()
			session.ProviderSessionID = "codex-thread-1"
			if _, err := adapter.Start(context.Background(), session); err != nil {
				t.Fatal(err)
			}
			configureLiveCapability(transport.conn, test.plugin, test.mcp, test.sites)
			result, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, test.semantic, test.consent)
			if err != nil || result.Disposition != test.want {
				t.Fatalf("readiness = %#v, %v", result, err)
			}
			if transportStartCount(transport) != 1 || connClosed(transport.conn) {
				t.Fatalf("readiness rejection altered runtime: starts=%d closed=%v", transportStartCount(transport), connClosed(transport.conn))
			}
		})
	}
}

func TestCodexTurnCapabilityLiveReadinessProjectsSetupAndConsentWithoutDispatch(t *testing.T) {
	t.Run("plugin setup", func(t *testing.T) {
		transport := newScriptedAppServerTransport()
		adapter := NewCodexAppServerAdapter(transport)
		session := testAppServerSession()
		session.ProviderSessionID = "codex-thread-1"
		if _, err := adapter.Start(context.Background(), session); err != nil {
			t.Fatal(err)
		}
		configureLiveCapability(transport.conn, "", "node_repl", false)
		result, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityBrowser, "")
		if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityUnknown || result.NextAction != "retry" || result.ReasonCode != "availability_unknown" {
			t.Fatalf("result = %#v, error = %v", result, err)
		}
		if got := len(appServerRequestParamsList(t, transport.conn, appServerMethodTurnStart)); got != 0 {
			t.Fatalf("turn/start calls = %d, want 0", got)
		}
	})

	t.Run("computer consent", func(t *testing.T) {
		transport := newScriptedAppServerTransport()
		adapter := NewCodexAppServerAdapter(transport)
		session := testAppServerSession()
		session.ProviderSessionID = "codex-thread-1"
		if _, err := adapter.Start(context.Background(), session); err != nil {
			t.Fatal(err)
		}
		configureLiveCapability(transport.conn, "computer-use@openai-bundled", "computer-use", false)
		result, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityComputer, "")
		if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityRejected || result.NextAction != "authorize_required" || result.ReasonCode != "consent_required" {
			t.Fatalf("result = %#v, error = %v", result, err)
		}
		if got := len(appServerRequestParamsList(t, transport.conn, appServerMethodTurnStart)); got != 0 {
			t.Fatalf("turn/start calls = %d, want 0", got)
		}
	})
}

func TestCodexTurnCapabilityLiveReadinessCacheIsDiscardedWithClientGeneration(t *testing.T) {
	transport := &multiProcAppServerTransport{}
	transport.setConfigure(func(conn *scriptedAppServerConnection) {
		configureLiveCapability(conn, "browser@openai-bundled", "node_repl", false)
	})
	adapter := NewCodexAppServerAdapter(transport)
	session := testAppServerSession()
	session.ProviderSessionID = "codex-thread-1"
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	first, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityBrowser, "")
	if err != nil || first.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound {
		t.Fatalf("first readiness = %#v, %v", first, err)
	}
	old := transport.conn(0)
	if err := adapter.ReleaseLiveSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if !connClosed(old) {
		t.Fatal("old client was not closed on generation release")
	}
	transport.setConfigure(func(conn *scriptedAppServerConnection) {
		configureLiveCapability(conn, "", "node_repl", false)
	})
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	second, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityBrowser, "")
	if err != nil || second.Disposition != runtimeprep.CodexTurnCapabilityUnknown {
		t.Fatalf("stale generation readiness = %#v, %v", second, err)
	}
	if spawned, _ := transport.snapshot(); spawned != 2 {
		t.Fatalf("spawned clients = %d, want exactly 2 generations", spawned)
	}
}

func TestCodexTurnCapabilityLiveReadinessRechecksAppMCPFactsWithinGeneration(t *testing.T) {
	transport := newScriptedAppServerTransport()
	adapter := NewCodexAppServerAdapter(transport)
	session := testAppServerSession()
	session.ProviderSessionID = "codex-thread-1"
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	configureLiveCapability(transport.conn, "browser@openai-bundled", "node_repl", false)
	if result, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityBrowser, ""); err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound {
		t.Fatalf("initial readiness = %#v, %v", result, err)
	}
	configureLiveCapability(transport.conn, "browser@openai-bundled", "", false)
	if result, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityBrowser, ""); err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityRejected {
		t.Fatalf("stale MCP readiness = %#v, %v", result, err)
	}
	if transportStartCount(transport) != 1 || connClosed(transport.conn) {
		t.Fatal("failed live recheck altered the current runtime")
	}
}

func TestCodexTurnCapabilityLiveReadinessDoesNotRefreshDuringActiveTurnOrApproval(t *testing.T) {
	transport := newScriptedAppServerTransport()
	adapter := NewCodexAppServerAdapter(transport)
	session := testAppServerSession()
	session.ProviderSessionID = "codex-thread-1"
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	configureLiveCapability(transport.conn, "browser@openai-bundled", "node_repl", false)
	adapter.mu.Lock()
	live := adapter.sessions[session.AgentSessionID]
	live.activeTurn = &codexAppServerActiveTurn{turnID: "active-turn"}
	live.pendingRequests = map[string]*pendingInteractiveRequest{"approval-1": {}}
	adapter.mu.Unlock()

	result, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityBrowser, "")
	if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityRejected {
		t.Fatalf("readiness = %#v, %v", result, err)
	}
	if transportStartCount(transport) != 1 || connClosed(transport.conn) || len(appServerRequestParamsList(t, transport.conn, appServerMethodThreadResume)) != 0 {
		t.Fatal("active turn readiness attempted to refresh or replace the current runtime")
	}
}

func TestCodexTurnCapabilityLiveReadinessReturnsUnknownForReadFailure(t *testing.T) {
	transport := newScriptedAppServerTransport()
	adapter := NewCodexAppServerAdapter(transport)
	session := testAppServerSession()
	session.ProviderSessionID = "codex-thread-1"
	if _, err := adapter.Start(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	configureLiveCapability(transport.conn, "browser@openai-bundled", "node_repl", false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := adapter.EnsureLiveCodexTurnCapability(ctx, session, runtimeprep.CodexNativeCapabilityBrowser, "")
	if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityUnknown {
		t.Fatalf("cancelled readiness = %#v, %v", result, err)
	}
	if transportStartCount(transport) != 1 || connClosed(transport.conn) {
		t.Fatal("unknown readiness altered the current runtime")
	}
}

func TestCodexTurnCapabilityLiveRefreshMCPAndSitesWithoutRuntimeReplacement(t *testing.T) {
	tests := []struct {
		name          string
		semantic      string
		plugin        string
		mcp           string
		sites         bool
		consent       runtimeprep.CodexTurnCapabilityConsent
		mcpReload     bool
		sitesReload   bool
		wantMCPReload int
	}{
		{"browser", runtimeprep.CodexNativeCapabilityBrowser, "browser@openai-bundled", "node_repl", false, "", true, false, 1},
		{"computer", runtimeprep.CodexNativeCapabilityComputer, "computer-use@openai-bundled", "computer-use", false, runtimeprep.CodexTurnCapabilityConsentExplicitSession, true, false, 1},
		{"sites", runtimeprep.CodexNativeCapabilitySites, "sites@openai-bundled", "", false, "", false, true, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := newScriptedAppServerTransport()
			adapter := NewCodexAppServerAdapter(transport)
			session := testAppServerSession()
			session.ProviderSessionID = "codex-thread-1"
			if _, err := adapter.Start(context.Background(), session); err != nil {
				t.Fatal(err)
			}
			configureLiveCapability(transport.conn, test.plugin, "", test.sites)
			transport.conn.mu.Lock()
			transport.conn.capabilityMCPName = test.mcp
			transport.conn.capabilityReloadMakesMCPReady = test.mcpReload
			transport.conn.capabilityReloadMakesSitesReady = test.sitesReload
			transport.conn.mu.Unlock()
			before := liveCapabilityIdentity(adapter, session)
			result, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, test.semantic, test.consent)
			if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound {
				t.Fatalf("refresh = %#v, %v", result, err)
			}
			if _, err := adapter.Exec(context.Background(), session, []PromptContentBlock{{Type: "mention", Name: result.Mention.Name, Path: result.Mention.Path}, {Type: "text", Text: test.name}}, test.name, "turn-"+test.name, nil, nil); err != nil {
				t.Fatal(err)
			}
			after := liveCapabilityIdentity(adapter, session)
			if before.client != after.client || before.threadID != after.threadID || transportStartCount(transport) != 1 || connClosed(transport.conn) {
				t.Fatal("refresh replaced the current runtime")
			}
			if got := len(appServerRequestParamsList(t, transport.conn, "config/mcpServer/reload")); got != test.wantMCPReload {
				t.Fatalf("MCP reload calls = %d, want %d", got, test.wantMCPReload)
			}
			if got := len(appServerRequestParamsList(t, transport.conn, "skills/list")); got != 1 {
				t.Fatalf("skills/list reload calls = %d, want 1", got)
			}
			if got := len(appServerRequestParamsList(t, transport.conn, appServerMethodThreadResume)); got != 0 {
				t.Fatalf("thread/resume calls = %d, want 0", got)
			}
			if got := len(appServerRequestParamsList(t, transport.conn, appServerMethodTurnStart)); got != 1 {
				t.Fatalf("turn/start calls = %d, want one", got)
			}
		})
	}
}

func TestCodexTurnCapabilityLiveRefreshSingleFlightAndFailClosed(t *testing.T) {
	t.Run("single flight", func(t *testing.T) {
		transport := newScriptedAppServerTransport()
		adapter := NewCodexAppServerAdapter(transport)
		session := testAppServerSession()
		session.ProviderSessionID = "codex-thread-1"
		if _, err := adapter.Start(context.Background(), session); err != nil {
			t.Fatal(err)
		}
		configureLiveCapability(transport.conn, "browser@openai-bundled", "", false)
		entered, release := make(chan struct{}, 1), make(chan struct{})
		transport.conn.mu.Lock()
		transport.conn.capabilityMCPName = "node_repl"
		transport.conn.capabilityReloadMakesMCPReady = true
		transport.conn.capabilityReloadEntered = entered
		transport.conn.capabilityReloadRelease = release
		transport.conn.mu.Unlock()
		results := make(chan CodexTurnCapabilityEnsureResult, 2)
		go func() {
			result, _ := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityBrowser, "")
			results <- result
		}()
		<-entered
		go func() {
			result, _ := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityBrowser, "")
			results <- result
		}()
		close(release)
		for range 2 {
			if result := <-results; result.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound {
				t.Fatalf("refresh result = %#v", result)
			}
		}
		if got := len(appServerRequestParamsList(t, transport.conn, "config/mcpServer/reload")); got != 1 {
			t.Fatalf("reload calls = %d, want 1", got)
		}
	})
	t.Run("unsupported reload", func(t *testing.T) {
		transport := newScriptedAppServerTransport()
		adapter := NewCodexAppServerAdapter(transport)
		session := testAppServerSession()
		session.ProviderSessionID = "codex-thread-1"
		if _, err := adapter.Start(context.Background(), session); err != nil {
			t.Fatal(err)
		}
		configureLiveCapability(transport.conn, "browser@openai-bundled", "", false)
		transport.conn.mu.Lock()
		transport.conn.capabilityMCPName = "node_repl"
		transport.conn.capabilityReloadError = true
		transport.conn.mu.Unlock()
		result, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityBrowser, "")
		if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityUnknown {
			t.Fatalf("unsupported reload = %#v, %v", result, err)
		}
		if got := len(appServerRequestParamsList(t, transport.conn, appServerMethodTurnStart)); got != 0 {
			t.Fatalf("turn/start calls = %d, want 0", got)
		}
	})
	t.Run("version gate", func(t *testing.T) {
		transport := newScriptedAppServerTransport()
		transport.conn.userAgent = "codex/0.136.0"
		adapter := NewCodexAppServerAdapter(transport)
		session := testAppServerSession()
		session.ProviderSessionID = "codex-thread-1"
		if _, err := adapter.Start(context.Background(), session); err != nil {
			t.Fatal(err)
		}
		configureLiveCapability(transport.conn, "browser@openai-bundled", "", false)
		transport.conn.mu.Lock()
		transport.conn.capabilityMCPName = "node_repl"
		transport.conn.mu.Unlock()
		result, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityBrowser, "")
		if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityRejected {
			t.Fatalf("version-gated refresh = %#v, %v", result, err)
		}
		if got := len(appServerRequestParamsList(t, transport.conn, "config/mcpServer/reload")); got != 0 {
			t.Fatalf("reload calls = %d, want 0", got)
		}
	})
	t.Run("admin disabled never reloads or enables", func(t *testing.T) {
		transport := newScriptedAppServerTransport()
		adapter := NewCodexAppServerAdapter(transport)
		session := testAppServerSession()
		session.ProviderSessionID = "codex-thread-1"
		if _, err := adapter.Start(context.Background(), session); err != nil {
			t.Fatal(err)
		}
		configureLiveCapability(transport.conn, "browser@openai-bundled", "node_repl", false)
		transport.conn.mu.Lock()
		transport.conn.capabilityPluginAvailability = "DISABLED_BY_ADMIN"
		transport.conn.mu.Unlock()
		result, err := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityBrowser, "")
		if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityRejected {
			t.Fatalf("admin-disabled readiness = %#v, %v", result, err)
		}
		if got := len(appServerRequestParamsList(t, transport.conn, "config/mcpServer/reload")); got != 0 {
			t.Fatalf("reload calls = %d, want 0", got)
		}
		if got := len(appServerRequestParamsList(t, transport.conn, "experimentalFeature/enablement/set")); got != 0 {
			t.Fatalf("enablement calls = %d, want 0", got)
		}
	})
	t.Run("generation change invalidates inflight refresh", func(t *testing.T) {
		transport := newScriptedAppServerTransport()
		adapter := NewCodexAppServerAdapter(transport)
		session := testAppServerSession()
		session.ProviderSessionID = "codex-thread-1"
		if _, err := adapter.Start(context.Background(), session); err != nil {
			t.Fatal(err)
		}
		configureLiveCapability(transport.conn, "browser@openai-bundled", "", false)
		entered, release := make(chan struct{}, 1), make(chan struct{})
		transport.conn.mu.Lock()
		transport.conn.capabilityMCPName = "node_repl"
		transport.conn.capabilityReloadMakesMCPReady = true
		transport.conn.capabilityReloadEntered = entered
		transport.conn.capabilityReloadRelease = release
		transport.conn.mu.Unlock()
		resultCh := make(chan CodexTurnCapabilityEnsureResult, 1)
		go func() {
			result, _ := adapter.EnsureLiveCodexTurnCapability(context.Background(), session, runtimeprep.CodexNativeCapabilityBrowser, "")
			resultCh <- result
		}()
		<-entered
		if err := adapter.ReleaseLiveSession(context.Background(), session); err != nil {
			t.Fatal(err)
		}
		close(release)
		if result := <-resultCh; result.Disposition != runtimeprep.CodexTurnCapabilityUnknown {
			t.Fatalf("generation-change refresh = %#v", result)
		}
	})
}

func configureLiveCapability(conn *scriptedAppServerConnection, pluginID, mcpName string, sites bool) {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	conn.capabilityPluginAvailable = pluginID != ""
	conn.capabilityPluginID = pluginID
	conn.capabilityMCPTools = mcpName != ""
	conn.capabilityMCPName = mcpName
	conn.capabilitySitesApp = sites
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
