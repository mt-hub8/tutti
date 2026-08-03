package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	agentsessionstore "github.com/tutti-os/tutti/packages/agent/daemon/activity"
	activityshared "github.com/tutti-os/tutti/packages/agent/daemon/activity/events"
	"github.com/tutti-os/tutti/packages/agent/runtimeprep"
)

func TestEnsureCodexTurnCapabilityAccumulatesBindingsAcrossSemantics(t *testing.T) {
	t.Parallel()
	codexHome := turnCapabilityRuntimeHome(t)
	adapter := &turnCapabilityRuntimeAdapter{provider: ProviderCodex, live: true}
	controller := NewController([]Adapter{adapter}, nil)
	controller.store(turnCapabilityRuntimeSession(codexHome, ProviderCodex))

	browser := ensureRuntimeCodexCapability(t, controller, runtimeprep.CodexTurnCapabilitySemanticBrowserUse, "turn-browser", "submit-browser")
	if browser.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound || browser.Binding.Semantic != runtimeprep.CodexNativeCapabilityBrowser || adapter.resumeCalls() != 0 {
		t.Fatalf("browser = %#v, resumes = %d", browser, adapter.resumeCalls())
	}
	sites := ensureRuntimeCodexCapability(t, controller, runtimeprep.CodexTurnCapabilitySemanticSites, "turn-sites", "submit-sites")
	if sites.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound || sites.Binding.Semantic != runtimeprep.CodexNativeCapabilitySites || adapter.resumeCalls() != 0 {
		t.Fatalf("sites = %#v, resumes = %d", sites, adapter.resumeCalls())
	}
	repeatedBrowser := ensureRuntimeCodexCapability(t, controller, runtimeprep.CodexTurnCapabilitySemanticBrowserUse, "turn-browser-repeat", "submit-browser-repeat")
	if repeatedBrowser.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound || adapter.resumeCalls() != 0 || adapter.ensureCalls() != 3 {
		t.Fatalf("repeated browser = %#v, resumes = %d ensures=%d", repeatedBrowser, adapter.resumeCalls(), adapter.ensureCalls())
	}
}

func TestEnsureCodexTurnCapabilityKeepsNativeAndTuttiPlansIsolated(t *testing.T) {
	t.Parallel()
	codexHome := turnCapabilityRuntimeHome(t)
	adapter := &turnCapabilityRuntimeAdapter{provider: ProviderCodex, live: true}
	controller := NewController([]Adapter{adapter}, nil)
	controller.store(turnCapabilityRuntimeSession(codexHome, ProviderCodex))

	native, err := controller.EnsureCodexTurnCapability(context.Background(), CodexTurnCapabilityEnsureInput{
		RoomID: "room-1", AgentSessionID: "session-1", TurnID: "turn-native", ClientSubmitID: "submit-native",
		Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse, PlanKey: "codex_native",
	})
	if err != nil || native.Mention.Path != "plugin://"+runtimeprep.CodexNativePluginBrowser || native.PromptItem.Type != "mention" {
		t.Fatalf("native Browser = %#v, %v", native, err)
	}
	tutti, err := controller.EnsureCodexTurnCapability(context.Background(), CodexTurnCapabilityEnsureInput{
		RoomID: "room-1", AgentSessionID: "session-1", TurnID: "turn-tutti", ClientSubmitID: "submit-tutti",
		Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse, PlanKey: "tutti",
	})
	if err != nil || tutti.PromptItem.Type != "skill" || tutti.PromptItem.Name != "browser-use" || tutti.Mention.Path != "" {
		t.Fatalf("Tutti Browser = %#v, %v", tutti, err)
	}
	nativeAgain, err := controller.EnsureCodexTurnCapability(context.Background(), CodexTurnCapabilityEnsureInput{
		RoomID: "room-1", AgentSessionID: "session-1", TurnID: "turn-native-again", ClientSubmitID: "submit-native-again",
		Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse, PlanKey: "codex_native",
	})
	if err != nil || nativeAgain.PromptItem.Type != "mention" || nativeAgain.Mention.Path != "plugin://"+runtimeprep.CodexNativePluginBrowser || adapter.resumeCalls() != 0 {
		t.Fatalf("native Browser after Tutti = %#v, %v, resumes=%d", nativeAgain, err, adapter.resumeCalls())
	}
}

