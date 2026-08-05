package runtimeprep

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CodexTurnCapabilityDisposition is the provider-local result of checking one
// native Codex capability for an existing runtime. The Host maps these values
// onto its provider-neutral contract; plugin and configuration details stay
// inside the Codex preparation boundary.
type CodexTurnCapabilityDisposition string

const (
	CodexTurnCapabilityAlreadyBound CodexTurnCapabilityDisposition = "already_bound"
	CodexTurnCapabilityApplied      CodexTurnCapabilityDisposition = "applied"
	CodexTurnCapabilityRejected     CodexTurnCapabilityDisposition = "rejected"
	CodexTurnCapabilityUnknown      CodexTurnCapabilityDisposition = "unknown"
)

// CodexTurnCapabilityConsent is the provider-local representation of explicit
// Host session consent. Empty never grants Computer Use enablement.
type CodexTurnCapabilityConsent string

const (
	CodexTurnCapabilityConsentExplicitSession CodexTurnCapabilityConsent = "explicitSession"
)

// Codex turn capability invocation semantics are stable, transport-safe
// names. The native runtime uses its own capability names below; this adapter
// is the single boundary between the external semantic vocabulary and those
// provider mechanics.
const (
	CodexTurnCapabilitySemanticSites       = "sites"
	CodexTurnCapabilitySemanticBrowserUse  = "browserUse"
	CodexTurnCapabilitySemanticComputerUse = "computerUse"
)

// CodexNativeCapabilityForTurnSemantic translates a public turn invocation
// semantic into the native Codex preparation capability. It intentionally
// accepts no plugin, backend, or filesystem identifiers.
func CodexNativeCapabilityForTurnSemantic(semantic string) (string, bool) {
	switch strings.TrimSpace(semantic) {
	case CodexTurnCapabilitySemanticSites:
		return CodexNativeCapabilitySites, true
	case CodexTurnCapabilitySemanticBrowserUse:
		return CodexNativeCapabilityBrowser, true
	case CodexTurnCapabilitySemanticComputerUse:
		return CodexNativeCapabilityComputer, true
	default:
		return "", false
	}
}

func IsCodexTurnCapabilityInvocationSemantic(semantic string) bool {
	_, ok := CodexNativeCapabilityForTurnSemantic(semantic)
	return ok
}

// CodexTurnCapabilityMention is the one structured plugin mention that may be
// appended to a capability turn. It identifies the installed official bundle,
// rather than selecting one of that bundle's skills.
type CodexTurnCapabilityMention struct {
	Name string
	Path string
}

// CodexTurnCapabilityPromptItem is the provider-local structured input for a
// capability Turn. Native plans use an official plugin mention.
type CodexTurnCapabilityPromptItem struct {
	Type string
	Name string
	Path string
}

// CodexTurnCapabilityBinding is a sanitized snapshot suitable for a daemon
// runtime context. It intentionally omits plugin IDs and filesystem roots.
// Loaded and Ready are legacy live-runtime fields retained only for decoding
// historical snapshots. They are never written into new durable snapshots and
// never establish current-client readiness.
//
// PackageVersion is the selected installed-plugin cache directory name. It is
// a stable identity only within the current CODEX_HOME and is used for equality
// checks; it is not a content digest or an integrity claim.
type CodexTurnCapabilityBinding struct {
	Semantic       string
	Loaded         bool
	Ready          bool
	PackageVersion string
	// Authorized is a coarse durable session fact for Computer Use. It never
	// serializes the transient consent enum or any authorization payload.
	Authorized bool
	// Consent is transient provider input/result data. The runtime-context codec
	// intentionally never persists it.
	Consent CodexTurnCapabilityConsent
}

// CodexTurnCapabilityBindingsRuntimeContextKey is the sole durable key for
// sanitized Codex native capability bindings.  The value contains no plugin
// paths, raw invocation, or consent request payload.
const CodexTurnCapabilityBindingsRuntimeContextKey = "codexNativeTurnCapabilities"

// CodexTurnCapabilityBindingsFromRuntimeContext decodes both the live typed
// value and JSON round-tripped values from a canonical runtime snapshot.
// Malformed and future-unknown entries fail closed by being omitted.
func CodexTurnCapabilityBindingsFromRuntimeContext(runtimeContext map[string]any) []CodexTurnCapabilityBinding {
	if runtimeContext == nil {
		return nil
	}
	return codexTurnCapabilityBindingsFromValue(runtimeContext[CodexTurnCapabilityBindingsRuntimeContextKey])
}

