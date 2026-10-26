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
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"time"
)

type inviteCode struct {
	Version   int        `json:"version"`
	Peer      peerRecord `json:"peer"`
	Token     string     `json:"token"`
	ExpiresAt time.Time  `json:"expires_at"`
}
type pairWire struct {
	Version   int        `json:"version"`
	ID        string     `json:"id"`
	Token     string     `json:"token"`
	Peer      peerRecord `json:"peer"`
	ExpiresAt time.Time  `json:"expires_at"`
}
type pairStatus struct {
	Version  int        `json:"version"`
	Accepted bool       `json:"accepted"`
	Peer     peerRecord `json:"peer"`
}
type networkError struct {
	Code    int
	Message string
}

func (e *networkError) Error() string { return fmt.Sprintf("mesh remote %d: %s", e.Code, e.Message) }

func (s *Service) selfLocked() peerRecord {
	return peerRecord{HubID: s.id, PublicKey: s.key.Public().(ed25519.PublicKey), Name: s.disk.Config.Name, Address: s.address}
}
func (s *Service) Invite(ttl time.Duration) (Invite, error) {
	if ttl <= 0 || ttl > 10*time.Minute {
		return Invite{}, errors.New("invitation lifetime must be between 0 and 10 minutes")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.disk.Config.Enabled || s.address == "" {
		return Invite{}, errors.New("LAN transport is disabled")
	}
	if !validAddress(s.address, false) {
		return Invite{}, errors.New("set an explicit LAN advertised address before inviting peers")
	}
	now := s.now()
	for token, expiry := range s.invites {
		if !expiry.After(now) {
			delete(s.invites, token)
		}
	}
	if len(s.invites) >= 32 {
		return Invite{}, errors.New("too many outstanding invitations")
	}
	token := randomID()
	expires := now.Add(ttl)
	s.invites[token] = expires
	data, _ := json.Marshal(inviteCode{Version: Version, Peer: s.selfLocked(), Token: token, ExpiresAt: expires.UTC()})
	return Invite{Code: base64.RawURLEncoding.EncodeToString(data), ExpiresAt: expires.UTC()}, nil
}
func validPeer(p peerRecord) bool {
	return len(p.PublicKey) == ed25519.PublicKeySize && hubID(p.PublicKey) == p.HubID && len(p.Name) <= 120 && validAddress(p.Address, false)
}
func (s *Service) RequestPair(ctx context.Context, code string) (PairRequest, error) {
	return s.RequestPairAt(ctx, "", code)
}

// RequestPairAt overrides only the invitation's network address. The advertised
// Hub ID, public key, and invitation proof remain pinned to the invitation.
func (s *Service) RequestPairAt(ctx context.Context, address, code string) (PairRequest, error) {
	if address != "" && !validAddress(address, false) {
		return PairRequest{}, errors.New("manual address must be a numeric LAN address and port")
	}
	if len(code) > 4096 {
		return PairRequest{}, errors.New("invitation exceeds limit")
	}
	data, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil {
		return PairRequest{}, errors.New("invalid invitation")
	}
	var invite inviteCode
	if err = json.Unmarshal(data, &invite); err != nil || invite.Version != Version || !validPeer(invite.Peer) || len(invite.Token) < 20 || invite.ExpiresAt.IsZero() {
		return PairRequest{}, errors.New("invalid or incompatible invitation")
	}
	if address != "" {
		invite.Peer.Address = address
	}
	if invite.Peer.HubID == s.id {
		return PairRequest{}, errors.New("cannot pair with self")
	}
	s.mu.Lock()
	if !s.disk.Config.Enabled || s.address == "" {
		s.mu.Unlock()
		return PairRequest{}, errors.New("LAN transport is disabled")
	}
	if !validAddress(s.address, false) {
		s.mu.Unlock()
		return PairRequest{}, errors.New("set an explicit LAN advertised address before pairing")
	}
	if len(s.disk.Pending) >= maxPeers || len(s.disk.Peers) >= maxPeers {
		s.mu.Unlock()
		return PairRequest{}, errors.New("pairing capacity reached")
	}
	id := randomID()
	// The issuer alone checks its token's lifetime. Keep that opaque timestamp
	// for matching/retries, and bound our own pending state using our local clock.
	record := pendingRecord{PairRequest: PairRequest{ID: id, HubID: invite.Peer.HubID, Name: invite.Peer.Name, Address: invite.Peer.Address, Direction: "outbound", ExpiresAt: s.now().Add(10 * time.Minute)}, Peer: invite.Peer, Token: invite.Token, InviteExpiresAt: invite.ExpiresAt}
	s.disk.Pending[id] = record
	if err = s.persistLocked(); err != nil {
		delete(s.disk.Pending, id)
		s.mu.Unlock()
		return PairRequest{}, err
	}
	self := s.selfLocked()
	s.mu.Unlock()
	var result PairRequest
	err = s.request(ctx, invite.Peer, "/v1/pair/request", pairWire{Version: Version, ID: id, Token: invite.Token, Peer: self, ExpiresAt: invite.ExpiresAt}, &result)
	s.changed()
	if err != nil {
		return record.PairRequest, err
	}
	return record.PairRequest, nil
}

func (s *Service) AcceptPair(ctx context.Context, id string) error {
	s.mu.Lock()
	pending, ok := s.disk.Pending[id]
	if !ok || pending.Direction != "inbound" || !pending.ExpiresAt.After(s.now()) {
		s.mu.Unlock()
		return errors.New("pair request missing or expired")
	}
	if len(s.disk.Peers) >= maxPeers {
		s.mu.Unlock()
		return errors.New("peer capacity reached")
	}
	if _, exists := s.disk.Peers[pending.HubID]; exists {
		s.mu.Unlock()
		return errors.New("peer already paired")
	}
	s.disk.Peers[pending.HubID] = pending.Peer
	delete(s.disk.Pending, id)
	if err := s.persistLocked(); err != nil {
		delete(s.disk.Peers, pending.HubID)
		s.disk.Pending[id] = pending
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	s.changed()
	return nil
}

func (s *Service) client(peer peerRecord) *http.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := peer.HubID + "|" + peer.Address
	if client, ok := s.clients[key]; ok {
		return client
	}
	// Discovery is a hint only. Every new connection pins the invitation/pair key.
	config := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{s.certificate}, InsecureSkipVerify: true, NextProtos: []string{"http/1.1"}, VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
		if len(raw) != 1 {
			return errors.New("unexpected peer certificate chain")
		}
		cert, err := x509.ParseCertificate(raw[0])
		if err != nil {
			return err
		}
		public, ok := cert.PublicKey.(ed25519.PublicKey)
		if !ok || !bytes.Equal(public, peer.PublicKey) || hubID(public) != peer.HubID {
			return errors.New("peer identity does not match pinned key")
		}
		return nil
	}}
	transport := &http.Transport{TLSClientConfig: config, DialContext: (&net.Dialer{Timeout: time.Second, KeepAlive: 15 * time.Second}).DialContext, TLSHandshakeTimeout: 2 * time.Second, ResponseHeaderTimeout: 5 * time.Second, IdleConnTimeout: 30 * time.Second, MaxIdleConns: maxPeers, MaxIdleConnsPerHost: 2, MaxConnsPerHost: 2, DisableCompression: true}
	client := &http.Client{Transport: transport, Timeout: 7 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("mesh redirects forbidden") }}
	// Temporary failed invitations cannot create an unbounded transport cache.
	if len(s.clients) >= maxPeers*2 {
		for k, c := range s.clients {
			c.CloseIdleConnections()
			delete(s.clients, k)
			break
		}
	}
	s.clients[key] = client
	return client
}
func (s *Service) request(ctx context.Context, peer peerRecord, path string, input, output any) error {
	return s.requestLimited(ctx, peer, path, input, output, maxFrame)
}

