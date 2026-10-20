package mesh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type peerRecord struct {
	HubID     string `json:"hub_id"`
	PublicKey []byte `json:"public_key"`
	Name      string `json:"name"`
	Address   string `json:"address"`
}
type pendingRecord struct {
	PairRequest
	Peer  peerRecord `json:"peer"`
	Token string     `json:"token,omitempty"`
}
type signedMessage struct {
	Message
	PublicKey []byte `json:"public_key"`
	Signature []byte `json:"signature"`
}
type outboxRecord struct {
	Envelope signedMessage `json:"envelope"`
	Receipt  Receipt       `json:"receipt"`
	Hash     string        `json:"hash"`
}
type inboxRecord struct {
	Envelope signedMessage `json:"envelope"`
	Hash     string        `json:"hash"`
	Received bool          `json:"received"`
	Expired  bool          `json:"expired,omitempty"`
}
type diskState struct {
	Config  Config                   `json:"config"`
	Peers   map[string]peerRecord    `json:"peers"`
	Pending map[string]pendingRecord `json:"pending"`
	Outbox  map[string]outboxRecord  `json:"outbox"`
	Inbox   map[string]inboxRecord   `json:"inbox"`
}
type retryState struct {
	Failures int
	Next     time.Time
}
type Service struct {
	mu          sync.Mutex
	lifecycle   sync.Mutex
	receiveMu   sync.Mutex
	flushMu     sync.Mutex
	opts        Options
	key         ed25519.PrivateKey
	id          string
	certificate tls.Certificate
	disk        diskState
	invites     map[string]time.Time
	online      map[string]time.Time
	ads         map[string]advertisement
	candidates  []Candidate
	clients     map[string]*http.Client
	retry       map[string]retryState
	address     string
	server      *http.Server
	listener    net.Listener
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

func hubID(public []byte) string {
	h := sha256.Sum256(public)
	return hex.EncodeToString(h[:])
}
func randomID() string {
	var b [24]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func New(opts Options) (*Service, error) {
	if opts.DataDir == "" {
		return nil, errors.New("mesh data directory required")
	}
	if err := os.MkdirAll(opts.DataDir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(opts.DataDir, "identity.json")
	var key ed25519.PrivateKey
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		_, key, err = ed25519.GenerateKey(rand.Reader)
		if err == nil {
			err = atomicJSON(path, key)
		}
	} else if err == nil {
		err = json.Unmarshal(data, &key)
	}
	if err != nil {
		return nil, fmt.Errorf("mesh identity: %w", err)
	}
	if len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid mesh identity")
	}
	if !key.Equal(ed25519.NewKeyFromSeed(key.Seed())) {
		return nil, errors.New("inconsistent mesh identity")
	}
	id := hubID(key.Public().(ed25519.PublicKey))
	template := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: id}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		return nil, err
	}
	s := &Service{opts: opts, key: key, id: id, certificate: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, invites: map[string]time.Time{}, online: map[string]time.Time{}, ads: map[string]advertisement{}, clients: map[string]*http.Client{}, retry: map[string]retryState{}}
	s.disk = diskState{Config: Config{Name: "AGI Hub", ListenAddr: "0.0.0.0:7347", Exports: []string{}}}
	data, err = os.ReadFile(filepath.Join(opts.DataDir, "state.json"))
	if err == nil {
		err = json.Unmarshal(data, &s.disk)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("mesh state: %w", err)
	}
	if s.disk.Peers == nil {
		s.disk.Peers = map[string]peerRecord{}
	}
	if s.disk.Pending == nil {
		s.disk.Pending = map[string]pendingRecord{}
	}
	if s.disk.Outbox == nil {
		s.disk.Outbox = map[string]outboxRecord{}
	}
	if s.disk.Inbox == nil {
		s.disk.Inbox = map[string]inboxRecord{}
	}
	if err := s.expireInboxLocked(time.Now()); err != nil {
		return nil, fmt.Errorf("expire mesh inbox: %w", err)
	}
	return s, nil
}

// Expiration preserves the ID/hash tombstone and never grants a received ACK.
// The caller owns mu (or is still constructing the unshared Service).
func (s *Service) expireInboxLocked(now time.Time) error {
	prior := map[string]inboxRecord{}
	for id, record := range s.disk.Inbox {
		if record.Received || record.Expired || record.Envelope.ExpiresAt.After(now) {
			continue
		}
		prior[id] = record
		record.Expired = true
		record.Envelope = signedMessage{}
		s.disk.Inbox[id] = record
	}
	if len(prior) == 0 {
		return nil
	}
	if err := s.persistLocked(); err != nil {
		for id, record := range prior {
			s.disk.Inbox[id] = record
		}
		return err
	}
	return nil
}

func atomicJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mesh-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}
func (s *Service) persistLocked() error {
	return atomicJSON(filepath.Join(s.opts.DataDir, "state.json"), s.disk)
}
func (s *Service) changed() {
	if s.opts.OnChange != nil {
		s.opts.OnChange()
	}
}

func (s *Service) Start(ctx context.Context) error {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return nil
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	enabled := s.disk.Config.Enabled
	s.mu.Unlock()
	if enabled {
		if err := s.listen(); err != nil {
			s.mu.Lock()
			s.cancel()
			s.cancel = nil
			s.mu.Unlock()
			return err
		}
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				s.maintain()
			}
		}
	}()
	return nil
}

func (s *Service) listen() error {
	s.mu.Lock()
	cfg := s.disk.Config
	ctx := s.ctx
	s.mu.Unlock()
	ln, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return err
	}
	ln = newLimitedListener(ln, 64)
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{s.certificate}, ClientAuth: tls.RequireAnyClientCert, NextProtos: []string{"http/1.1"}}
	server := &http.Server{Handler: http.HandlerFunc(s.handle), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 8 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192, BaseContext: func(net.Listener) context.Context { return ctx }}
	address := cfg.AdvertiseAddr
	if address == "" {
		address = ln.Addr().String()
	}
	s.mu.Lock()
	s.server, s.listener, s.address = server, ln, address
	s.mu.Unlock()
	s.wg.Add(1)
	go func() { defer s.wg.Done(); _ = server.Serve(tls.NewListener(ln, tlsConfig)) }()
	return nil
}

func validateConfig(cfg Config) error {
	if len(cfg.Name) > 120 || len(cfg.Exports) > 128 {
		return errors.New("mesh config exceeds limit")
	}
	if cfg.ListenAddr == "" {
		return errors.New("LAN listen address required")
	}
	if !validAddress(cfg.ListenAddr, true) {
		return errors.New("invalid LAN listen address")
	}
	if cfg.AdvertiseAddr != "" {
		if !validAddress(cfg.AdvertiseAddr, false) {
			return errors.New("invalid advertised address")
		}
	}
	for _, id := range cfg.Exports {
		if !validEndpoint(id) {
			return errors.New("invalid exported endpoint")
		}
	}
	return nil
}

func validAddress(address string, listener bool) bool {
	if len(address) > 256 {
		return false
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	if !(ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || (listener && ip.IsUnspecified())) {
		return false
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 0 || number > 65535 || (!listener && number == 0) {
		return false
	}
	return true
}
func validEndpoint(id string) bool {
	if id == "" || len(id) > 100 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

func (s *Service) Configure(cfg Config) error {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "0.0.0.0:7347"
	}
	if err := validateConfig(cfg); err != nil {
		return err
	}
	cfg.Exports = append([]string{}, cfg.Exports...)
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	old := s.disk.Config
	s.disk.Config = cfg
	if err := s.persistLocked(); err != nil {
		s.disk.Config = old
		s.mu.Unlock()
		return err
	}
	server := s.server
	restart := old.ListenAddr != cfg.ListenAddr || old.AdvertiseAddr != cfg.AdvertiseAddr || old.Enabled != cfg.Enabled
	started := s.cancel != nil
	if restart {
		s.server, s.listener, s.address = nil, nil, ""
		s.online = map[string]time.Time{}
		s.retry = map[string]retryState{}
	}
	s.mu.Unlock()
	if restart && server != nil {
		shutdownServer(server)
	}
	if restart {
		s.closeClients()
	}
	if started && cfg.Enabled && (restart || server == nil) {
		if err := s.listen(); err != nil {
			s.mu.Lock()
			s.disk.Config.Enabled = false
			_ = s.persistLocked()
			s.mu.Unlock()
			s.changed()
			return err
		}
	}
	s.changed()
	return nil
}

func (s *Service) Close() error {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	if s.cancel == nil {
		s.mu.Unlock()
		return nil
	}
	s.cancel()
	server := s.server
	s.server, s.listener, s.address = nil, nil, ""
	s.mu.Unlock()
	if server != nil {
		shutdownServer(server)
	}
	s.closeClients()
	s.wg.Wait()
	s.mu.Lock()
	s.cancel = nil
	s.mu.Unlock()
	return nil
}
func shutdownServer(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		_ = server.Close()
	}
}
func (s *Service) closeClients() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, client := range s.clients {
		client.CloseIdleConnections()
	}
	s.clients = map[string]*http.Client{}
}