// RuntimeContextWithCodexTurnCapabilityBinding returns a JSON-safe runtime
// snapshot with one semantic binding replaced or appended.  This provider
// owned codec is the only writer for the durable binding shape.
func RuntimeContextWithCodexTurnCapabilityBinding(runtimeContext map[string]any, binding CodexTurnCapabilityBinding) map[string]any {
	next := make(map[string]any, len(runtimeContext)+1)
	for key, value := range runtimeContext {
		next[key] = value
	}
	bindings := CodexTurnCapabilityBindingsFromRuntimeContext(next)
	replaced := false
	for index := range bindings {
		if bindings[index].Semantic == binding.Semantic {
			bindings[index] = binding
			replaced = true
			break
		}
	}
	if !replaced {
		bindings = append(bindings, binding)
	}
	persisted := make([]map[string]any, 0, len(bindings))
	for _, item := range bindings {
		entry := map[string]any{
			"semantic":       item.Semantic,
			"packageVersion": item.PackageVersion,
			"authorized":     item.Authorized,
		}
		persisted = append(persisted, entry)
	}
	next[CodexTurnCapabilityBindingsRuntimeContextKey] = persisted
	return next
}

func codexTurnCapabilityBindingsFromValue(value any) []CodexTurnCapabilityBinding {
	var values []any
	switch typed := value.(type) {
	case []CodexTurnCapabilityBinding:
		values = make([]any, len(typed))
		for index := range typed {
			values[index] = typed[index]
		}
	case []map[string]any:
		values = make([]any, len(typed))
		for index := range typed {
			values[index] = typed[index]
		}
	case []any:
		values = typed
	default:
		return nil
	}
	bindings := make([]CodexTurnCapabilityBinding, 0, len(values))
	for _, value := range values {
		binding, ok := codexTurnCapabilityBindingFromRuntimeValue(value)
		if ok {
			bindings = append(bindings, binding)
		}
	}
	return bindings
}

func codexTurnCapabilityBindingFromRuntimeValue(value any) (CodexTurnCapabilityBinding, bool) {
	if binding, ok := value.(CodexTurnCapabilityBinding); ok {
		return binding, bindingIsDurablyUsable(binding)
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return CodexTurnCapabilityBinding{}, false
	}
	semantic, _ := fields["semantic"].(string)
	packageVersion, _ := fields["packageVersion"].(string)
	authorized, _ := fields["authorized"].(bool)
	// Read the former durable representation only for backwards compatibility;
	// all newly written snapshots use the coarse boolean above.
	if !authorized {
		consent, _ := fields["consent"].(string)
		authorized = strings.TrimSpace(consent) == string(CodexTurnCapabilityConsentExplicitSession)
	}
	binding := CodexTurnCapabilityBinding{
		Semantic:       strings.TrimSpace(semantic),
		PackageVersion: strings.TrimSpace(packageVersion),
		Authorized:     authorized,
	}
	return binding, bindingIsDurablyUsable(binding)
}

func bindingIsDurablyUsable(binding CodexTurnCapabilityBinding) bool {
	if binding.Semantic == "" {
		return false
	}
	if binding.Semantic == CodexNativeCapabilityComputer {
		return binding.Authorized
	}
	// Browser and Sites retain only a stable installed-package discriminator for
	// diagnostics/backwards compatibility; callers must always re-check current
	// App Server runtime state before use.
	return binding.PackageVersion != ""
}

// CodexTurnCapabilityEnsureInput supplies only the semantic request, the
// session-scoped Codex home, and the prior sanitized binding snapshots.
type CodexTurnCapabilityEnsureInput struct {
	Semantic        string
	Consent         CodexTurnCapabilityConsent
	CodexHome       string
	CurrentBindings []CodexTurnCapabilityBinding
}

// CodexTurnCapabilityEnsureResult keeps the provider-specific mechanics local
// while exposing the exact structured plugin mention and resulting binding
// facts.
type CodexTurnCapabilityEnsureResult struct {
	Disposition CodexTurnCapabilityDisposition
	Mention     CodexTurnCapabilityMention
	Binding     CodexTurnCapabilityBinding
	Reason      string
}

