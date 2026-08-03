package runtimeprep

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexTurnCapabilityBindingRuntimeContextCodecIsJSONSafeAndFailClosed(t *testing.T) {
	context := RuntimeContextWithCodexTurnCapabilityBinding(nil, CodexTurnCapabilityBinding{
		Semantic: CodexNativeCapabilityComputer, Ready: true, Loaded: true,
		PackageVersion: "1.2.3", Authorized: true,
	})
	encoded, err := json.Marshal(context)
	if err != nil {
		t.Fatal(err)
	}
	var restored map[string]any
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	bindings := CodexTurnCapabilityBindingsFromRuntimeContext(restored)
	if strings.Contains(string(encoded), "explicitSession") || strings.Contains(string(encoded), "\"consent\"") {
		t.Fatalf("runtime context persisted a raw consent: %s", encoded)
	}
	if len(bindings) != 1 || bindings[0].Semantic != CodexNativeCapabilityComputer || !bindings[0].Authorized {
		t.Fatalf("restored bindings = %#v", bindings)
	}
	// Future fields are harmless; legacy live fields never reconstruct current
	// readiness, and forged consent never reconstructs Computer authorization.
	restored[CodexTurnCapabilityBindingsRuntimeContextKey] = []any{
		map[string]any{"semantic": CodexNativeCapabilityBrowser, "loaded": true, "ready": true, "future": "ok"},
		map[string]any{"semantic": CodexNativeCapabilityComputer, "loaded": true, "ready": true, "consent": "forged"},
		map[string]any{"semantic": CodexNativeCapabilitySites, "loaded": "true", "ready": true},
	}
	bindings = CodexTurnCapabilityBindingsFromRuntimeContext(restored)
	if len(bindings) != 0 {
		t.Fatalf("fail-safe decode bindings = %#v", bindings)
	}
}

func TestEnsureCodexTurnCapabilityAlreadyBoundBrowserReturnsValidatedMention(t *testing.T) {
	t.Parallel()
	codexHome, pluginRoot := turnCapabilityBrowserHome(t, true)
	result, err := EnsureCodexTurnCapability(CodexTurnCapabilityEnsureInput{
		Semantic:  CodexNativeCapabilityBrowser,
		CodexHome: codexHome,
		CurrentBindings: []CodexTurnCapabilityBinding{{
			Semantic: CodexNativeCapabilityBrowser, Ready: true, Loaded: true,
			PackageVersion: filepath.Base(pluginRoot),
		}},
	})
	if err != nil || result.Disposition != CodexTurnCapabilityAlreadyBound {
		t.Fatalf("ensure = %#v, %v", result, err)
	}
	if result.Mention.Name != CodexNativePluginBrowser || result.Mention.Path != "plugin://"+CodexNativePluginBrowser {
		t.Fatalf("mention = %#v", result.Mention)
	}
}

func TestValidateCodexTuttiTurnSkillRequiresManagedCurrentSessionSkill(t *testing.T) {
	t.Parallel()
	codexHome := t.TempDir()
	skillPath := filepath.Join(codexHome, "skills", "browser-use", "SKILL.md")
	mustWrite(t, skillPath, "---\nname: browser-use\n---\n")
	mustWrite(t, filepath.Join(filepath.Dir(skillPath), ".tutti-managed-skill"), "tutti/browser-use\n")
	if !ValidateCodexTuttiTurnSkill(codexHome, CodexTurnCapabilitySemanticBrowserUse, "browser-use", skillPath) {
		t.Fatal("managed Browser skill was rejected")
	}
	if ValidateCodexTuttiTurnSkill(codexHome, CodexTurnCapabilitySemanticBrowserUse, "browser-use", filepath.Join(codexHome, "outside", "SKILL.md")) {
		t.Fatal("unmanaged Browser skill was accepted")
	}
	mustWrite(t, filepath.Join(filepath.Dir(skillPath), ".tutti-managed-skill"), "tutti/computer-use\n")
	if ValidateCodexTuttiTurnSkill(codexHome, CodexTurnCapabilitySemanticBrowserUse, "browser-use", skillPath) {
		t.Fatal("wrong managed identity was accepted")
	}
}

func TestEnsureCodexTurnCapabilityBrowserPreparesAndRequiresRebind(t *testing.T) {
	t.Parallel()
	codexHome, _ := turnCapabilityBrowserHome(t, false)
	result, err := EnsureCodexTurnCapability(CodexTurnCapabilityEnsureInput{Semantic: CodexNativeCapabilityBrowser, CodexHome: codexHome})
	if err != nil || result.Disposition != CodexTurnCapabilityApplied || result.Binding.Loaded || !result.Binding.Ready {
		t.Fatalf("ensure = %#v, %v", result, err)
	}
	content, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil || !strings.Contains(string(content), `[plugins."browser@openai-bundled"]`) || !strings.Contains(string(content), "enabled = true") {
		t.Fatalf("prepared config = %q, %v", content, err)
	}
}

func TestEnsureCodexTurnCapabilitySitesReturnsPluginMention(t *testing.T) {
	t.Parallel()
	codexHome, pluginRoot := turnCapabilitySitesHome(t, true)
	result, err := EnsureCodexTurnCapability(CodexTurnCapabilityEnsureInput{
		Semantic: CodexNativeCapabilitySites, CodexHome: codexHome,
		CurrentBindings: []CodexTurnCapabilityBinding{{Semantic: CodexNativeCapabilitySites, Ready: true, Loaded: true, PackageVersion: filepath.Base(pluginRoot)}},
	})
	if err != nil || result.Disposition != CodexTurnCapabilityAlreadyBound || result.Mention.Name != CodexNativePluginSites {
		t.Fatalf("ensure = %#v, %v", result, err)
	}
}

