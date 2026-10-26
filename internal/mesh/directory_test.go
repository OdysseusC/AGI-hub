package mesh

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustDirectory(t *testing.T, s *Service) directoryFrame {
	t.Helper()
	frame, err := s.directory()
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func offlineDirectoryPeer(t *testing.T) (*Service, *Service, func(directoryFrame)) {
	t.Helper()
	local, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	remote, err := New(Options{DataDir: t.TempDir(), LocalAgents: func() []Agent {
		return []Agent{{ID: "codex-pr9", Name: "Codex", Driver: "codex", CommunicationLanguage: "auto"}}
	}})
	if err != nil {
		t.Fatal(err)
	}
	remote.address = "100.70.1.2:7347"
	remote.disk.Config.Exports = []string{"codex-pr9"}
	peer := remote.selfLocked()
	local.disk.Config.Enabled = true
	local.disk.Peers[peer.HubID] = peer
	// No listener or maintenance: only the HTTP transport is replaced. The real
	// refresh, signed directory validation and public Snapshot are exercised.
	local.server = &http.Server{}
	var frame directoryFrame
	local.clients[peer.HubID+"|"+peer.Address] = &http.Client{Transport: offlineRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/directory" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
		}
		data, err := json.Marshal(frame)
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(data))}, nil
	})}
	return local, remote, func(next directoryFrame) { frame = next; local.refresh(context.Background(), peer) }
}

func TestPairedDirectoryDoesNotRequireSynchronizedClocks(t *testing.T) {
	for _, offset := range []time.Duration{-2 * time.Hour, 5700 * time.Millisecond, 2 * time.Hour} {
		t.Run(offset.String(), func(t *testing.T) {
			local, remote, refresh := offlineDirectoryPeer(t)
			now := time.Now().Add(offset)
			local.clock = func() time.Time { return now }
			remote.clock = func() time.Time { return now.Add(-2 * offset) }
			frame := mustDirectory(t, remote)
			refresh(frame)
			peer := local.Snapshot().Peers[0]
			if !peer.Online || !peer.Reachable || len(peer.Agents) != 1 {
				t.Fatalf("live self directory requires no shared clock: %+v", peer)
			}
			first := local.adLeases[remote.id]
			if first.Sub(now) != directoryLease {
				t.Fatal("lease not relative to local receipt")
			}
			now = now.Add(9 * time.Second)
			refresh(frame)
			if !local.adLeases[remote.id].After(first) {
				t.Fatal("fresh direct response did not renew its lease")
			}
			now = now.Add(11 * time.Second)
			if len(local.Snapshot().Peers[0].Agents) != 0 {
				t.Fatal("expired local lease still exposes agents")
			}
			raw, _ := json.Marshal(frame)
			if strings.Contains(string(raw), "expires_at") || !ed25519.Verify(remote.key.Public().(ed25519.PublicKey), advertisementBytes(frame.Entries[0]), frame.Entries[0].Signature) {
				t.Fatal("wire directory depends on UTC or signature was altered")
			}
		})
	}
}

func TestRelayedDirectoryRevisionCannotRenewOrReviveExpiredLease(t *testing.T) {
	local, remote, _ := offlineDirectoryPeer(t)
	now := time.Now()
	local.clock = func() time.Time { return now }
	older, frame := mustDirectory(t, remote), mustDirectory(t, remote)
	local.acceptDirectory(frame)
	deadline := local.adLeases[remote.id]
	now = now.Add(9 * time.Second)
	local.acceptDirectory(frame)
	local.acceptDirectory(older)
	if !local.adLeases[remote.id].Equal(deadline) {
		t.Fatal("same or older relay revision renewed the lease")
	}
	now = now.Add(2 * time.Second)
	local.acceptDirectory(frame)
	local.acceptDirectory(older)
	if len(local.Snapshot().Peers[0].Agents) != 0 || local.adRevisions[remote.id] != frame.Entries[0].Revision {
		t.Fatal("old relay revived or lost high-water mark")
	}
	if local.Snapshot().Peers[0].Online {
		t.Fatal("relay hint claimed a live direct connection")
	}
	local.acceptDirectory(mustDirectory(t, remote))
	if len(local.Snapshot().Peers[0].Agents) != 1 || local.adLeases[remote.id].Sub(now) != directoryLease {
		t.Fatal("new revision did not get one bounded local lease")
	}
	ad := local.ads[remote.id]
	if !ed25519.Verify(ad.PublicKey, advertisementBytes(ad), ad.Signature) {
		t.Fatal("lease bookkeeping altered signed ad")
	}
}