func (s *Service) requestLimited(ctx context.Context, peer peerRecord, path string, input, output any, limit int) error {
	var body io.Reader
	method := http.MethodGet
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		if len(data) > limit {
			return errors.New("mesh frame exceeds limit")
		}
		body = bytes.NewReader(data)
		method = http.MethodPost
	}
	route, err := url.ParseRequestURI(path)
	if err != nil {
		return err
	}
	// URL.String escapes an IPv6 scope delimiter for HTTP without changing the
	// numeric peer address used by the dialer or the pairing query parameters.
	target := url.URL{Scheme: "https", Host: peer.Address, Path: route.Path, RawPath: route.RawPath, RawQuery: route.RawQuery}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client(peer).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, int64(limit)+1))
	if err != nil {
		return err
	}
	if len(data) > limit {
		return errors.New("mesh response exceeds limit")
	}
	if res.StatusCode != http.StatusOK {
		return &networkError{Code: res.StatusCode, Message: string(data)}
	}
	if output != nil {
		return json.Unmarshal(data, output)
	}
	return nil
}

func remoteKey(r *http.Request) ([]byte, error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
		return nil, errors.New("client certificate required")
	}
	public, ok := r.TLS.PeerCertificates[0].PublicKey.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("unsupported client identity")
	}
	return public, nil
}
func decodeRequest(w http.ResponseWriter, r *http.Request, value any) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFrame)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		http.Error(w, "invalid or oversized frame", http.StatusBadRequest)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		http.Error(w, "trailing frame data", http.StatusBadRequest)
		return false
	}
	return true
}
func respond(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func (s *Service) handle(w http.ResponseWriter, r *http.Request) {
	public, err := remoteKey(r)
	if err != nil {
		http.Error(w, "client identity required", http.StatusUnauthorized)
		return
	}
	id := hubID(public)
	s.mu.Lock()
	enabled := s.disk.Config.Enabled
	peer, paired := s.disk.Peers[id]
	s.mu.Unlock()
	if !enabled {
		http.Error(w, "LAN disabled", http.StatusServiceUnavailable)
		return
	}
	switch r.URL.Path {
	case "/v1/pair/request":
		s.handlePair(w, r, public)
		return
	case "/v1/pair/status":
		if r.Method != http.MethodGet {
			http.Error(w, "GET required", http.StatusMethodNotAllowed)
			return
		}
		s.mu.Lock()
		status := pairStatus{Version: Version, Accepted: paired, Peer: s.selfLocked()}
		s.mu.Unlock()
		respond(w, status)
		return
	}
	if !paired || !bytes.Equal(peer.PublicKey, public) {
		http.Error(w, "peer is not paired", http.StatusForbidden)
		return
	}
	switch r.URL.Path {
	case "/v1/manage":
		s.handleManagement(w, r, id)
	case "/v1/directory":
		if r.Method != http.MethodGet {
			http.Error(w, "GET required", http.StatusMethodNotAllowed)
			return
		}
		frame, err := s.directory()
		if err != nil {
			http.Error(w, "directory revision unavailable", http.StatusServiceUnavailable)
			return
		}
		respond(w, frame)
	case "/v1/mail":
		var frame relayFrame
		if !decodeRequest(w, r, &frame) {
			return
		}
		ack, err := s.deliver(r.Context(), id, frame)
		if err != nil {
			code := http.StatusServiceUnavailable
			var netErr *networkError
			if errors.As(err, &netErr) {
				code = netErr.Code
			}
			http.Error(w, err.Error(), code)
			return
		}
		respond(w, ack)
	default:
		http.NotFound(w, r)
	}
}
func (s *Service) handlePair(w http.ResponseWriter, r *http.Request, public []byte) {
	var request pairWire
	if !decodeRequest(w, r, &request) {
		return
	}
	if request.Version != Version {
		http.Error(w, "incompatible mesh version", http.StatusUpgradeRequired)
		return
	}
	if !validPeer(request.Peer) || !bytes.Equal(request.Peer.PublicKey, public) || request.Peer.HubID == s.id || len(request.ID) > 100 || request.ID == "" {
		http.Error(w, "pair identity mismatch", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	if existing, ok := s.disk.Pending[request.ID]; ok {
		s.mu.Unlock()
		if existing.HubID != request.Peer.HubID || existing.Direction != "inbound" {
			http.Error(w, "pair request conflict", http.StatusConflict)
			return
		}
		if !existing.ExpiresAt.After(s.now()) {
			http.Error(w, "invitation expired or consumed", http.StatusForbidden)
			return
		}
		respond(w, existing.PairRequest)
		return
	}
	expiry, ok := s.invites[request.Token]
	if !ok || !expiry.After(s.now()) || !request.ExpiresAt.Equal(expiry) {
		s.mu.Unlock()
		http.Error(w, "invitation expired or consumed", http.StatusForbidden)
		return
	}
	if len(s.disk.Pending) >= maxPeers || len(s.disk.Peers) >= maxPeers {
		s.mu.Unlock()
		http.Error(w, "pairing capacity reached", http.StatusServiceUnavailable)
		return
	}
	record := pendingRecord{PairRequest: PairRequest{ID: request.ID, HubID: request.Peer.HubID, Name: request.Peer.Name, Address: request.Peer.Address, Direction: "inbound", ExpiresAt: expiry}, Peer: request.Peer}
	s.disk.Pending[request.ID] = record
	if err := s.persistLocked(); err != nil {
		delete(s.disk.Pending, request.ID)
		s.mu.Unlock()
		http.Error(w, "pair persistence failed", 503)
		return
	}
	delete(s.invites, request.Token)
	s.mu.Unlock()
	s.changed()
	respond(w, record.PairRequest)
}

func (s *Service) maintain() {
	s.mu.Lock()
	_ = s.expireInboxLocked(time.Now())
	if !s.disk.Config.Enabled || s.server == nil {
		s.mu.Unlock()
		return
	}
	ctx := s.ctx
	pending := []pendingRecord{}
	peers := []peerRecord{}
	expired := map[string]pendingRecord{}
	for id, p := range s.disk.Pending {
		if !p.ExpiresAt.After(s.now()) {
			expired[id] = p
			delete(s.disk.Pending, id)
			continue
		}
		if p.Direction == "outbound" && p.ExpiresAt.After(s.now()) {
			pending = append(pending, p)
		}
	}
	if len(expired) > 0 {
		if err := s.persistLocked(); err != nil {
			for id, p := range expired {
				s.disk.Pending[id] = p
			}
		}
	}
	for _, p := range s.disk.Peers {
		if !s.retry[p.HubID].Next.After(s.now()) {
			peers = append(peers, p)
		}
	}
	s.mu.Unlock()
	for _, p := range pending {
		if ctx.Err() != nil {
			return
		}
		var status pairStatus
		if err := s.request(ctx, p.Peer, "/v1/pair/status?id="+url.QueryEscape(p.ID), nil, &status); err != nil {
			continue
		}
		if status.Version != Version {
			continue
		}
		if status.Accepted {
			s.mu.Lock()
			if current, ok := s.disk.Pending[p.ID]; ok && current.Direction == "outbound" && current.ExpiresAt.After(s.now()) && bytes.Equal(status.Peer.PublicKey, p.Peer.PublicKey) && status.Peer.HubID == p.Peer.HubID {
				s.disk.Peers[p.HubID] = p.Peer
				delete(s.disk.Pending, p.ID)
				if err := s.persistLocked(); err != nil {
					delete(s.disk.Peers, p.HubID)
					s.disk.Pending[p.ID] = current
				}
			}
			s.mu.Unlock()
			s.changed()
		} else {
			s.mu.Lock()
			self := s.selfLocked()
			s.mu.Unlock()
			_ = s.request(ctx, p.Peer, "/v1/pair/request", pairWire{Version: Version, ID: p.ID, Token: p.Token, Peer: self, ExpiresAt: p.InviteExpiresAt}, nil)
		}
	}
	// A small worker limit keeps offline peers from serially stalling every route.
	sem := make(chan struct{}, 8)
	var done = make(chan struct{}, len(peers))
	launched := 0
launch:
	for _, p := range peers {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break launch
		}
		launched++
		go func(p peerRecord) {
			defer func() { <-sem; done <- struct{}{} }()
			s.refresh(ctx, p)
		}(p)
	}
	for n := 0; n < launched; n++ {
		<-done
	}
	s.flush(ctx)
}

func (s *Service) refresh(ctx context.Context, p peerRecord) {
	var directory directoryFrame
	err := s.request(ctx, p, "/v1/directory", nil, &directory)
	if err == nil && directory.Version != Version {
		err = errors.New("incompatible mesh directory version")
	}
	if err != nil {
		s.mu.Lock()
		alternates := []Candidate{}
		for _, candidate := range s.candidates {
			if candidate.HubID == p.HubID && candidate.Address != p.Address && candidate.ExpiresAt.After(time.Now()) && len(alternates) < 3 {
				alternates = append(alternates, candidate)
			}
		}
		s.mu.Unlock()
		for _, candidate := range alternates {
			next := p
			next.Address = candidate.Address
			if requestErr := s.request(ctx, next, "/v1/directory", nil, &directory); requestErr == nil && directory.Version == Version {
				p = next
				err = nil
				break
			}
		}
	}
	s.mu.Lock()
	previous, stillPaired := s.disk.Peers[p.HubID]
	if !stillPaired {
		s.mu.Unlock()
		return
	}
	if err != nil {
		delete(s.online, p.HubID)
		backoff := s.retry[p.HubID]
		if backoff.Failures < 6 {
			backoff.Failures++
		}
		delay := time.Second << uint(backoff.Failures-1)
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		backoff.Next = s.now().Add(delay)
		s.retry[p.HubID] = backoff
		s.mu.Unlock()
		return
	}
	if previous.Address != p.Address {
		s.disk.Peers[p.HubID] = p
		if err := s.persistLocked(); err != nil {
			s.disk.Peers[p.HubID] = previous
			s.mu.Unlock()
			return
		}
	}
	s.online[p.HubID] = s.now().Add(4 * time.Second)
	s.retry[p.HubID] = retryState{Next: s.now().Add(time.Second)}
	s.mu.Unlock()
	s.acceptDirectoryFrom(p.HubID, directory)
	s.changed()
}

func (s *Service) sortedPeers() []peerRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	peers := []peerRecord{}
	for _, p := range s.disk.Peers {
		peers = append(peers, p)
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].HubID < peers[j].HubID })
	return peers
}