func TestEnsureCodexTurnCapabilityRejectsComputerWithoutAuthorization(t *testing.T) {
	t.Parallel()
	codexHome := t.TempDir()
	mustWrite(t, filepath.Join(codexHome, "config.toml"), "")
	result, err := EnsureCodexTurnCapability(CodexTurnCapabilityEnsureInput{Semantic: CodexNativeCapabilityComputer, CodexHome: codexHome})
	if err != nil || result.Disposition != CodexTurnCapabilityRejected {
		t.Fatalf("ensure = %#v, %v", result, err)
	}
	content, _ := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if string(content) != "" {
		t.Fatalf("computer rejection changed config: %q", content)
	}
}

func TestEnsureCodexTurnCapabilityComputerRequiresExplicitSessionConsent(t *testing.T) {
	t.Parallel()
	codexHome, _ := turnCapabilityComputerHome(t)
	result, err := EnsureCodexTurnCapability(CodexTurnCapabilityEnsureInput{
		Semantic: CodexNativeCapabilityComputer, CodexHome: codexHome,
		Consent: CodexTurnCapabilityConsentExplicitSession,
	})
	if err != nil || result.Disposition != CodexTurnCapabilityApplied ||
		!result.Binding.Authorized ||
		result.Mention.Name != CodexNativePluginComputerUse {
		t.Fatalf("ensure = %#v, %v", result, err)
	}
	if result.Mention.Path != "plugin://"+CodexNativePluginComputerUse {
		t.Fatalf("computer mention = %#v", result.Mention)
	}
	content, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil || !strings.Contains(string(content), "enabled = true") {
		t.Fatalf("consented computer config = %q, %v", content, err)
	}
	repeated, err := EnsureCodexTurnCapability(CodexTurnCapabilityEnsureInput{
		Semantic:  CodexNativeCapabilityComputer,
		CodexHome: codexHome,
		CurrentBindings: []CodexTurnCapabilityBinding{{
			Semantic:       result.Binding.Semantic,
			Loaded:         true,
			Ready:          true,
			PackageVersion: result.Binding.PackageVersion,
			Authorized:     true,
		}},
	})
	if err != nil || repeated.Disposition != CodexTurnCapabilityAlreadyBound ||
		!repeated.Binding.Authorized {
		t.Fatalf("repeated ensure = %#v, %v", repeated, err)
	}
}

func turnCapabilityBrowserHome(t *testing.T, enabled bool) (string, string) {
	t.Helper()
	codexHome := t.TempDir()
	pluginRoot := filepath.Join(codexHome, "plugins", "cache", "openai-bundled", "browser", "1.2.3")
	mustMkdir(t, filepath.Join(pluginRoot, ".codex-plugin"))
	mustWrite(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), `{}`)
	mustWrite(t, filepath.Join(pluginRoot, "scripts", "browser-client.mjs"), "// browser\n")
	mustWrite(t, filepath.Join(pluginRoot, "skills", "control-in-app-browser", "SKILL.md"), "# browser\n")
	node := filepath.Join(codexHome, "node")
	mustWrite(t, node, "#!/bin/sh\n")
	if err := os.Chmod(node, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(codexHome, "config.toml"), `[plugins."browser@openai-bundled"]
enabled = `+strconvBool(enabled)+`

[mcp_servers.node_repl]
command = "`+node+`"
enabled = true

[mcp_servers.node_repl.env]
BROWSER_USE_AVAILABLE_BACKENDS = "chrome"
`)
	return codexHome, pluginRoot
}

func turnCapabilitySitesHome(t *testing.T, enabled bool) (string, string) {
	t.Helper()
	codexHome := t.TempDir()
	pluginRoot := filepath.Join(codexHome, "plugins", "cache", "openai-bundled", "sites", "1.2.3")
	mustMkdir(t, filepath.Join(pluginRoot, ".codex-plugin"))
	mustWrite(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), `{}`)
	mustWrite(t, filepath.Join(pluginRoot, "skills", "sites-building", "SKILL.md"), "# build\n")
	mustWrite(t, filepath.Join(pluginRoot, "skills", "sites-hosting", "SKILL.md"), "# host\n")
	mustWrite(t, filepath.Join(pluginRoot, ".app.json"), `{"apps":{"sites":{}}}`)
	mustWrite(t, filepath.Join(codexHome, "config.toml"), `[plugins."sites@openai-bundled"]
enabled = `+strconvBool(enabled)+`
`)
	return codexHome, pluginRoot
}

func turnCapabilityComputerHome(t *testing.T) (string, string) {
	t.Helper()
	codexHome := t.TempDir()
	pluginRoot := filepath.Join(codexHome, "plugins", "cache", "openai-bundled", "computer-use", "1.2.3")
	mustMkdir(t, filepath.Join(pluginRoot, ".codex-plugin"))
	mustWrite(t, filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), `{}`)
	mustWrite(t, filepath.Join(pluginRoot, "skills", "computer-use", "SKILL.md"), "# computer\n")
	launcher := filepath.Join(pluginRoot, "bin", "computer-use-client-launcher")
	mustWrite(t, launcher, "#!/bin/sh\n")
	if err := os.Chmod(launcher, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(pluginRoot, ".mcp.json"), `{"mcpServers":{"computer-use":{"command":"./bin/computer-use-client-launcher","args":["mcp"],"cwd":"."}}}`)
	client := filepath.Join(codexHome, filepath.FromSlash(codexNativeComputerClientRel))
	mustWrite(t, client, "binary")
	if err := os.Chmod(client, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(codexHome, "config.toml"), `[plugins."computer-use@openai-bundled"]
enabled = false

[mcp_servers.computer-use]
enabled = false
`)
	return codexHome, pluginRoot
}

func strconvBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
