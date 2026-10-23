package manager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"agihub/internal/language"
)

// FederatedContext is deliberately pairwise in protocol 1. Permits are divided
// between the two senders, so disconnected/concurrent writers cannot multiply
// the global message budget. A new recipient uses a new conversation.
type FederatedContext struct {
	OriginHub      string `json:"origin_hub"`
	OriginEndpoint string `json:"origin_endpoint"`
	PeerHub        string `json:"peer_hub"`
	PeerEndpoint   string `json:"peer_endpoint"`
}

// NetworkMail contains no native session address or filesystem attachment.
// The mesh authenticates its sender and destination before ImportRemote.
type NetworkMail struct {
	FederatedContext
	CommunicationLanguage string `json:"communication_language,omitempty"`
	ID                    string `json:"id"`
	SourceID              string `json:"source_id"`
	CorrelationID         string `json:"correlation_id"`
	FromHub               string `json:"from_hub"`
	ToHub                 string `json:"to_hub"`
	Sender                string `json:"sender"`
	Recipient             string `json:"recipient"`
	Kind                  string `json:"kind"`
	Content               string `json:"content"`
	OriginRef             string `json:"origin_ref"`
	ReplyTo               string `json:"reply_to,omitempty"`
	Deadline              string `json:"deadline"`
	MaxMessages           int    `json:"max_messages"`
	Sequence              int    `json:"sequence"`
}

func validHubID(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 32 && id == strings.ToLower(id)
}

func RemoteAddress(hub, endpoint string) string { return hub + "/" + endpoint }

func ParseRemoteAddress(address string) (hub, endpoint string, err error) {
	hub, endpoint, ok := strings.Cut(address, "/")
	if !ok || !validHubID(hub) || strings.ContainsAny(endpoint, "/\x00\r\n") || checkIdentifier("endpoint", endpoint) != nil {
		return "", "", errors.New("remote recipient must be <64-hex Hub ID>/<endpoint>")
	}
	return hub, endpoint, nil
}

func networkID(hub, sourceID string) string {
	hash := sha256.Sum256([]byte(hub + "\x00" + sourceID))
	return "lan-" + hex.EncodeToString(hash[:])
}

func fingerprint(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// PublishRemote journals before exposing the message to network transport.
// The caller must authenticate sender and enforce explicit sharing/pairing ACLs.
func (b *Broker) PublishRemote(localHub, sender string, request MailSubmission, communicationLanguage string) (Envelope, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.writable(); err != nil {
		return Envelope{}, err
	}
	remoteHub, recipient, err := ParseRemoteAddress(request.Recipient)
	if err != nil || !validHubID(localHub) || remoteHub == localHub {
		return Envelope{}, errors.New("remote message requires two distinct valid Hub identities")
	}
	endpoint, ok := b.state.Endpoints[sender]
	if !ok || !endpoint.Enabled || endpoint.MailboxProtocolVersion != 1 {
		return Envelope{}, errors.New("sender requires enabled mailbox protocol 1")
	}
	if err := checkIdentifier("message ID", request.ID); err != nil {
		return Envelope{}, err
	}
	id := networkID(localHub, request.ID)
	if request.CorrelationID == "" || request.CorrelationID == request.ID {
		request.CorrelationID = id
	}
	conversation, exists := b.state.MailConversations[request.CorrelationID]
	if exists {
		if conversation.Federation == nil || !conversation.Participants[sender] {
			return Envelope{}, errors.New("not a participant in this federated conversation")
		}
		if request.Deadline != "" && request.Deadline != conversation.Deadline || request.MaxMessages != 0 && request.MaxMessages != conversation.MaxMessages || request.OriginRef != "" && request.OriginRef != conversation.RootOrigin.RequestRef {
			return Envelope{}, errors.New("cannot replace original context, deadline or budget")
		}
	} else {
		if request.CorrelationID != id {
			return Envelope{}, errors.New("new conversation must use its first message ID")
		}
		if strings.TrimSpace(request.OriginRef) == "" || len(request.OriginRef) > 512 {
			return Envelope{}, errors.New("new conversation requires origin_ref, at most 512 bytes")
		}
		if request.MaxMessages == 0 {
			request.MaxMessages = 16
		}
		if request.Deadline == "" {
			request.Deadline = time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339)
		}
		conversation = MailConversation{
			ID: id, RootOrigin: OriginContext{Endpoint: sender, DeviceID: endpoint.DeviceID, SessionID: endpoint.SessionID, RequestRef: request.OriginRef},
			Participants: map[string]bool{sender: true, request.Recipient: true}, MaxMessages: request.MaxMessages, Deadline: request.Deadline,
			Federation: &FederatedContext{OriginHub: localHub, OriginEndpoint: sender, PeerHub: remoteHub, PeerEndpoint: recipient},
		}
	}
	wire := NetworkMail{FederatedContext: *conversation.Federation, ID: id, SourceID: request.ID, CorrelationID: conversation.ID,
		FromHub: localHub, ToHub: remoteHub, Sender: sender, Recipient: recipient, Kind: request.Kind, Content: request.Content, ReplyTo: request.ReplyTo,
		OriginRef: conversation.RootOrigin.RequestRef, Deadline: conversation.Deadline, MaxMessages: conversation.MaxMessages}
	if old, ok := b.state.Messages[id]; ok {
		if old.Network == nil {
			return Envelope{}, errors.New("message ID collision")
		}
		wire.Sequence = old.Network.Sequence
		// A retry acknowledges the original immutable message, even if the
		// directory's expression preference changed since the first send.
		wire.CommunicationLanguage = old.Network.CommunicationLanguage
		if fingerprint(wire) != fingerprint(*old.Network) {
			return Envelope{}, errors.New("message ID already used with different content")
		}
		return cloneEnvelope(old), nil
	}
	wire.CommunicationLanguage, err = language.Normalize(communicationLanguage)
	if err != nil {
		return Envelope{}, err
	}
	wire.Sequence = 1
	for _, old := range b.state.Messages {
		if old.Network != nil && old.CorrelationID == conversation.ID && old.Network.FromHub == localHub && old.Network.Sequence >= wire.Sequence {
			wire.Sequence = old.Network.Sequence + 1
		}
	}
	if err := validateNetworkMail(wire); err != nil {
		return Envelope{}, err
	}
	if conversation.Closed {
		return Envelope{}, errors.New("conversation is closed")
	}
	if wire.ReplyTo != "" {
		old, ok := b.state.Messages[wire.ReplyTo]
		if !ok || old.Network == nil || old.CorrelationID != wire.CorrelationID || old.Sender != sender && old.Recipient != sender {
			return Envelope{}, errors.New("reply_to must reference visible mail in this conversation")
		}
	}
	if conversation.RootOrigin.Endpoint == sender && !matchesRootSession(endpoint, conversation.RootOrigin) {
		return Envelope{}, errors.New("original endpoint now binds another native session")
	}
	envelope := Envelope{Network: &wire, Mailbox: true, MailFingerprint: fingerprint(wire), ID: id, CorrelationID: conversation.ID, RootOrigin: &conversation.RootOrigin,
		ReplyTo: wire.ReplyTo, Kind: wire.Kind, Sender: sender, Recipient: request.Recipient, Mode: "queue", Content: wire.Content, Delivery: "network_pending"}
	envelope.CommunicationLanguage = wire.CommunicationLanguage
	next := cloneSnapshot(b.state)
	next.Version = 4
	conversation.UsedMessages++
	next.MailConversations[conversation.ID] = conversation
	next.Messages[id] = envelope
	if err := b.commit(next); err != nil {
		return Envelope{}, err
	}
	return cloneEnvelope(envelope), nil
}