func (s *Service) exportedAgents() []Agent {
	var agents []Agent
	if s.opts.LocalAgents != nil {
		agents = s.opts.LocalAgents()
	}
	s.mu.Lock()
	exports := append([]string{}, s.disk.Config.Exports...)
	s.mu.Unlock()
	allow := map[string]bool{}
	for _, id := range exports {
		allow[id] = true
	}
	out := []Agent{}
	seen := map[string]bool{}
	for _, a := range agents {
		if allow[a.ID] && validEndpoint(a.ID) && !seen[a.ID] && len(out) < 128 {
			a.Name = strings.ToValidUTF8(a.Name[:min(len(a.Name), 120)], "")
			a.Status = strings.ToValidUTF8(a.Status[:min(len(a.Status), 100)], "")
			out = append(out, a)
			seen[a.ID] = true
		}
	}
	return out
}

func (s *Service) Snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := State{HubID: s.id, Name: s.disk.Config.Name, Address: s.address, Config: s.disk.Config, Peers: []Peer{}, Pending: []PairRequest{}, Routes: []Route{}, Candidates: []Candidate{}, Outbox: []Receipt{}}
	state.Config.Exports = append([]string{}, s.disk.Config.Exports...)
	now := time.Now()
	for id, p := range s.disk.Peers {
		path, expiry := s.routeLocked(id, nil)
		agents := []Agent{}
		if ad, ok := s.ads[id]; ok && ad.ExpiresAt.After(now) {
			agents = append(agents, ad.Agents...)
		}
		state.Peers = append(state.Peers, Peer{HubID: id, Name: p.Name, Address: p.Address, Online: s.isOnlineLocked(id), Reachable: len(path) > 0, Agents: agents})
		if len(path) > 0 {
			state.Routes = append(state.Routes, Route{HubID: id, Path: path, ExpiresAt: expiry})
		}
	}
	for _, p := range s.disk.Pending {
		if p.ExpiresAt.After(now) {
			state.Pending = append(state.Pending, p.PairRequest)
		}
	}
	for _, c := range s.candidates {
		if c.ExpiresAt.After(now) {
			state.Candidates = append(state.Candidates, c)
		}
	}
	for _, o := range s.disk.Outbox {
		state.Outbox = append(state.Outbox, o.Receipt)
	}
	sort.Slice(state.Peers, func(i, j int) bool { return state.Peers[i].HubID < state.Peers[j].HubID })
	sort.Slice(state.Outbox, func(i, j int) bool { return state.Outbox[i].MessageID < state.Outbox[j].MessageID })
	return state
}

func (s *Service) Announcement() Candidate {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Candidate{HubID: s.id, Name: s.disk.Config.Name, Address: s.address, Version: Version, ExpiresAt: time.Now().Add(15 * time.Second)}
}
func (s *Service) SetCandidates(candidates []Candidate) {
	s.mu.Lock()
	previous := map[string]bool{}
	for _, c := range s.candidates {
		previous[c.HubID+"|"+c.Address] = true
	}
	s.candidates = []Candidate{}
	for _, c := range candidates {
		if c.HubID != s.id && len(c.HubID) == 64 && c.Version == Version && len(c.Name) <= 120 && c.ExpiresAt.After(time.Now()) && len(s.candidates) < maxPeers {
			if validAddress(c.Address, false) {
				s.candidates = append(s.candidates, c)
				if !previous[c.HubID+"|"+c.Address] {
					delete(s.retry, c.HubID)
				}
			}
		}
	}
	s.mu.Unlock()
	s.changed()
}

func (s *Service) Disconnect(id string) error {
	s.mu.Lock()
	peer, ok := s.disk.Peers[id]
	if !ok {
		s.mu.Unlock()
		return errors.New("peer not paired")
	}
	pending := map[string]pendingRecord{}
	for requestID, record := range s.disk.Pending {
		if record.HubID == id {
			pending[requestID] = record
			delete(s.disk.Pending, requestID)
		}
	}
	delete(s.disk.Peers, id)
	delete(s.online, id)
	delete(s.ads, id)
	delete(s.retry, id)
	if err := s.persistLocked(); err != nil {
		s.disk.Peers[id] = peer
		for requestID, record := range pending {
			s.disk.Pending[requestID] = record
		}
		s.mu.Unlock()
		return err
	}
	for k, client := range s.clients {
		if strings.HasPrefix(k, id+"|") {
			client.CloseIdleConnections()
			delete(s.clients, k)
		}
	}
	s.mu.Unlock()
	s.changed()
	return nil
}