func TestEnsureCodexTurnCapabilityRebindFailureKeepsOldRuntimeUsable(t *testing.T) {
	t.Parallel()
	codexHome := turnCapabilityRuntimeHome(t)
	rebindErr := errors.New("replacement resume failed")
	adapter := &turnCapabilityRuntimeAdapter{provider: ProviderCodex, live: true, resumeErr: rebindErr, execDone: make(chan struct{}, 1)}
	controller := NewController([]Adapter{adapter}, nil)
	controller.store(turnCapabilityRuntimeSession(codexHome, ProviderCodex))

	result, err := controller.EnsureCodexTurnCapability(context.Background(), CodexTurnCapabilityEnsureInput{
		RoomID: "room-1", AgentSessionID: "session-1", TurnID: "turn-1", ClientSubmitID: "submit-1", Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse, PlanKey: "codex_native",
	})
	if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound || adapter.resumeCalls() != 0 || !adapter.isLive() {
		t.Fatalf("ensure = %#v, %v; live = %v", result, err, adapter.isLive())
	}
	if _, err := controller.Exec(context.Background(), ExecInput{
		RoomID: "room-1", AgentSessionID: "session-1", TurnID: "ordinary-turn", ClientSubmitID: "ordinary-submit",
		CanonicalSubmitOccurredAtUnixMS: 1,
		Content:                         []PromptContentBlock{{Type: "text", Text: "ordinary message"}},
	}); err != nil {
		t.Fatalf("ordinary Exec after failed rebind: %v", err)
	}
	<-adapter.execDone
}

func TestEnsureCodexTurnCapabilityRejectsNonCodexUnknownAndComputerWithoutEffects(t *testing.T) {
	t.Parallel()
	codexHome := turnCapabilityRuntimeHome(t)
	nonCodexAdapter := &recordingStartAdapter{provider: "other"}
	controller := NewController([]Adapter{nonCodexAdapter}, nil)
	controller.store(turnCapabilityRuntimeSession(codexHome, "other"))

	result, err := controller.EnsureCodexTurnCapability(context.Background(), CodexTurnCapabilityEnsureInput{
		RoomID: "room-1", AgentSessionID: "session-1", TurnID: "turn-1", ClientSubmitID: "submit-1", Semantic: runtimeprep.CodexTurnCapabilitySemanticBrowserUse,
	})
	if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityRejected {
		t.Fatalf("non-Codex ensure = %#v, %v", result, err)
	}

	adapter := &turnCapabilityRuntimeAdapter{provider: ProviderCodex, live: true}
	controller = NewController([]Adapter{adapter}, nil)
	controller.store(turnCapabilityRuntimeSession(codexHome, ProviderCodex))
	before, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, semantic := range []string{"unknown", runtimeprep.CodexTurnCapabilitySemanticComputerUse} {
		result, err = controller.EnsureCodexTurnCapability(context.Background(), CodexTurnCapabilityEnsureInput{
			RoomID: "room-1", AgentSessionID: "session-1", TurnID: "turn-" + semantic, ClientSubmitID: "submit-" + semantic, Semantic: semantic, PlanKey: "codex_native",
		})
		if err != nil || result.Disposition != runtimeprep.CodexTurnCapabilityRejected {
			t.Fatalf("%s ensure = %#v, %v", semantic, result, err)
		}
		if semantic == runtimeprep.CodexTurnCapabilitySemanticComputerUse && result.Reason != "computer use requires explicit session-scoped authorization" {
			t.Fatalf("computer semantic did not reach native authorization gate: %#v", result)
		}
	}
	after, _ := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if string(before) != string(after) || adapter.resumeCalls() != 0 {
		t.Fatalf("rejected capability changed config or rebound: before=%q after=%q rebinds=%d", before, after, adapter.resumeCalls())
	}
}

