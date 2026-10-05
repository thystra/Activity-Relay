package directoryscheduler

import (
	"context"
	"github.com/thystra/Activity-Relay/internal/directoryclient"
	"testing"
	"time"
)

type profileSyncClient struct {
	fakeClient
	digest              string
	version             int
	negotiatedRegisters int
}

func (client *profileSyncClient) ProfileDigest() string { return client.digest }
func (client *profileSyncClient) RegisterNegotiated(context.Context) (directoryclient.Response, error) {
	client.negotiatedRegisters++
	return directoryclient.Response{ProtocolVersion: client.version, Operation: directoryclient.OperationRegister, Outcome: directoryclient.OutcomeUpdated}, nil
}
func (client *profileSyncClient) HeartbeatWithRegisterReconciliationNegotiated(context.Context) (directoryclient.Response, error) {
	return directoryclient.Response{ProtocolVersion: client.version, Operation: directoryclient.OperationHeartbeat, Outcome: directoryclient.OutcomeRecorded}, nil
}

func TestProfileDigestChangeReconcilesBeforeHeartbeatDeadline(t *testing.T) {
	now := time.Date(2026, 10, 3, 22, 0, 0, 0, time.UTC)
	store := newFakeStore()
	oldDigest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	newDigest := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	store.states[testOrigin] = State{Registered: true, NextAttempt: now.Add(12 * time.Hour), LastObserved: now, ProfileDigest: oldDigest, ProfileProtocolVersion: 1}
	client := &profileSyncClient{digest: newDigest, version: 2}
	scheduler := testScheduler(t, store, &fakeClock{now: now}, client, nil)
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := store.states[testOrigin]
	if client.negotiatedRegisters != 1 || state.ProfileDigest != newDigest || state.ProfileProtocolVersion != 2 || state.LastOutcome != "registered" {
		t.Fatalf("client=%#v state=%#v", client, state)
	}
}

func TestProtocolV3HeartbeatReconcilesStoredProfileProtocol(t *testing.T) {
	now := time.Date(2026, 10, 5, 18, 0, 0, 0, time.UTC)
	store := newFakeStore()
	digest := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	store.states[testOrigin] = State{
		Registered:             true,
		NextAttempt:            now,
		LastObserved:           now.Add(-time.Hour),
		ProfileDigest:          digest,
		ProfileProtocolVersion: 2,
	}
	client := &profileSyncClient{digest: digest, version: 3}
	scheduler := testScheduler(t, store, &fakeClock{now: now}, client, nil)
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := store.states[testOrigin]
	if client.negotiatedRegisters != 1 || state.ProfileProtocolVersion != 3 || state.LastOutcome != "registered" {
		t.Fatalf("client=%#v state=%#v", client, state)
	}
}

func TestProfileStateAcceptsProtocolV3(t *testing.T) {
	digest := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	state := State{ProfileDigest: digest, ProfileProtocolVersion: 3}
	if err := validateState(testOrigin, state); err != nil {
		t.Fatalf("validateState() rejected protocol v3 profile state: %v", err)
	}
}
