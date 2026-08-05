package agenthost

// TurnCapabilityConsent is an explicit user-confirmed, session-scoped consent
// fact. It is intentionally an enum rather than a boolean so unknown consent
// claims fail closed at the Host boundary.
type TurnCapabilityConsent string

const (
	TurnCapabilityConsentExplicitSession TurnCapabilityConsent = "explicitSession"
)

// TurnCapabilityInvocation is a provider-neutral semantic request. Host owns
// when it may run but does not interpret or enumerate provider mechanics.
type TurnCapabilityInvocation struct {
	Semantic string
	Consent  TurnCapabilityConsent
}

// RuntimeTurnCapabilityDisposition records the provider's authoritative
// knowledge after an EnsureTurnCapability attempt. Unknown is intentionally
// fail-closed: Host must retain the submit replay fence and never infer that a
// retry can safely re-run the provider mutation.
type RuntimeTurnCapabilityDisposition string

const (
	RuntimeTurnCapabilityAlreadyBound RuntimeTurnCapabilityDisposition = "already_bound"
	RuntimeTurnCapabilityApplied      RuntimeTurnCapabilityDisposition = "applied"
	RuntimeTurnCapabilityRejected     RuntimeTurnCapabilityDisposition = "rejected"
	RuntimeTurnCapabilityUnknown      RuntimeTurnCapabilityDisposition = "unknown"
)

// RuntimeTurnCapabilityInput binds a semantic capability request to the exact
// canonical submission identity. Provider adapters must use this complete key
// for their own idempotency and must not infer a Turn from current session
// state.
type RuntimeTurnCapabilityInput struct {
	WorkspaceID    string
	AgentSessionID string
	TurnID         string
	ClientSubmitID string
	Invocation     TurnCapabilityInvocation
	// Plan is opaque provider/product policy selected after Host has claimed the
	// submission and acquired the session lock. Host never interprets it.
	Plan RuntimeTurnCapabilityPlan
}

// RuntimeTurnCapabilityPlan is an opaque, adapter-selected execution plan for
// one semantic invocation. It lets product policy select an implementation
// without moving claim, locking, Ensure, or Exec out of Host.
type RuntimeTurnCapabilityPlan struct {
	Key string
}

type RuntimeTurnCapabilityAdmissionDisposition string

const (
	RuntimeTurnCapabilityAdmissionAllowed     RuntimeTurnCapabilityAdmissionDisposition = "allowed"
	RuntimeTurnCapabilityAdmissionRejected    RuntimeTurnCapabilityAdmissionDisposition = "rejected"
	RuntimeTurnCapabilityAdmissionUnavailable RuntimeTurnCapabilityAdmissionDisposition = "unavailable"
	RuntimeTurnCapabilityAdmissionUnknown     RuntimeTurnCapabilityAdmissionDisposition = "unknown"
)

// RuntimeTurnCapabilityAdmissionInput carries only canonical target facts and
// the claimed invocation identity to a product policy adapter. Host does not
// interpret those facts as a provider or backend selection.
type RuntimeTurnCapabilityAdmissionInput struct {
	WorkspaceID       string
	AgentSessionID    string
	TurnID            string
	ClientSubmitID    string
	Initial           bool
	AgentTargetID     string
	Provider          string
	ProviderTargetRef map[string]any
	RuntimeContext    map[string]any
	Invocation        TurnCapabilityInvocation
}

type RuntimeTurnCapabilityAdmissionResult struct {
	Disposition RuntimeTurnCapabilityAdmissionDisposition
	Plan        RuntimeTurnCapabilityPlan
}

// RuntimeTurnCapabilityResult may add one internal structured mention or skill
// after a successful already-bound or applied result. An empty augmentation is
// also valid for a provider plan that prepares runtime state without changing
// the prompt. A non-empty augmentation must be exactly one structured block;
// Host rejects text, images, unknown, incomplete, and multiple blocks.
type RuntimeTurnCapabilityResult struct {
	Disposition RuntimeTurnCapabilityDisposition
	// Retryable is valid only for Unknown before Exec. It records a provider
	// operation that is independently idempotent and is not Turn delivery, so
	// Host may abandon the claim for a retry instead of retaining a delivery
	// fence. Providers must never set it after a provider Turn may have started.
	Retryable bool
	// Outcome is a sanitized, provider-neutral recovery instruction. It is
	// meaningful only when Ensure does not return a usable augmentation. Host
	// carries it through the rejected/retryable path but never interprets a
	// provider, plugin, or settings implementation.
	Outcome            *RuntimeTurnCapabilityOutcome
	PromptAugmentation []PromptContentBlock
}

// RuntimeTurnCapabilityNextAction is the small user-recovery vocabulary a
// provider may project after a pre-Exec capability check. It intentionally
// excludes provider identifiers and installation configuration.
type RuntimeTurnCapabilityNextAction string

const (
	RuntimeTurnCapabilityNextActionSetup     RuntimeTurnCapabilityNextAction = "setup_required"
	RuntimeTurnCapabilityNextActionEnable    RuntimeTurnCapabilityNextAction = "enable_required"
	RuntimeTurnCapabilityNextActionAuthorize RuntimeTurnCapabilityNextAction = "authorize_required"
	RuntimeTurnCapabilityNextActionRetry     RuntimeTurnCapabilityNextAction = "retry"
	RuntimeTurnCapabilityNextActionBlocked   RuntimeTurnCapabilityNextAction = "blocked"
)

// RuntimeTurnCapabilityOutcome is safe to return to product adapters. Reason
// codes are a closed, low-cardinality product vocabulary rather than provider
// diagnostics, paths, or raw authorization data.
type RuntimeTurnCapabilityOutcome struct {
	NextAction RuntimeTurnCapabilityNextAction
	ReasonCode string
}