func TestEnsureCodexTurnCapabilityRestoresJSONBindingsWithoutRebind(t *testing.T) {
	t.Parallel()
	codexHome := turnCapabilityRuntimeHome(t)
	adapter := &turnCapabilityRuntimeAdapter{provider: ProviderCodex, live: true}
	controller := NewController([]Adapter{adapter}, nil)
	controller.store(turnCapabilityRuntimeSession(codexHome, ProviderCodex))
	_ = ensureRuntimeCodexCapability(t, controller, runtimeprep.CodexTurnCapabilitySemanticBrowserUse, "turn-browser", "submit-browser")
	_ = ensureRuntimeCodexCapability(t, controller, runtimeprep.CodexTurnCapabilitySemanticSites, "turn-sites", "submit-sites")
	if adapter.resumeCalls() != 0 {
		t.Fatalf("initial resumes = %d", adapter.resumeCalls())
	}
	session, found := controller.Session("room-1", "session-1")
	if !found {
		t.Fatal("session missing before JSON round-trip")
	}
	encoded, err := json.Marshal(session.RuntimeContext)
	if err != nil {
		t.Fatalf("marshal runtime context: %v", err)
	}
	var restored map[string]any
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatalf("unmarshal runtime context: %v", err)
	}
	session.RuntimeContext = restored
	controller.store(session)
	repeated := ensureRuntimeCodexCapability(t, controller, runtimeprep.CodexTurnCapabilitySemanticBrowserUse, "turn-browser-restored", "submit-browser-restored")
	if repeated.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound || adapter.resumeCalls() != 0 {
		t.Fatalf("restored browser = %#v, resumes = %d", repeated, adapter.resumeCalls())
	}
}

func TestEnsureCodexTurnCapabilityComputerRequiresConsentThenReusesSessionBinding(t *testing.T) {
	t.Parallel()
	codexHome := turnCapabilityRuntimeHome(t)
	adapter := &turnCapabilityRuntimeAdapter{provider: ProviderCodex, live: true}
	controller := NewController([]Adapter{adapter}, nil)
	controller.store(turnCapabilityRuntimeSession(codexHome, ProviderCodex))

	first, err := controller.EnsureCodexTurnCapability(context.Background(), CodexTurnCapabilityEnsureInput{
		RoomID: "room-1", AgentSessionID: "session-1", TurnID: "turn-computer", ClientSubmitID: "submit-computer",
		Semantic: runtimeprep.CodexTurnCapabilitySemanticComputerUse,
		Consent:  runtimeprep.CodexTurnCapabilityConsentExplicitSession,
		PlanKey:  "codex_native",
	})
	if err != nil || first.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound ||
		first.Binding.Consent != runtimeprep.CodexTurnCapabilityConsentExplicitSession || adapter.resumeCalls() != 0 {
		t.Fatalf("consented computer ensure = %#v, %v, resumes=%d", first, err, adapter.resumeCalls())
	}
	session, found := controller.Session("room-1", "session-1")
	if !found {
		t.Fatal("session missing after consented computer binding")
	}
	encoded, err := json.Marshal(session.RuntimeContext)
	if err != nil {
		t.Fatalf("marshal runtime context: %v", err)
	}
	var restored map[string]any
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatalf("unmarshal runtime context: %v", err)
	}
	session.RuntimeContext = restored
	controller.store(session)
	repeated := ensureRuntimeCodexCapability(t, controller, runtimeprep.CodexTurnCapabilitySemanticComputerUse, "turn-computer-repeat", "submit-computer-repeat")
	if repeated.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound ||
		!repeated.Binding.Authorized || adapter.resumeCalls() != 0 {
		t.Fatalf("repeated computer ensure = %#v, resumes=%d", repeated, adapter.resumeCalls())
	}
}

