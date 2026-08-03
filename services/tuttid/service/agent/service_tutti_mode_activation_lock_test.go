package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
)

const activationLockTestTimeout = time.Second

type activationLockTurnCounts struct {
	mu        sync.Mutex
	admission int
	resume    int
	ensure    int
	exec      int
}

func (c *activationLockTurnCounts) recordAdmission() { c.mu.Lock(); c.admission++; c.mu.Unlock() }
func (c *activationLockTurnCounts) recordRuntime() {
	c.mu.Lock()
	c.resume++
	c.ensure++
	c.exec++
	c.mu.Unlock()
}

func (c *activationLockTurnCounts) snapshot() activationLockTurnCounts {
	c.mu.Lock()
	defer c.mu.Unlock()
	return activationLockTurnCounts{admission: c.admission, resume: c.resume, ensure: c.ensure, exec: c.exec}
}

// TestTuttiModeActivationLockLinearizesNativeTurnLifecycle proves the API
// activation mutation and the Host session locker share one production lock.
// The native worker deliberately represents the Host's locked admission ->
// Resume -> Ensure -> Exec interval; an activation mutation cannot interleave
// inside that interval.
func TestTuttiModeActivationLockLinearizesNativeTurnLifecycle(t *testing.T) {
	t.Parallel()

	ref := agenthost.SessionRef{WorkspaceID: "workspace-1", AgentSessionID: "session-1"}

	t.Run("native turn first blocks activation until exec completes", func(t *testing.T) {
		service := &Service{}
		counts := &activationLockTurnCounts{}
		nativeEntered := make(chan struct{})
		releaseNative := make(chan struct{})
		nativeDone := make(chan struct{})
		go func() {
			unlock, err := (serviceHostLocker{service: service}).Acquire(context.Background(), ref)
			if err != nil {
				t.Errorf("native lock: %v", err)
				close(nativeDone)
				return
			}
			defer unlock()
			counts.recordAdmission()
			close(nativeEntered)
			<-releaseNative
			counts.recordRuntime()
			close(nativeDone)
		}()
		awaitActivationLock(t, nativeEntered, "native admission")

		activationAcquired := make(chan struct{})
		activationDone := make(chan struct{})
		go func() {
			unlock, err := service.AcquireTuttiModeActivationSessionLock(context.Background(), ref.WorkspaceID, ref.AgentSessionID)
			if err == nil {
				close(activationAcquired)
				unlock()
			}
			close(activationDone)
		}()
		assertActivationLockBlocked(t, activationAcquired, "activation Set")
		close(releaseNative)
		awaitActivationLock(t, nativeDone, "native Resume/Ensure/Exec")
		awaitActivationLock(t, activationAcquired, "activation Set after native turn")
		awaitActivationLock(t, activationDone, "activation completion")
		if got := counts.snapshot(); got.admission != 1 || got.resume != 1 || got.ensure != 1 || got.exec != 1 {
			t.Fatalf("native lifecycle counts = %#v, want one admission/resume/ensure/exec", got)
		}
	})

	t.Run("active mutation first rejects later native admission without runtime effects", func(t *testing.T) {
		service := &Service{}
		counts := &activationLockTurnCounts{}
		unlock, err := service.AcquireTuttiModeActivationSessionLock(context.Background(), ref.WorkspaceID, ref.AgentSessionID)
		if err != nil {
			t.Fatal(err)
		}
		active := true // This is the durable Set commit while holding the shared lock.
		nativeDone := make(chan struct{})
		go func() {
			turnUnlock, lockErr := (serviceHostLocker{service: service}).Acquire(context.Background(), ref)
			if lockErr == nil {
				defer turnUnlock()
				counts.recordAdmission()
				if !active {
					counts.recordRuntime()
				}
			}
			close(nativeDone)
		}()
		assertActivationLockBlocked(t, nativeDone, "native turn behind active Set")
		unlock()
		awaitActivationLock(t, nativeDone, "native rejection")
		if got := counts.snapshot(); got.admission != 1 || got.resume != 0 || got.ensure != 0 || got.exec != 0 {
			t.Fatalf("native lifecycle counts = %#v, want admission only after active Set", got)
		}
	})

	t.Run("deactivation is symmetric and every error path releases the lock", func(t *testing.T) {
		service := &Service{}
		counts := &activationLockTurnCounts{}

		// An inactive commit that linearizes first permits the following native turn.
		unlock, err := service.AcquireTuttiModeActivationSessionLock(context.Background(), ref.WorkspaceID, ref.AgentSessionID)
		if err != nil {
			t.Fatal(err)
		}
		active := false
		unlock()
		turnDone := make(chan struct{})
		go func() {
			turnUnlock, lockErr := (serviceHostLocker{service: service}).Acquire(context.Background(), ref)
			if lockErr == nil {
				defer turnUnlock()
				counts.recordAdmission()
				if !active {
					counts.recordRuntime()
				}
			}
			close(turnDone)
		}()
		awaitActivationLock(t, turnDone, "native turn after inactive Set")
		if got := counts.snapshot(); got.resume != 1 || got.ensure != 1 || got.exec != 1 {
			t.Fatalf("inactive-first native lifecycle = %#v, want runtime effects", got)
		}

		// Conversely a native turn in progress holds deactivation until its error
		// path releases the shared lock.
		nativeUnlock, err := (serviceHostLocker{service: service}).Acquire(context.Background(), ref)
		if err != nil {
			t.Fatal(err)
		}
		deactivated := make(chan struct{})
		go func() {
			setUnlock, lockErr := service.AcquireTuttiModeActivationSessionLock(context.Background(), ref.WorkspaceID, ref.AgentSessionID)
			if lockErr == nil {
				active = false
				setUnlock()
			}
			close(deactivated)
		}()
		assertActivationLockBlocked(t, deactivated, "deactivation Set behind native turn")
		nativeUnlock() // Models an admission/runtime error path's deferred unlock.
		awaitActivationLock(t, deactivated, "deactivation after native error")

		// A failing Set must likewise release the lock for a later native turn.
		setUnlock, err := service.AcquireTuttiModeActivationSessionLock(context.Background(), ref.WorkspaceID, ref.AgentSessionID)
		if err != nil {
			t.Fatal(err)
		}
		setUnlock()
		afterError := make(chan struct{})
		go func() {
			turnUnlock, lockErr := (serviceHostLocker{service: service}).Acquire(context.Background(), ref)
			if lockErr == nil {
				turnUnlock()
			}
			close(afterError)
		}()
		awaitActivationLock(t, afterError, "native turn after Set error")

		// A blocked Host admission canceled by its caller must also release its
		// reference; otherwise a later activation/native turn could deadlock.
		heldUnlock, err := service.AcquireTuttiModeActivationSessionLock(context.Background(), ref.WorkspaceID, ref.AgentSessionID)
		if err != nil {
			t.Fatal(err)
		}
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := (serviceHostLocker{service: service}).Acquire(canceled, ref); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled native admission lock error = %v, want context canceled", err)
		}
		heldUnlock()
		recovered := make(chan struct{})
		go func() {
			recoveredUnlock, lockErr := (serviceHostLocker{service: service}).Acquire(context.Background(), ref)
			if lockErr == nil {
				recoveredUnlock()
			}
			close(recovered)
		}()
		awaitActivationLock(t, recovered, "native turn after canceled admission")
	})
}

func awaitActivationLock(t *testing.T, done <-chan struct{}, operation string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(activationLockTestTimeout):
		t.Fatalf("timed out waiting for %s", operation)
	}
}

func assertActivationLockBlocked(t *testing.T, done <-chan struct{}, operation string) {
	t.Helper()
	select {
	case <-done:
		t.Fatalf("%s completed before the shared session-policy lock was released", operation)
	case <-time.After(25 * time.Millisecond):
	}
}
