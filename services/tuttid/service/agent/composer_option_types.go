package agent

import (
	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	modelplanbiz "github.com/tutti-os/tutti/services/tuttid/biz/modelplan"
)

type ComposerSettings = agenthost.ComposerSettings

type ComposerOptionsInput struct {
	AgentTargetID            string
	Cwd                      string
	Locale                   string
	Provider                 string
	WorkspaceID              string
	Settings                 ComposerSettings
	IncludeCapabilityCatalog *bool
	// ResolvedModelPlan is a daemon-only exact plan override supplied by a
	// WorkspaceAgent resolver. It may contain a credential and must never be
	// serialized into runtime context or transport responses.
	ResolvedModelPlan *modelplanbiz.Plan
	// IgnoreModelPlanBinding forces provider-native credentials and model
	// discovery for internal probes and subscription checks that must not
	// inherit the workspace target binding. It is daemon-only and must not be
	// exposed as a user-facing session setting.
	IgnoreModelPlanBinding   bool
	providerTargetRef        map[string]any
	extensionComposerProfile ExtensionComposerProfile
}

type ComposerSkillOption struct {
	Name        string
	Trigger     string
	SourceKind  string
	Description string
	PluginName  string
	Path        string
	Invocation  string
}

type ComposerCapabilityOption struct {
	ID          string
	Kind        string
	Name        string
	Label       string
	Description string
	Status      string
	Source      string
	PluginName  string
	ServerName  string
	ToolName    string
	Trigger     string
	Path        string
	Invocation  string
	// Semantic is a stable presentation and interaction key for a
	// provider-native capability. It deliberately does not expose provider
	// implementation identifiers or filesystem-owned icon paths to clients.
	Semantic string
	// InvocationScope controls lifecycle placement for an explicit capability
	// invocation. An omitted value is backwards-compatible createOnly.
	InvocationScope string
	// ConsentRequirement is an explicit, provider-neutral confirmation required
	// for this capability. It is presentation policy only, never session state.
	ConsentRequirement string
}

// ComposerReservedTurnCapabilityAlias is a provider-neutral presentation
// descriptor for a slash command that the provider reserves for a current-turn
// capability. It is deliberately separate from ComposerCapabilityOption and
// ComposerSkillOption: neither skills nor the capability catalog are changed
// by this fail-closed presentation projection.
type ComposerReservedTurnCapabilityAlias struct {
	Alias              string
	Semantic           string
	Name               string
	Label              string
	Description        string
	Status             string
	Reason             string
	NextAction         string
	Invocation         string
	InvocationScope    string
	ConsentRequirement string
}

type ComposerCommandOption struct {
	Name        string
	Description string
	InputHint   string
}

type ComposerReasoningProfile struct {
	DefaultValue string
	Options      []ComposerConfigOptionValue
}

type ComposerOptions struct {
	Provider                      string
	Capabilities                  []string
	Commands                      []ComposerCommandOption
	ModelConfig                   ComposerConfigOption
	PermissionConfig              PermissionConfig
	ReasoningConfig               ComposerConfigOption
	ReasoningOptionsByModel       map[string]ComposerReasoningProfile
	SpeedConfig                   ComposerConfigOption
	EffectiveSettings             ComposerSettings
	RuntimeContext                map[string]any
	Skills                        []ComposerSkillOption
	CapabilityCatalog             []ComposerCapabilityOption
	ReservedTurnCapabilityAliases []ComposerReservedTurnCapabilityAlias
	Behavior                      providerregistry.ComposerBehaviorDescriptor
	SlashCommandPolicy            *providerregistry.SlashCommandPolicyDescriptor
}