func validateNetworkMail(m NetworkMail) error {
	if normalized, err := language.Normalize(m.CommunicationLanguage); err != nil || m.CommunicationLanguage != "" && normalized != m.CommunicationLanguage {
		return errors.New("invalid network communication language")
	}
	if !validHubID(m.FromHub) || !validHubID(m.ToHub) || m.FromHub == m.ToHub || m.ID != networkID(m.FromHub, m.SourceID) || checkIdentifier("source ID", m.SourceID) != nil {
		return errors.New("invalid network message identity")
	}
	if _, _, err := ParseRemoteAddress(RemoteAddress(m.FromHub, m.Sender)); err != nil {
		return err
	}
	if _, _, err := ParseRemoteAddress(RemoteAddress(m.ToHub, m.Recipient)); err != nil {
		return err
	}
	origin := m.FromHub == m.OriginHub && m.Sender == m.OriginEndpoint && m.ToHub == m.PeerHub && m.Recipient == m.PeerEndpoint
	peer := m.FromHub == m.PeerHub && m.Sender == m.PeerEndpoint && m.ToHub == m.OriginHub && m.Recipient == m.OriginEndpoint
	if !origin && !peer {
		return errors.New("federation protocol 1 supports a fixed pair of addressed endpoints")
	}
	if m.MaxMessages < 1 || m.MaxMessages > 64 {
		return errors.New("max_messages must be between 1 and 64")
	}
	permits := m.MaxMessages / 2
	if origin {
		permits = (m.MaxMessages + 1) / 2
	}
	if m.Sequence < 1 || m.Sequence > permits {
		return errors.New("this sender's federated message permits are exhausted")
	}
	if m.Kind != "request" && m.Kind != "reply" && m.Kind != "status" && m.Kind != "result" {
		return errors.New("invalid mail kind")
	}
	if strings.TrimSpace(m.Content) == "" || len(m.Content) > 64<<10 || len(m.OriginRef) > 512 || strings.TrimSpace(m.OriginRef) == "" {
		return errors.New("invalid content or original reference")
	}
	deadline, err := time.Parse(time.RFC3339, m.Deadline)
	if err != nil || !time.Now().Before(deadline) || deadline.After(time.Now().Add(24*time.Hour)) {
		return errors.New("federated deadline must be future and within 24 hours")
	}
	if !strings.HasPrefix(m.CorrelationID, "lan-") || len(m.CorrelationID) != 68 {
		return errors.New("invalid federated correlation ID")
	}
	return nil
}

