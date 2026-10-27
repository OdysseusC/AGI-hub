package mesh

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type relayFrame struct {
	Envelope signedMessage `json:"envelope"`
	Path     []string      `json:"path"`
}
type signedAck struct {
	MessageID string `json:"message_id"`
	FromHub   string `json:"from_hub"`
	ToHub     string `json:"to_hub"`
	Hash      string `json:"hash"`
	Status    string `json:"status"`
	Signature []byte `json:"signature"`
}

func messageBytes(m Message) []byte {
	b, _ := json.Marshal(m)
	return append([]byte("agi-hub-mail-v1\x00"), b...)
}
func messageHash(m Message) string {
	sum := sha256.Sum256(messageBytes(m))
	return hex.EncodeToString(sum[:])
}
func ackBytes(ack signedAck) []byte {
	ack.Signature = nil
	b, _ := json.Marshal(ack)
	return append([]byte("agi-hub-ack-v1\x00"), b...)
}
func (s *Service) signMessage(m Message) signedMessage {
	return signedMessage{Message: m, PublicKey: s.key.Public().(ed25519.PublicKey), Signature: ed25519.Sign(s.key, messageBytes(m))}
}
func validateMessage(m Message) error {
	if len(m.ID) == 0 || len(m.ID) > 200 || len(m.FromHub) != 64 || len(m.ToHub) != 64 || m.FromHub == m.ToHub || !validEndpoint(m.FromEndpoint) || !validEndpoint(m.ToEndpoint) || len(m.Payload) > maxPayload || !json.Valid(m.Payload) {
		return errors.New("invalid message or payload exceeds limit")
	}
	for _, c := range m.ID {
		if c < '!' || c > '~' {
			return errors.New("invalid message ID")
		}
	}
	if !m.ExpiresAt.After(time.Now()) || m.ExpiresAt.After(time.Now().Add(7*24*time.Hour)) {
		return errors.New("message expired or lifetime exceeds seven days")
	}
	return nil
}

func (s *Service) Send(ctx context.Context, m Message) (Receipt, error) {
	if m.FromHub == "" {
		m.FromHub = s.id
	}
	if m.FromHub != s.id {
		return Receipt{}, errors.New("cannot send as another Hub")
	}
	senderAllowed := s.hasExport(m.FromEndpoint)
	s.mu.Lock()
	if existing, ok := s.disk.Outbox[m.ID]; ok {
		s.mu.Unlock()
		if existing.Hash != messageHash(m) {
			return Receipt{}, errors.New("message ID/content conflict")
		}
		return existing.Receipt, nil
	}
	if !senderAllowed {
		s.mu.Unlock()
		return Receipt{}, errors.New("local sender endpoint is not exported")
	}
	if !s.disk.Config.Enabled || s.server == nil {
		s.mu.Unlock()
		return Receipt{}, errors.New("LAN transport is disabled")
	}
	if _, ok := s.disk.Peers[m.ToHub]; !ok {
		s.mu.Unlock()
		return Receipt{}, errors.New("destination is not paired")
	}
	active := 0
	for _, record := range s.disk.Outbox {
		if record.Receipt.Status == "queued" {
			active++
		}
	}
	if active >= maxQueue {
		s.mu.Unlock()
		return Receipt{}, errors.New("durable outbox capacity reached")
	}
	if err := validateMessage(m); err != nil {
		s.mu.Unlock()
		return Receipt{}, err
	}
	// Own bytes: callers may reuse their original json.RawMessage after Send.
	m.Payload = append(json.RawMessage{}, m.Payload...)
	record := outboxRecord{Envelope: s.signMessage(m), Receipt: Receipt{MessageID: m.ID, Status: "queued"}, Hash: messageHash(m)}
	s.disk.Outbox[m.ID] = record
	if err := s.persistLocked(); err != nil {
		delete(s.disk.Outbox, m.ID)
		s.mu.Unlock()
		return Receipt{}, err
	}
	s.mu.Unlock()
	s.changed()
	// Submission succeeds once durable; network or destination persistence
	// failures leave exactly the same signed message queued for reconnect.
	s.flushMu.Lock()
	s.sendOne(ctx, m.ID)
	s.flushMu.Unlock()
	s.mu.Lock()
	receipt := s.disk.Outbox[m.ID].Receipt
	s.mu.Unlock()
	return receipt, nil
}