func TestInvalidDirectoryCannotRenewLocalLease(t *testing.T) {
	for _, kind := range []string{"bad_signature", "invalid_profile", "invalid_address", "wrong_pin", "old_version", "zero_revision"} {
		t.Run(kind, func(t *testing.T) {
			local, remote, refresh := offlineDirectoryPeer(t)
			now := time.Now()
			local.clock = func() time.Time { return now }
			refresh(mustDirectory(t, remote))
			deadline := local.adLeases[remote.id]
			now = now.Add(9 * time.Second)
			frame := mustDirectory(t, remote)
			ad := &frame.Entries[0]
			switch kind {
			case "invalid_profile":
				ad.Agents[0].CommunicationLanguage = "not-a-language"
			case "invalid_address":
				ad.Address = "8.8.8.8:7347"
			case "wrong_pin":
				p := local.disk.Peers[remote.id]
				p.PublicKey = make([]byte, ed25519.PublicKeySize)
				local.disk.Peers[remote.id] = p
			case "old_version":
				frame.Version = Version - 1
			case "zero_revision":
				ad.Revision = 0
			}
			ad.Signature = ed25519.Sign(remote.key, advertisementBytes(*ad))
			if kind == "bad_signature" {
				ad.Name += " forged"
			}
			refresh(frame)
			if !local.adLeases[remote.id].Equal(deadline) {
				t.Fatal("invalid directory renewed local lease")
			}
			if kind == "old_version" && local.Snapshot().Peers[0].Online {
				t.Fatal("old mesh version reported online")
			}
		})
	}
}

func TestDirectoryRevisionReservationSurvivesRestartAndFailsClosed(t *testing.T) {
	_, source, _ := offlineDirectoryPeer(t)
	first := mustDirectory(t, source).Entries[0].Revision
	path := filepath.Join(source.opts.DataDir, "directory-revision.json")
	reserved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 10; n++ {
		if mustDirectory(t, source).Entries[0].Revision <= first {
			t.Fatal("revision did not increase")
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(reserved, after) {
		t.Fatal("every directory response rewrites reserved range")
	}
	restarted, err := New(Options{DataDir: source.opts.DataDir})
	if err != nil {
		t.Fatal(err)
	}
	if restarted.id != source.id || mustDirectory(t, restarted).Entries[0].Revision <= source.revision {
		t.Fatal("restart rolled back revision")
	}
	restarted.revision = restarted.revisionLimit
	if mustDirectory(t, restarted).Entries[0].Revision <= source.revisionLimit {
		t.Fatal("exhausted revision range reused")
	}
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	restarted.revision = restarted.revisionLimit
	if _, err := restarted.directory(); err == nil {
		t.Fatal("failed reservation still produced signed directory")
	}
	if _, err := New(Options{DataDir: source.opts.DataDir}); err == nil {
		t.Fatal("corrupt revision silently reset")
	}
}

func TestDirectoryHighWaterMarksAreBoundedAndPairedOriginsHaveCapacity(t *testing.T) {
	local, remote, refresh := offlineDirectoryPeer(t)
	now := time.Now()
	local.clock = func() time.Time { return now }
	frame := directoryFrame{Version: Version}
	for n := 0; n < maxPeers; n++ {
		public, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		ad := advertisement{HubID: hubID(public), PublicKey: public, Address: "100.70.1.2:7347", Revision: 1}
		ad.Signature = ed25519.Sign(key, advertisementBytes(ad))
		frame.Entries = append(frame.Entries, ad)
	}
	local.acceptDirectory(frame)
	now = now.Add(11 * time.Second)
	local.acceptDirectory(frame)
	if len(local.ads) != 0 || len(local.adRevisions) != maxPeers {
		t.Fatal("expired body retained or high-water marks evicted")
	}
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ad := advertisement{HubID: hubID(public), PublicKey: public, Address: "100.70.1.2:7347", Revision: 1}
	ad.Signature = ed25519.Sign(key, advertisementBytes(ad))
	local.acceptDirectory(directoryFrame{Version: Version, Entries: []advertisement{ad}})
	if len(local.adRevisions) != maxPeers {
		t.Fatal("relay high-water cache exceeded bound")
	}
	refresh(mustDirectory(t, remote))
	if len(local.Snapshot().Peers[0].Agents) != 1 || len(local.adRevisions) != maxPeers+1 {
		t.Fatal("relay hints consumed paired capacity")
	}
	if err := local.persistLocked(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(Options{DataDir: local.opts.DataDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted.adLeases) != 0 || len(restarted.Snapshot().Peers[0].Agents) != 0 {
		t.Fatal("restart restored stale lease")
	}
	if err := local.Disconnect(remote.id); err != nil {
		t.Fatal(err)
	}
	if _, ok := local.adRevisions[remote.id]; ok {
		t.Fatal("disconnect retained revoked peer state")
	}
}