// ImportRemote is called only after mesh origin authentication and local export
// authorization. It does not mark a native session idle or synthesize a Report.
func (b *Broker) ImportRemote(localHub string, m NetworkMail) (Envelope, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.writable(); err != nil {
		return Envelope{}, err
	}
	if m.ToHub != localHub {
		return Envelope{}, errors.New("network destination mismatch")
	}
	if old, ok := b.state.Messages[m.ID]; ok {
		if old.Network == nil || fingerprint(*old.Network) != fingerprint(m) {
			return Envelope{}, errors.New("conflicting network message replay")
		}
		return cloneEnvelope(old), nil
	}
	if err := validateNetworkMail(m); err != nil {
		return Envelope{}, err
	}
	recipient, ok := b.state.Endpoints[m.Recipient]
	if !ok || !recipient.Enabled || recipient.MailboxProtocolVersion != 1 {
		return Envelope{}, errors.New("local recipient is not an enabled mailbox")
	}
	conversation, exists := b.state.MailConversations[m.CorrelationID]
	if !exists {
		if m.FromHub != m.OriginHub || m.Sequence != 1 || m.CorrelationID != m.ID || m.ReplyTo != "" {
			return Envelope{}, errors.New("initial federated message has not arrived")
		}
		context := m.FederatedContext
		conversation = MailConversation{ID: m.CorrelationID, Federation: &context, RootOrigin: OriginContext{Endpoint: RemoteAddress(m.OriginHub, m.OriginEndpoint), DeviceID: m.OriginHub, RequestRef: m.OriginRef},
			Participants: map[string]bool{m.Recipient: true, RemoteAddress(m.FromHub, m.Sender): true}, MaxMessages: m.MaxMessages, Deadline: m.Deadline}
	} else if conversation.Federation == nil || *conversation.Federation != m.FederatedContext || conversation.MaxMessages != m.MaxMessages || conversation.Deadline != m.Deadline || conversation.RootOrigin.RequestRef != m.OriginRef {
		return Envelope{}, errors.New("remote mail cannot replace conversation context")
	}
	if conversation.Closed {
		return Envelope{}, errors.New("conversation closed locally; no native cancellation implied")
	}
	if m.Recipient == conversation.RootOrigin.Endpoint && !matchesRootSession(recipient, conversation.RootOrigin) {
		return Envelope{}, errors.New("original native session changed")
	}
	for _, old := range b.state.Messages {
		if old.Network != nil && old.CorrelationID == m.CorrelationID && old.Network.FromHub == m.FromHub && old.Network.Sequence == m.Sequence {
			return Envelope{}, errors.New("sender reused a conversation permit")
		}
	}
	if m.ReplyTo != "" {
		old, ok := b.state.Messages[m.ReplyTo]
		if !ok || old.CorrelationID != m.CorrelationID || old.Network == nil {
			return Envelope{}, errors.New("referenced mail has not arrived")
		}
	}
	envelope := Envelope{Network: &m, Mailbox: true, MailFingerprint: fingerprint(m), ID: m.ID, CorrelationID: m.CorrelationID, RootOrigin: &conversation.RootOrigin,
		ReplyTo: m.ReplyTo, Kind: m.Kind, Sender: RemoteAddress(m.FromHub, m.Sender), Recipient: m.Recipient, Mode: "queue", Content: m.Content, Delivery: "pending"}
	envelope.CommunicationLanguage = m.CommunicationLanguage
	if envelope.CommunicationLanguage == "" {
		envelope.CommunicationLanguage = language.Effective(recipient.Driver, recipient.CommunicationLanguage)
	}
	next := cloneSnapshot(b.state)
	next.Version = 4
	conversation.UsedMessages++
	next.MailConversations[conversation.ID] = conversation
	next.Messages[m.ID] = envelope
	if err := b.commit(next); err != nil {
		return Envelope{}, err
	}
	return cloneEnvelope(envelope), nil
}

// NetworkReceipt describes Hub persistence only. Native delivery/execution
// stays at the destination and is reported by an explicitly addressed message.
func (b *Broker) NetworkReceipt(id, status string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.writable(); err != nil {
		return err
	}
	m, ok := b.state.Messages[id]
	if !ok || m.Network == nil {
		return errors.New("unknown network message")
	}
	if m.Delivery != "network_pending" {
		return nil
	}
	switch status {
	case "received":
		m.Delivery = "remote_received"
		m.Detail = "destination Hub persisted message; native execution is unverified"
	case "failed":
		m.Delivery = "failed"
		m.Detail = "network message expired or transport rejected it; inspect remote state"
	default:
		return nil
	}
	next := cloneSnapshot(b.state)
	next.Messages[id] = m
	return b.commit(next)
}