// EnsureCodexTurnCapability verifies or prepares one native Codex capability
// for an existing session. Browser and Sites may prepare session config and
// require a subsequent runtime rebind. Computer Use may do so only when its
// exact invocation carries explicit session-scoped consent evidence.
func EnsureCodexTurnCapability(input CodexTurnCapabilityEnsureInput) (CodexTurnCapabilityEnsureResult, error) {
	semantic := strings.TrimSpace(input.Semantic)
	codexHome := strings.TrimSpace(input.CodexHome)
	if codexHome == "" {
		return CodexTurnCapabilityEnsureResult{Disposition: CodexTurnCapabilityRejected, Reason: "session CODEX_HOME is required"}, nil
	}
	if !isCodexTurnCapabilitySemantic(semantic) {
		return CodexTurnCapabilityEnsureResult{Disposition: CodexTurnCapabilityRejected, Reason: "unknown Codex turn capability"}, nil
	}
	consent := CodexTurnCapabilityConsent(strings.TrimSpace(string(input.Consent)))
	if consent != "" && consent != CodexTurnCapabilityConsentExplicitSession {
		return CodexTurnCapabilityEnsureResult{Disposition: CodexTurnCapabilityRejected, Reason: "unknown session consent evidence"}, nil
	}
	evidence, err := InspectCodexNativeCapabilityEvidence(codexHome)
	if err != nil {
		return CodexTurnCapabilityEnsureResult{Disposition: CodexTurnCapabilityUnknown}, err
	}
	item, found := nativeCapabilityEvidence(evidence, semantic)
	if !found {
		return CodexTurnCapabilityEnsureResult{Disposition: CodexTurnCapabilityRejected, Reason: "native capability evidence is unavailable"}, nil
	}
	// Validate the installed plugin identity before any session config
	// preparation can write. A malformed plugin must fail closed, not become a
	// partially enabled runtime that cannot receive its structured mention.
	mention, err := resolveCodexTurnCapabilityMention(item)
	if err != nil {
		return CodexTurnCapabilityEnsureResult{Disposition: CodexTurnCapabilityRejected, Reason: err.Error()}, nil
	}
	if semantic == CodexNativeCapabilityComputer {
		if consent != CodexTurnCapabilityConsentExplicitSession {
			binding := CodexTurnCapabilityBinding{
				Semantic:       semantic,
				Loaded:         true,
				Ready:          true,
				PackageVersion: filepath.Base(filepath.Clean(item.InstallPath)),
				Authorized:     true,
			}
			if codexTurnCapabilityBindingMatches(input.CurrentBindings, binding) {
				return CodexTurnCapabilityEnsureResult{
					Disposition: CodexTurnCapabilityAlreadyBound,
					Mention:     mention,
					Binding:     binding,
					Reason:      "validated native capability is already loaded by this runtime",
				}, nil
			}
			return CodexTurnCapabilityEnsureResult{Disposition: CodexTurnCapabilityRejected, Reason: "computer use requires explicit session-scoped authorization"}, nil
		}
		prepared, err := prepareCodexNativeComputerUse(codexHome, true)
		if err != nil {
			return CodexTurnCapabilityEnsureResult{Disposition: CodexTurnCapabilityUnknown}, err
		}
		if !prepared.Prepared {
			return CodexTurnCapabilityEnsureResult{Disposition: CodexTurnCapabilityRejected, Reason: prepared.Reason}, nil
		}
		evidence, err = InspectCodexNativeCapabilityEvidence(codexHome)
		if err != nil {
			return CodexTurnCapabilityEnsureResult{Disposition: CodexTurnCapabilityUnknown}, err
		}
		item, found = nativeCapabilityEvidence(evidence, semantic)
		if !found {
			return CodexTurnCapabilityEnsureResult{Disposition: CodexTurnCapabilityRejected, Reason: "native capability evidence is unavailable"}, nil
		}
		mention, err = resolveCodexTurnCapabilityMention(item)
		if err != nil {
			return CodexTurnCapabilityEnsureResult{Disposition: CodexTurnCapabilityRejected, Reason: err.Error()}, nil
		}
	}
	ready := nativeCapabilityReady(item)
	if !ready {
		if err := prepareCodexTurnCapability(codexHome, semantic); err != nil {
			return CodexTurnCapabilityEnsureResult{Disposition: CodexTurnCapabilityUnknown}, err
		}
		evidence, err = InspectCodexNativeCapabilityEvidence(codexHome)
		if err != nil {
			return CodexTurnCapabilityEnsureResult{Disposition: CodexTurnCapabilityUnknown}, err
		}
		item, found = nativeCapabilityEvidence(evidence, semantic)
		ready = found && nativeCapabilityReady(item)
		if !ready {
			reason := "native capability preparation did not produce ready evidence"
			if found {
				_, reason = nativeCapabilityStateFromEvidence(item)
			}
			return CodexTurnCapabilityEnsureResult{Disposition: CodexTurnCapabilityRejected, Reason: reason}, nil
		}
	}

	mention, err = resolveCodexTurnCapabilityMention(item)
	if err != nil {
		return CodexTurnCapabilityEnsureResult{Disposition: CodexTurnCapabilityRejected, Reason: err.Error()}, nil
	}
	binding := CodexTurnCapabilityBinding{
		Semantic:       semantic,
		Ready:          true,
		PackageVersion: filepath.Base(filepath.Clean(item.InstallPath)),
		Authorized:     semantic == CodexNativeCapabilityComputer && consent == CodexTurnCapabilityConsentExplicitSession,
		Consent:        consent,
	}
	if codexTurnCapabilityBindingMatches(input.CurrentBindings, binding) {
		binding.Loaded = true
		return CodexTurnCapabilityEnsureResult{
			Disposition: CodexTurnCapabilityAlreadyBound,
			Mention:     mention,
			Binding:     binding,
			Reason:      "validated native capability is already loaded by this runtime",
		}, nil
	}
	return CodexTurnCapabilityEnsureResult{
		Disposition: CodexTurnCapabilityApplied,
		Mention:     mention,
		Binding:     binding,
		Reason:      "validated native capability requires runtime rebind",
	}, nil
}

