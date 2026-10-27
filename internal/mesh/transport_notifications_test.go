package mesh

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func observeTransportChanges(local *Service) *int {
	changes := new(int)
	local.opts.OnChange = func() {
		// A notification may inspect the resulting state without taking the
		// transport mutation's lock recursively.
		_ = local.Snapshot()
		(*changes)++
	}
	return changes
}

func TestRefreshNotificationsIgnoreSignedDirectoryRenewals(t *testing.T) {
	local, remote, refresh := offlineDirectoryPeer(t)
	now := time.Now()
	local.clock = func() time.Time { return now }
	agents := []Agent{
		{ID: "codex-pr9", Name: "Codex", Driver: "codex", Status: "idle", CommunicationLanguage: "auto"},
		{ID: "other", Name: "Other", Driver: "workbuddy", Status: "busy", CommunicationLanguage: "en"},
	}
	remote.opts.LocalAgents = func() []Agent { return agents }
	remote.disk.Config.Exports = []string{"codex-pr9", "other"}
	changes := observeTransportChanges(local)
	first := mustDirectory(t, remote)
	refresh(first)
	if *changes != 1 {
		t.Fatalf("first online notification = %d, want 1", *changes)
	}
	lease, online := local.adLeases[remote.id], local.online[remote.id]
	now = now.Add(time.Second)
	refresh(first) // A direct, pinned response may renew the same revision.
	if !local.adLeases[remote.id].After(lease) || !local.online[remote.id].After(online) {
		t.Fatal("quiet refresh stopped renewing live directory/online leases")
	}
	agents[0], agents[1] = agents[1], agents[0]
	newer := mustDirectory(t, remote)
	if reflect.DeepEqual(first.Entries[0].Signature, newer.Entries[0].Signature) {
		t.Fatal("fixture did not advance the signed revision")
	}
	refresh(newer)
	if *changes != 1 {
		t.Fatalf("revision, signature, lease or agent order generated %d notifications", *changes)
	}
	if local.adRevisions[remote.id] != newer.Entries[0].Revision {
		t.Fatal("quiet refresh stopped advancing replay protection")
	}
}

func TestRefreshNotificationsTrackEffectiveDirectoryAndRoutes(t *testing.T) {
	local, remote, refresh := offlineDirectoryPeer(t)
	changes := observeTransportChanges(local)
	refresh(mustDirectory(t, remote))
	want := 1
	agent := Agent{ID: "codex-pr9", Name: "Codex", Driver: "codex", CommunicationLanguage: "auto"}
	remote.opts.LocalAgents = func() []Agent { return []Agent{agent} }
	for _, change := range []func(){
		func() { agent.Status = "idle" },
		func() { agent.Name = "Renamed" },
		func() { agent.Driver = "workbuddy" },
		func() { agent.CommunicationLanguage = "en" },
		func() { remote.disk.Config.Exports = nil },
		func() { remote.disk.Config.Exports = []string{agent.ID} },
	} {
		change()
		frame := mustDirectory(t, remote)
		refresh(frame)
		want++
		refresh(frame)
		if *changes != want {
			t.Fatalf("semantic change notification = %d, want %d", *changes, want)
		}
	}
	third, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	third.address = "100.70.1.3:7347"
	local.disk.Peers[third.id] = third.selfLocked()
	// Synchronize an already-published administrative change with the next
	// refresh, rather than reporting the same peer addition twice.
	local.changed()
	want++
	refresh(mustDirectory(t, remote))
	if *changes != want {
		t.Fatal("refresh repeated an already-notified peer addition")
	}
	frame := mustDirectory(t, remote)
	frame.Entries[0].Neighbors = []string{third.id}
	frame.Entries[0].Signature = ed25519.Sign(remote.key, advertisementBytes(frame.Entries[0]))
	refresh(frame)
	want++
	refresh(frame)
	if *changes != want {
		t.Fatalf("effective route notification = %d, want %d", *changes, want)
	}
	var reachable bool
	for _, p := range local.Snapshot().Peers {
		if p.HubID == third.id {
			reachable = p.Reachable
		}
	}
	if !reachable {
		t.Fatal("route fixture did not make the third paired Hub reachable")
	}
	refresh(mustDirectory(t, remote))
	if *changes != want+1 {
		t.Fatal("removing an effective route did not notify")
	}
}