func TestComputerCapabilityBindingSurvivesCanonicalStatePatchAndControllerResume(t *testing.T) {
	t.Parallel()
	codexHome := turnCapabilityRuntimeHome(t)
	canonicalStore := &canonicalRuntimeContextStore{}
	firstAdapter := &turnCapabilityRuntimeAdapter{provider: ProviderCodex, live: true}
	firstController := NewController([]Adapter{firstAdapter}, canonicalStore)
	firstSession := turnCapabilityRuntimeSession(codexHome, ProviderCodex)
	firstController.store(firstSession)

	first, err := firstController.EnsureCodexTurnCapability(context.Background(), CodexTurnCapabilityEnsureInput{
		RoomID: "room-1", AgentSessionID: "session-1", TurnID: "turn-computer", ClientSubmitID: "submit-computer",
		Semantic: runtimeprep.CodexTurnCapabilitySemanticComputerUse,
		Consent:  runtimeprep.CodexTurnCapabilityConsentExplicitSession,
		PlanKey:  "codex_native",
	})
	if err != nil || first.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound {
		t.Fatalf("initial computer Ensure = %#v, %v", first, err)
	}
	if _, err := firstController.Exec(context.Background(), ExecInput{
		RoomID: "room-1", AgentSessionID: "session-1", TurnID: "turn-computer", ClientSubmitID: "submit-computer",
		CanonicalSubmitOccurredAtUnixMS: 1,
		Content:                         []PromptContentBlock{{Type: "text", Text: "use computer"}},
	}); err != nil {
		t.Fatalf("initial Exec: %v", err)
	}
	persistedContext, found := canonicalStore.runtimeContext("room-1", "session-1")
	if !found {
		t.Fatal("canonical state reporter did not persist the runtime context")
	}

	// A reconstructed controller receives only the JSON-safe canonical snapshot,
	// exactly as a daemon resume does after process restart.
	restartedAdapter := &turnCapabilityRuntimeAdapter{provider: ProviderCodex}
	restartedController := NewController([]Adapter{restartedAdapter}, nil)
	if _, err := restartedController.Resume(context.Background(), ResumeInput{
		RoomID: "room-1", AgentSessionID: "session-1", Provider: ProviderCodex,
		ProviderSessionID: "provider-session-1", Resumable: true,
		Env: []string{"CODEX_HOME=" + codexHome}, RuntimeContext: persistedContext,
	}); err != nil {
		t.Fatalf("Resume from canonical state: %v", err)
	}
	rebindsAfterResume := restartedAdapter.resumeCalls()
	second := ensureRuntimeCodexCapability(
		t,
		restartedController,
		runtimeprep.CodexTurnCapabilitySemanticComputerUse,
		"turn-computer-again",
		"submit-computer-again",
	)
	if second.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound ||
		!second.Binding.Authorized ||
		restartedAdapter.resumeCalls() != rebindsAfterResume {
		t.Fatalf("resumed computer Ensure = %#v, resume calls=%d (before Ensure=%d)", second, restartedAdapter.resumeCalls(), rebindsAfterResume)
	}
}

