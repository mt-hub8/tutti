package agenthost

import (
	"context"
	"strings"
	"time"

	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

func legacyClientSubmitID(metadata map[string]any) string {
	value, _ := metadata["clientSubmitId"].(string)
	return strings.TrimSpace(value)
}

func submissionMetadata(metadata map[string]any, typedClientSubmitID string) map[string]any {
	clientID := strings.TrimSpace(typedClientSubmitID)
	if clientID == "" {
		return metadata
	}
	result := cloneMap(metadata)
	if result == nil {
		result = make(map[string]any, 1)
	}
	result["clientSubmitId"] = clientID
	return result
}

// canonicalTurnIDForSubmitClaim restores the durable submit identity before a
// request-local TurnID is allocated. A duplicate ClientSubmitID must therefore
// never manufacture a conflicting turn identity before Host can reconcile its
// existing replay fence.
func (h *Host) canonicalTurnIDForSubmitClaim(ctx context.Context, ref SessionRef, metadata map[string]any, requestedTurnID string) (string, error) {
	requestedTurnID = strings.TrimSpace(requestedTurnID)
	clientID := legacyClientSubmitID(metadata)
	if requestedTurnID != "" || clientID == "" || h == nil || h.store == nil {
		return requestedTurnID, nil
	}
	claim, found, err := h.store.GetSubmitClaim(ctx, ref.WorkspaceID, ref.AgentSessionID, clientID)
	if err != nil || !found {
		return requestedTurnID, err
	}
	if canonical := strings.TrimSpace(claim.CanonicalTurnID); canonical != "" {
		return canonical, nil
	}
	return requestedTurnID, nil
}

func (h *Host) prepareSubmitClaim(ctx context.Context, ref SessionRef, metadata map[string]any, canonicalTurnID string) (storesqlite.SubmitClaim, bool, error) {
	clientID := legacyClientSubmitID(metadata)
	if h == nil || h.store == nil || clientID == "" {
		return storesqlite.SubmitClaim{}, false, nil
	}
	claim, created, err := h.store.PrepareSubmitClaim(ctx, storesqlite.SubmitClaimPrepare{
		WorkspaceID: ref.WorkspaceID, AgentSessionID: ref.AgentSessionID,
		ClientSubmitID: clientID, CanonicalTurnID: strings.TrimSpace(canonicalTurnID), NowUnixMS: h.now().UnixMilli(),
	})
	if err != nil || created || claim.Status != "prepared" {
		return claim, created, err
	}
	turnID, found, err := h.store.FindTurnByClientSubmitID(ctx, ref.WorkspaceID, ref.AgentSessionID, clientID)
	if err != nil || !found {
		return claim, false, err
	}
	// A retry may arrive without a caller-provided TurnID. The existing submit
	// claim, not a newly generated request-local ID, is the canonical fence.
	// Only a disagreement between durable claim and durable turn is a conflict.
	if strings.TrimSpace(turnID) != strings.TrimSpace(claim.CanonicalTurnID) {
		return claim, false, storesqlite.ErrSubmitClaimTurnConflict
	}
	claim, _, err = h.store.AcceptSubmitClaim(
		ctx, ref.WorkspaceID, ref.AgentSessionID, clientID, turnID, h.now().UnixMilli(),
	)
	return claim, false, err
}

func (h *Host) abandonSubmitClaim(ref SessionRef, clientID string) {
	if h == nil || h.store == nil || strings.TrimSpace(clientID) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = h.store.DeleteSubmitClaim(ctx, ref.WorkspaceID, ref.AgentSessionID, clientID)
}

func (h *Host) acceptSubmitClaim(ref SessionRef, clientID, turnID string) error {
	if h == nil || h.store == nil || strings.TrimSpace(clientID) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := h.store.AcceptSubmitClaim(ctx, ref.WorkspaceID, ref.AgentSessionID, clientID, turnID, h.now().UnixMilli())
	return err
}
