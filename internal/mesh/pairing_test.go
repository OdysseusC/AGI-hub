package mesh

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type pairProbe struct {
	posts  int
	expiry time.Time
}

func offlinePairingHubs(t *testing.T) (*Service, *Service, *pairProbe) {
	t.Helper()
	issuer, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	requester, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	issuer.disk.Config.Enabled, requester.disk.Config.Enabled = true, true
	issuer.address, requester.address = "100.70.1.2:7347", "100.70.1.3:7347"
	requester.server, requester.ctx = &http.Server{}, context.Background()
	peer := issuer.selfLocked()
	probe := &pairProbe{}
	// Call the real receiver handler with a synthetic authenticated TLS identity;
	// no listeners, interfaces, sockets or real assistants are involved.
	requester.clients[peer.HubID+"|"+peer.Address] = &http.Client{Transport: offlineRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/pair/request" {
			data, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			var wire pairWire
			if err := json.Unmarshal(data, &wire); err != nil {
				return nil, err
			}
			probe.posts++
			probe.expiry = wire.ExpiresAt
			r.Body = io.NopCloser(bytes.NewReader(data))
		}
		r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{PublicKey: requester.key.Public().(ed25519.PublicKey)}}}
		w := httptest.NewRecorder()
		issuer.handle(w, r)
		return w.Result(), nil
	})}
	return issuer, requester, probe
}

func TestPairingAcrossClockOffsetsUsesLocalPendingDeadline(t *testing.T) {
	for _, offset := range []time.Duration{-2 * time.Hour, 2 * time.Hour} {
		t.Run(offset.String(), func(t *testing.T) {
			issuer, requester, probe := offlinePairingHubs(t)
			now := time.Now()
			issuer.clock = func() time.Time { return now.Add(offset) }
			requester.clock = func() time.Time { return now }
			invite, err := issuer.Invite(time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			req, err := requester.RequestPair(context.Background(), invite.Code)
			if err != nil {
				t.Fatal(err)
			}
			if req.ExpiresAt.Sub(now) != 10*time.Minute {
				t.Fatal("outbound pending borrowed a foreign deadline")
			}
			if !requester.disk.Pending[req.ID].InviteExpiresAt.Equal(invite.ExpiresAt) || !probe.expiry.Equal(invite.ExpiresAt) {
				t.Fatal("issuer opaque expiry was not preserved")
			}
			requester.maintain() // Status false, retry while waiting for issuer approval.
			if probe.posts != 2 || !probe.expiry.Equal(invite.ExpiresAt) {
				t.Fatal("pair retry replaced issuer expiry with local deadline")
			}
			if err := issuer.AcceptPair(context.Background(), req.ID); err != nil {
				t.Fatal(err)
			}
			requester.maintain()
			if len(requester.Snapshot().Peers) != 1 {
				t.Fatal("approved pair was not imported across clock offset")
			}
		})
	}
}

func TestExpiredInvitationIsRejectedByIssuer(t *testing.T) {
	issuer, requester, probe := offlinePairingHubs(t)
	issuerNow := time.Now()
	issuer.clock = func() time.Time { return issuerNow }
	requester.clock = func() time.Time { return issuerNow.Add(-24 * time.Hour) }
	invite, err := issuer.Invite(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	issuerNow = issuerNow.Add(2 * time.Second)
	_, err = requester.RequestPair(context.Background(), invite.Code)
	var remote *networkError
	if !errors.As(err, &remote) || remote.Code != http.StatusForbidden || probe.posts != 1 {
		t.Fatalf("expiry was not rejected at issuer: %v", err)
	}
	if len(issuer.disk.Pending) != 0 || len(issuer.disk.Peers) != 0 {
		t.Fatal("expired issuer invitation created pairing state")
	}
}

func TestLocalPendingAndIssuerApprovalStillExpire(t *testing.T) {
	issuer, requester, probe := offlinePairingHubs(t)
	issuerNow, requesterNow := time.Now(), time.Now().Add(2*time.Hour)
	issuer.clock = func() time.Time { return issuerNow }
	requester.clock = func() time.Time { return requesterNow }
	invite, err := issuer.Invite(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	req, err := requester.RequestPair(context.Background(), invite.Code)
	if err != nil {
		t.Fatal(err)
	}
	issuerNow = issuerNow.Add(2 * time.Second)
	if err := issuer.AcceptPair(context.Background(), req.ID); err == nil {
		t.Fatal("issuer approved an expired inbound request")
	}
	requesterNow = requesterNow.Add(10*time.Minute + time.Second)
	requester.maintain()
	if len(requester.disk.Pending) != 0 || probe.posts != 1 {
		t.Fatal("expired local pending was retained or retried")
	}
}

func TestPairingRejectsOldProtocolAndTamperedIssuerExpiry(t *testing.T) {
	issuer, requester, probe := offlinePairingHubs(t)
	invite, err := issuer.Invite(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := base64.RawURLEncoding.DecodeString(invite.Code)
	var code inviteCode
	if err := json.Unmarshal(data, &code); err != nil {
		t.Fatal(err)
	}
	code.Version = Version - 1
	data, _ = json.Marshal(code)
	if _, err := requester.RequestPair(context.Background(), base64.RawURLEncoding.EncodeToString(data)); err == nil || probe.posts != 0 {
		t.Fatal("old mesh invitation was accepted")
	}
	code.Version = Version
	code.ExpiresAt = code.ExpiresAt.Add(time.Hour)
	data, _ = json.Marshal(code)
	_, err = requester.RequestPair(context.Background(), base64.RawURLEncoding.EncodeToString(data))
	var remote *networkError
	if !errors.As(err, &remote) || remote.Code != http.StatusForbidden {
		t.Fatalf("tampered issuer expiry accepted: %v", err)
	}
	peer := issuer.selfLocked()
	err = requester.request(context.Background(), peer, "/v1/pair/request", pairWire{Version: Version - 1, ID: "old-mesh", Peer: requester.selfLocked(), Token: code.Token, ExpiresAt: invite.ExpiresAt}, nil)
	if !errors.As(err, &remote) || remote.Code != http.StatusUpgradeRequired {
		t.Fatalf("old pairing wire version accepted: %v", err)
	}
}

func TestLegacyOutboundPairCannotReuseForeignPendingDeadline(t *testing.T) {
	_, requester, _ := offlinePairingHubs(t)
	requester.disk.Pending["legacy"] = pendingRecord{PairRequest: PairRequest{
		ID: "legacy", Direction: "outbound", ExpiresAt: time.Now().Add(24 * time.Hour),
	}}
	if err := requester.persistLocked(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(Options{DataDir: requester.opts.DataDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted.disk.Pending) != 0 {
		t.Fatal("legacy foreign pending deadline survived mesh upgrade")
	}
}