func TestCapabilityTurnDurablyReportsJSONSafeBindingContext(t *testing.T) {
	t.Parallel()
	codexHome := turnCapabilityRuntimeHome(t)
	reporter := &recordingReporter{}
	adapter := &turnCapabilityRuntimeAdapter{provider: ProviderCodex, live: true}
	controller := NewController([]Adapter{adapter}, reporter)
	controller.store(turnCapabilityRuntimeSession(codexHome, ProviderCodex))
	result, err := controller.EnsureCodexTurnCapability(context.Background(), CodexTurnCapabilityEnsureInput{
		RoomID: "room-1", AgentSessionID: "session-1", TurnID: "turn-computer", ClientSubmitID: "submit-computer",
		Semantic: runtimeprep.CodexTurnCapabilitySemanticComputerUse, Consent: runtimeprep.CodexTurnCapabilityConsentExplicitSession, PlanKey: "codex_native",
	})
	if err != nil {
		t.Fatalf("EnsureCodexTurnCapability: %v", err)
	}
	if result.Disposition != runtimeprep.CodexTurnCapabilityAlreadyBound {
		t.Fatalf("ensure result = %#v", result)
	}
	if _, err := controller.Exec(context.Background(), ExecInput{
		RoomID: "room-1", AgentSessionID: "session-1", TurnID: "turn-exec", ClientSubmitID: "submit-exec",
		CanonicalSubmitOccurredAtUnixMS: 1,
		Content:                         []PromptContentBlock{{Type: "text", Text: "ordinary message"}},
	}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	calls := reporter.snapshot()
	if len(calls) == 0 || len(calls[0].report.StatePatches) == 0 {
		t.Fatalf("durable reports = %#v", calls)
	}
	context := calls[0].report.StatePatches[0].RuntimeContext
	raw, ok := context[runtimeprep.CodexTurnCapabilityBindingsRuntimeContextKey].([]map[string]any)
	if !ok || len(raw) != 1 || raw[0]["semantic"] != runtimeprep.CodexNativeCapabilityComputer || raw[0]["authorized"] != true {
		t.Fatalf("durable runtime context = %#v", context)
	}
	if _, found := raw[0]["loaded"]; found {
		t.Fatalf("live Loaded leaked into durable context: %#v", raw[0])
	}
	if _, found := raw[0]["ready"]; found {
		t.Fatalf("live Ready leaked into durable context: %#v", raw[0])
	}
}

// canonicalRuntimeContextStore models the canonical state-patch write boundary
// used by the runtime reporter. It JSON-round-trips the patch before making it
// available to a rebuilt controller, so this test cannot accidentally reuse a
// live typed runtime context.
type canonicalRuntimeContextStore struct {
	mu       sync.Mutex
	contexts map[string]map[string]any
}

func (s *canonicalRuntimeContextStore) Report(_ context.Context, report agentsessionstore.ReportActivityInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.contexts == nil {
		s.contexts = make(map[string]map[string]any)
	}
	for _, patch := range report.StatePatches {
		if patch.AgentSessionID == "" || patch.RuntimeContext == nil {
			continue
		}
		encoded, err := json.Marshal(patch.RuntimeContext)
		if err != nil {
			return err
		}
		var persisted map[string]any
		if err := json.Unmarshal(encoded, &persisted); err != nil {
			return err
		}
		s.contexts[sessionKey(report.WorkspaceID, patch.AgentSessionID)] = persisted
	}
	return nil
}

func (s *canonicalRuntimeContextStore) ReportSubmitProvenance(ctx context.Context, report agentsessionstore.ReportActivityInput) error {
	return s.Report(ctx, report)
}

func (s *canonicalRuntimeContextStore) runtimeContext(roomID, agentSessionID string) (map[string]any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	context, found := s.contexts[sessionKey(roomID, agentSessionID)]
	return clonePayload(context), found
}

func ensureRuntimeCodexCapability(t *testing.T, controller *Controller, semantic, turnID, clientSubmitID string) CodexTurnCapabilityEnsureResult {
	t.Helper()
	result, err := controller.EnsureCodexTurnCapability(context.Background(), CodexTurnCapabilityEnsureInput{
		RoomID: "room-1", AgentSessionID: "session-1", TurnID: turnID, ClientSubmitID: clientSubmitID, Semantic: semantic, PlanKey: "codex_native",
	})
	if err != nil {
		t.Fatalf("EnsureCodexTurnCapability(%s): %v", semantic, err)
	}
	return result
}

func turnCapabilityRuntimeSession(codexHome, provider string) Session {
	return Session{
		RoomID: "room-1", AgentSessionID: "session-1", RootAgentSessionID: "session-1", Provider: provider,
		ProviderSessionID: "provider-session-1", Resumable: true, Env: []string{"CODEX_HOME=" + codexHome}, Status: SessionStatusReady,
	}
}

func turnCapabilityRuntimeHome(t *testing.T) string {
	t.Helper()
	codexHome := t.TempDir()
	browserRoot := filepath.Join(codexHome, "plugins", "cache", "openai-bundled", "browser", "1.2.3")
	sitesRoot := filepath.Join(codexHome, "plugins", "cache", "openai-bundled", "sites", "4.5.6")
	computerRoot := filepath.Join(codexHome, "plugins", "cache", "openai-bundled", "computer-use", "1.0.0")
	for _, root := range []string{browserRoot, sitesRoot, computerRoot} {
		if err := os.MkdirAll(filepath.Join(root, ".codex-plugin"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".codex-plugin", "plugin.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustRuntimeCapabilityFile(t, filepath.Join(browserRoot, "scripts", "browser-client.mjs"), "// browser\n")
	mustRuntimeCapabilityFile(t, filepath.Join(browserRoot, "skills", "control-in-app-browser", "SKILL.md"), "# browser\n")
	mustRuntimeCapabilityFile(t, filepath.Join(sitesRoot, "skills", "sites-building", "SKILL.md"), "# sites\n")
	mustRuntimeCapabilityFile(t, filepath.Join(sitesRoot, "skills", "sites-hosting", "SKILL.md"), "# host\n")
	mustRuntimeCapabilityFile(t, filepath.Join(sitesRoot, ".app.json"), `{"apps":{"sites":{}}}`)
	mustRuntimeCapabilityFile(t, filepath.Join(computerRoot, "skills", "computer-use", "SKILL.md"), "# computer\n")
	launcher := filepath.Join(computerRoot, "bin", "computer-use-client-launcher")
	mustRuntimeCapabilityFile(t, launcher, "#!/bin/sh\n")
	if err := os.Chmod(launcher, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRuntimeCapabilityFile(t, filepath.Join(computerRoot, ".mcp.json"), `{"mcpServers":{"computer-use":{"command":"./bin/computer-use-client-launcher","args":["mcp"],"cwd":"."}}}`)
	client := filepath.Join(codexHome, filepath.FromSlash("computer-use-client"))
	mustRuntimeCapabilityFile(t, client, "binary")
	if err := os.Chmod(client, 0o755); err != nil {
		t.Fatal(err)
	}
	node := filepath.Join(codexHome, "node")
	mustRuntimeCapabilityFile(t, node, "#!/bin/sh\n")
	if err := os.Chmod(node, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRuntimeCapabilityFile(t, filepath.Join(codexHome, "config.toml"), `[plugins."browser@openai-bundled"]
enabled = true

[plugins."sites@openai-bundled"]
enabled = true

[plugins."computer-use@openai-bundled"]
enabled = false

[mcp_servers.computer-use]
enabled = false

[mcp_servers.node_repl]
command = "`+node+`"
enabled = true

[mcp_servers.node_repl.env]
BROWSER_USE_AVAILABLE_BACKENDS = "chrome"
`)
	return codexHome
}

func mustRuntimeCapabilityFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func bindingIsLoaded(bindings []runtimeprep.CodexTurnCapabilityBinding, semantic string) bool {
	for _, binding := range bindings {
		if binding.Semantic == semantic && binding.Loaded && binding.Ready {
			return true
		}
	}
	return false
}

type turnCapabilityRuntimeAdapter struct {
	mu          sync.Mutex
	provider    string
	live        bool
	resumeErr   error
	resumeCount int
	ensureCount int
	execDone    chan struct{}
}

func (a *turnCapabilityRuntimeAdapter) Provider() string                  { return a.provider }
func (*turnCapabilityRuntimeAdapter) supportsCodexTurnCapabilityRuntime() {}
func (*turnCapabilityRuntimeAdapter) Start(context.Context, Session) ([]activityshared.Event, error) {
	return nil, nil
}
func (a *turnCapabilityRuntimeAdapter) Resume(context.Context, Session) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.resumeCount++
	if a.resumeErr != nil {
		return a.resumeErr
	}
	a.live = true
	return nil
}
func (a *turnCapabilityRuntimeAdapter) EnsureLiveCodexTurnCapability(_ context.Context, session Session, semantic string, consent runtimeprep.CodexTurnCapabilityConsent) (CodexTurnCapabilityEnsureResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ensureCount++
	if semantic == runtimeprep.CodexNativeCapabilityComputer && consent != runtimeprep.CodexTurnCapabilityConsentExplicitSession && !codexComputerAuthorized(session.RuntimeContext) {
		return CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityRejected, Reason: "computer use requires explicit session-scoped authorization"}, nil
	}
	pluginID, _ := codexTurnCapabilityPluginID(semantic)
	return CodexTurnCapabilityEnsureResult{
		Disposition: runtimeprep.CodexTurnCapabilityAlreadyBound,
		Mention:     runtimeprep.CodexTurnCapabilityMention{Name: pluginID, Path: "plugin://" + pluginID},
		PromptItem:  runtimeprep.CodexTurnCapabilityPromptItem{Type: "mention", Name: pluginID, Path: "plugin://" + pluginID},
		Binding: runtimeprep.CodexTurnCapabilityBinding{
			Semantic: semantic, Authorized: semantic != runtimeprep.CodexNativeCapabilityComputer || consent == runtimeprep.CodexTurnCapabilityConsentExplicitSession || codexComputerAuthorized(session.RuntimeContext), Consent: consent,
		},
	}, nil
}
func (a *turnCapabilityRuntimeAdapter) EnsureLiveCodexTuttiTurnCapability(_ context.Context, session Session, semantic string, consent runtimeprep.CodexTurnCapabilityConsent) (CodexTurnCapabilityEnsureResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ensureCount++
	if semantic == runtimeprep.CodexTurnCapabilitySemanticComputerUse && consent != runtimeprep.CodexTurnCapabilityConsentExplicitSession && !codexComputerAuthorized(session.RuntimeContext) {
		return CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityRejected, Reason: "computer use requires explicit session-scoped authorization"}, nil
	}
	identity, ok := runtimeprep.CodexTuttiTurnSkillForTurnSemantic(semantic)
	if !ok {
		return CodexTurnCapabilityEnsureResult{Disposition: runtimeprep.CodexTurnCapabilityRejected}, nil
	}
	nativeSemantic, _ := runtimeprep.CodexNativeCapabilityForTurnSemantic(semantic)
	return CodexTurnCapabilityEnsureResult{
		Disposition: runtimeprep.CodexTurnCapabilityAlreadyBound,
		PromptItem:  runtimeprep.CodexTurnCapabilityPromptItem{Type: "skill", Name: identity.Name, Path: "/runtime/skills/" + identity.Name + "/SKILL.md"},
		Binding:     runtimeprep.CodexTurnCapabilityBinding{Semantic: nativeSemantic, Authorized: nativeSemantic != runtimeprep.CodexNativeCapabilityComputer || consent == runtimeprep.CodexTurnCapabilityConsentExplicitSession || codexComputerAuthorized(session.RuntimeContext), Consent: consent},
	}, nil
}
func (a *turnCapabilityRuntimeAdapter) Close(context.Context, Session) error {
	a.mu.Lock()
	a.live = false
	a.mu.Unlock()
	return nil
}
func (a *turnCapabilityRuntimeAdapter) Exec(_ context.Context, _ Session, _ []PromptContentBlock, _ string, _ string, _ EventSink, _ CommandSnapshotSink) ([]activityshared.Event, error) {
	if a.execDone != nil {
		a.execDone <- struct{}{}
	}
	return nil, nil
}
func (*turnCapabilityRuntimeAdapter) Cancel(context.Context, Session, string) ([]activityshared.Event, error) {
	return nil, nil
}
func (a *turnCapabilityRuntimeAdapter) HasLiveSession(Session) bool { return a.isLive() }
func (a *turnCapabilityRuntimeAdapter) resumeCalls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.resumeCount
}
func (a *turnCapabilityRuntimeAdapter) ensureCalls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ensureCount
}
func (a *turnCapabilityRuntimeAdapter) isLive() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.live
}