func TestRefreshNotificationsReportSlowFailureOnceAndRecovery(t *testing.T) {
	local, remote, refresh := offlineDirectoryPeer(t)
	now := time.Now()
	local.clock = func() time.Time { return now }
	changes := observeTransportChanges(local)
	refresh(mustDirectory(t, remote))
	peer := local.disk.Peers[remote.id]
	key := peer.HubID + "|" + peer.Address
	working := local.clients[key]
	first := true
	local.clients[key] = &http.Client{Transport: offlineRoundTripper(func(*http.Request) (*http.Response, error) {
		if first {
			first = false
			now = now.Add(5 * time.Second) // The failed request outlives online TTL.
		}
		return nil, errors.New("offline fixture")
	})}
	local.refresh(context.Background(), peer)
	if *changes != 2 || local.Snapshot().Peers[0].Online {
		t.Fatal("slow online-to-offline transition was not notified")
	}
	local.refresh(context.Background(), peer)
	if *changes != 2 || local.retry[peer.HubID].Failures != 2 {
		t.Fatal("repeated failure notified again or changed retry accounting")
	}
	local.clients[key] = working
	refresh(mustDirectory(t, remote))
	if *changes != 3 || !local.Snapshot().Peers[0].Online {
		t.Fatal("recovery did not notify exactly once")
	}
}

func TestForwardFailureNotifiesBeforeRefreshSeesOfflinePeer(t *testing.T) {
	local, remote, refresh := offlineDirectoryPeer(t)
	changes := observeTransportChanges(local)
	refresh(mustDirectory(t, remote))
	peer := local.disk.Peers[remote.id]
	local.clients[peer.HubID+"|"+peer.Address] = &http.Client{Transport: offlineRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline fixture")
	})}
	_, err := local.forward(context.Background(), relayFrame{Envelope: signedMessage{Message: Message{ToHub: remote.id}}, Path: []string{local.id}})
	if err == nil || *changes != 2 || local.Snapshot().Peers[0].Online {
		t.Fatal("failed forward removed online state without one notification")
	}
	local.refresh(context.Background(), peer)
	if *changes != 2 {
		t.Fatal("refresh repeated the already-notified forward failure")
	}
}

func TestRefreshNotificationsDoNotRepeatConcurrentOnlineState(t *testing.T) {
	local, remote, _ := offlineDirectoryPeer(t)
	now := time.Now()
	local.clock = func() time.Time { return now }
	peer := local.disk.Peers[remote.id]
	data, err := json.Marshal(mustDirectory(t, remote))
	if err != nil {
		t.Fatal(err)
	}
	local.clients[peer.HubID+"|"+peer.Address] = &http.Client{Transport: offlineRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(data))}, nil
	})}
	var changes atomic.Int32
	local.opts.OnChange = func() { _ = local.Snapshot(); changes.Add(1) }
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local.refresh(context.Background(), peer)
		}()
	}
	wg.Wait()
	if changes.Load() != 1 {
		t.Fatalf("concurrent identical online notifications = %d, want 1", changes.Load())
	}
}

func TestRefreshNotificationsIgnoreAnAlreadyOfflinePeer(t *testing.T) {
	local, remote, _ := offlineDirectoryPeer(t)
	peer := local.disk.Peers[remote.id]
	local.clients[peer.HubID+"|"+peer.Address] = &http.Client{Transport: offlineRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline fixture")
	})}
	changes := observeTransportChanges(local)
	local.refresh(context.Background(), peer)
	local.refresh(context.Background(), peer)
	if *changes != 0 {
		t.Fatal("a peer which was never online produced an offline edge")
	}
}
