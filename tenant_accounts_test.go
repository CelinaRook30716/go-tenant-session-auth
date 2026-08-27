package main

import (
	"errors"
	"testing"
	"time"
)

func TestLoginLifecycleDecision(t *testing.T) {
	tests := []struct {
		name      string
		password  string
		state     accountState
		wantError error
	}{
		{name: "active owner receives session", password: "pipeline-passphrase", state: stateActive},
		{name: "wrong password is rejected", password: "incorrect-password", state: stateActive, wantError: errBadCredentials},
		{name: "suspended owner is rejected", password: "pipeline-passphrase", state: stateSuspended, wantError: errUserSuspended},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newAccountStore()
			store.now = func() time.Time { return time.Unix(1700000000, 0) }
			_, owner, err := store.onboard("Warehouse Labs", "owner@example.com", "pipeline-passphrase")
			if err != nil {
				t.Fatal(err)
			}
			owner.State = tt.state
			sess, err := store.login("owner@example.com", tt.password)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("login error = %v, want %v", err, tt.wantError)
			}
			if tt.wantError == nil && sess.ExpiresAt.Sub(store.now()) != 12*time.Hour {
				t.Fatalf("session lifetime = %s", sess.ExpiresAt.Sub(store.now()))
			}
		})
	}
}

func TestSuspensionRevokesExistingSession(t *testing.T) {
	store := newAccountStore()
	_, owner, _ := store.onboard("Warehouse Labs", "owner@example.com", "pipeline-passphrase")
	sess, _ := store.login(owner.Email, "pipeline-passphrase")
	if err := store.setUserState(owner.ID, owner.ID, stateSuspended); err != nil {
		t.Fatal(err)
	}
	if _, err := store.authenticate(sess.ID); !errors.Is(err, errBadCredentials) {
		t.Fatalf("authenticate after suspension = %v", err)
	}
}