func (s *Service) flush(ctx context.Context) {
	if !s.flushMu.TryLock() {
		return
	}
	defer s.flushMu.Unlock()
	s.mu.Lock()
	ids := []string{}
	for id, record := range s.disk.Outbox {
		if record.Receipt.Status == "queued" {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	// Bound work per pass; persisted messages survive longer outages.
	if len(ids) > 32 {
		ids = ids[:32]
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		s.sendOne(ctx, id)
	}
}
func (s *Service) sendOne(ctx context.Context, id string) {
	s.mu.Lock()
	record, ok := s.disk.Outbox[id]
	if !ok || record.Receipt.Status != "queued" {
		s.mu.Unlock()
		return
	}
	dest, paired := s.disk.Peers[record.Envelope.ToHub]
	s.mu.Unlock()
	senderAllowed := s.hasExport(record.Envelope.FromEndpoint)
	var ack signedAck
	var err error
	if !record.Envelope.ExpiresAt.After(time.Now()) {
		err = &networkError{Code: 410, Message: "message expired"}
	} else if !paired {
		err = &networkError{Code: 403, Message: "destination pairing revoked"}
	} else if !senderAllowed {
		err = &networkError{Code: 403, Message: "local sender endpoint is no longer exported"}
	} else {
		ack, err = s.forward(ctx, relayFrame{Envelope: record.Envelope, Path: []string{s.id}})
		if err == nil && len(dest.PublicKey) != ed25519.PublicKeySize {
			err = errors.New("invalid destination identity")
		}
		if err == nil && (ack.MessageID != id || ack.FromHub != s.id || ack.ToHub != dest.HubID || ack.Hash != messageHash(record.Envelope.Message) || ack.Status != "received" || !ed25519.Verify(dest.PublicKey, ackBytes(ack), ack.Signature)) {
			err = errors.New("destination acknowledgement authentication failed")
		}
	}
	s.mu.Lock()
	current := s.disk.Outbox[id]
	updated := current
	if err == nil {
		updated.Receipt.Status = "received"
		updated.Receipt.Error = ""
		updated.Envelope = signedMessage{}
	} else {
		// Only locally established expiry/revocation is permanent. A relay may
		// return arbitrary HTTP errors, so unsigned remote errors cannot erase mail.
		if !record.Envelope.ExpiresAt.After(time.Now()) || !paired || !senderAllowed {
			updated.Receipt.Status = "failed"
			updated.Envelope = signedMessage{}
		}
		updated.Receipt.Error = boundedError(err)
	}
	s.disk.Outbox[id] = updated
	if persistErr := s.persistLocked(); persistErr != nil {
		s.disk.Outbox[id] = current
	}
	s.mu.Unlock()
	s.changed()
}
func boundedError(err error) string {
	value := err.Error()
	if len(value) > 300 {
		return value[:300]
	}
	return value
}

func (s *Service) forward(ctx context.Context, frame relayFrame) (signedAck, error) {
	s.mu.Lock()
	s.rememberTransportViewLocked()
	path, _ := s.routeLocked(frame.Envelope.ToHub, frame.Path)
	if len(path) < 2 {
		s.mu.Unlock()
		return signedAck{}, errors.New("no currently reachable route")
	}
	peer, ok := s.disk.Peers[path[1]]
	s.mu.Unlock()
	if !ok {
		return signedAck{}, errors.New("next hop is not paired")
	}
	var ack signedAck
	err := s.request(ctx, peer, "/v1/mail", frame, &ack)
	if err != nil {
		s.mu.Lock()
		delete(s.online, peer.HubID)
		notify := s.transportChangedLocked()
		s.mu.Unlock()
		if notify {
			s.changed()
		}
	}
	return ack, err
}

func permanent(code int, message string) error { return &networkError{Code: code, Message: message} }
func (s *Service) hasExport(endpoint string) bool {
	for _, agent := range s.exportedAgents() {
		if agent.ID == endpoint {
			return true
		}
	}
	return false
}
func (s *Service) deliver(ctx context.Context, networkPeer string, frame relayFrame) (signedAck, error) {
	m := frame.Envelope.Message
	if len(frame.Path) == 0 || len(frame.Path) >= maxHops || frame.Path[0] != m.FromHub || frame.Path[len(frame.Path)-1] != networkPeer {
		return signedAck{}, permanent(400, "invalid relay path")
	}
	seen := map[string]bool{s.id: true}
	for _, id := range frame.Path {
		if seen[id] {
			return signedAck{}, permanent(400, "relay loop")
		}
		seen[id] = true
	}
	if err := validateMessage(m); err != nil {
		return signedAck{}, permanent(400, err.Error())
	}
	if len(frame.Envelope.PublicKey) != ed25519.PublicKeySize || hubID(frame.Envelope.PublicKey) != m.FromHub || !ed25519.Verify(frame.Envelope.PublicKey, messageBytes(m), frame.Envelope.Signature) {
		return signedAck{}, permanent(403, "origin signature invalid")
	}
	s.mu.Lock()
	_, hopTrusted := s.disk.Peers[networkPeer]
	origin, originTrusted := s.disk.Peers[m.FromHub]
	enabled := s.disk.Config.Enabled && s.server != nil
	s.mu.Unlock()
	if !enabled || !hopTrusted {
		return signedAck{}, permanent(403, "incoming hop not authorized")
	}
	if m.ToHub != s.id {
		frame.Path = append(append([]string{}, frame.Path...), s.id)
		return s.forward(ctx, frame)
	}
	s.receiveMu.Lock()
	defer s.receiveMu.Unlock()
	// Re-evaluate trust and exports after waiting behind another durable write.
	s.mu.Lock()
	origin, originTrusted = s.disk.Peers[m.FromHub]
	_, hopTrusted = s.disk.Peers[networkPeer]
	enabled = s.disk.Config.Enabled && s.server != nil
	s.mu.Unlock()
	if !enabled || !hopTrusted || !originTrusted || !bytes.Equal(origin.PublicKey, frame.Envelope.PublicKey) {
		return signedAck{}, permanent(403, "origin is not paired with destination")
	}
	exported := s.hasExport(m.ToEndpoint)
	if !exported {
		return signedAck{}, permanent(403, "destination endpoint is not exported")
	}
	hash := messageHash(m)
	key := m.FromHub + "/" + m.ID
	s.mu.Lock()
	if err := s.expireInboxLocked(time.Now()); err != nil {
		s.mu.Unlock()
		return signedAck{}, fmt.Errorf("persist expired inbox tombstones: %w", err)
	}
	record, exists := s.disk.Inbox[key]
	if exists && record.Hash != hash {
		s.mu.Unlock()
		return signedAck{}, permanent(409, "message ID/content conflict")
	}
	if exists && record.Received {
		s.mu.Unlock()
		return s.ack(m, hash), nil
	}
	if exists && record.Expired {
		s.mu.Unlock()
		return signedAck{}, permanent(410, "message expired before durable receiver acceptance")
	}
	if !exists {
		active := 0
		for _, record := range s.disk.Inbox {
			if !record.Received && !record.Expired {
				active++
			}
		}
		if active >= maxQueue {
			s.mu.Unlock()
			return signedAck{}, errors.New("durable inbox capacity reached")
		}
		record = inboxRecord{Envelope: frame.Envelope, Hash: hash}
		s.disk.Inbox[key] = record
		if err := s.persistLocked(); err != nil {
			delete(s.disk.Inbox, key)
			s.mu.Unlock()
			return signedAck{}, fmt.Errorf("persist incoming mail: %w", err)
		}
	}
	s.mu.Unlock()
	if s.opts.Receive == nil {
		return signedAck{}, errors.New("durable receiver unavailable")
	}
	if err := s.opts.Receive(ctx, m); err != nil {
		return signedAck{}, fmt.Errorf("durable receiver: %w", err)
	}
	s.mu.Lock()
	record.Received = true
	pendingEnvelope := record.Envelope
	record.Envelope = signedMessage{}
	s.disk.Inbox[key] = record
	if err := s.persistLocked(); err != nil {
		record.Received = false
		record.Envelope = pendingEnvelope
		s.disk.Inbox[key] = record
		s.mu.Unlock()
		return signedAck{}, err
	}
	s.mu.Unlock()
	s.changed()
	return s.ack(m, hash), nil
}
func (s *Service) ack(m Message, hash string) signedAck {
	ack := signedAck{MessageID: m.ID, FromHub: m.FromHub, ToHub: m.ToHub, Hash: hash, Status: "received"}
	ack.Signature = ed25519.Sign(s.key, ackBytes(ack))
	return ack
}