func isCodexTurnCapabilitySemantic(semantic string) bool {
	switch semantic {
	case CodexNativeCapabilityBrowser, CodexNativeCapabilityComputer, CodexNativeCapabilitySites:
		return true
	default:
		return false
	}
}

func nativeCapabilityEvidence(evidence []CodexNativeCapabilityEvidence, semantic string) (CodexNativeCapabilityEvidence, bool) {
	for _, item := range evidence {
		if item.Capability == semantic {
			return item, true
		}
	}
	return CodexNativeCapabilityEvidence{}, false
}

func nativeCapabilityReady(item CodexNativeCapabilityEvidence) bool {
	state, _ := nativeCapabilityStateFromEvidence(item)
	return state == NativeCapabilityReady
}

func prepareCodexTurnCapability(codexHome, semantic string) error {
	switch semantic {
	case CodexNativeCapabilityBrowser:
		_, err := prepareCodexNativeBrowser(codexHome)
		return err
	case CodexNativeCapabilitySites:
		_, err := prepareCodexNativeSites(codexHome)
		return err
	default:
		return fmt.Errorf("unsupported Codex turn capability %q", semantic)
	}
}

func codexTurnCapabilityBindingMatches(current []CodexTurnCapabilityBinding, next CodexTurnCapabilityBinding) bool {
	for _, binding := range current {
		if binding.Loaded && binding.Ready && binding.Semantic == next.Semantic &&
			binding.PackageVersion != "" && binding.PackageVersion == next.PackageVersion &&
			binding.Authorized == next.Authorized {
			return true
		}
	}
	return false
}

func resolveCodexTurnCapabilityMention(item CodexNativeCapabilityEvidence) (CodexTurnCapabilityMention, error) {
	pluginID := strings.TrimSpace(item.PluginID)
	if pluginID == "" || strings.ContainsAny(pluginID, "\\/\t\r\n") {
		return CodexTurnCapabilityMention{}, fmt.Errorf("installed plugin identity is invalid")
	}
	root, err := filepath.EvalSymlinks(strings.TrimSpace(item.InstallPath))
	if err != nil {
		return CodexTurnCapabilityMention{}, fmt.Errorf("resolve installed plugin root: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return CodexTurnCapabilityMention{}, fmt.Errorf("resolve installed plugin root: %w", err)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return CodexTurnCapabilityMention{}, fmt.Errorf("installed plugin root is missing")
	}
	return CodexTurnCapabilityMention{Name: pluginID, Path: "plugin://" + pluginID}, nil
}
