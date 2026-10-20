package manager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Publish persists one addressed message. It never creates a Task, chooses a
// helper, schedules follow-up work, or manufactures a response on the sender's behalf.
// The API must authenticate sender and enforce its user-configured recipient ACL.
func (b *Broker) Publish(sender string, request MailSubmission) (Envelope, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.writable(); err != nil {
		return Envelope{}, err
	}
	endpoint, ok := b.state.Endpoints[sender]
	if !ok || !endpoint.Enabled || endpoint.MailboxProtocolVersion != 1 {
		return Envelope{}, errors.New("sender requires an enabled mailbox protocol 1 endpoint")
	}
	for label, value := range map[string]string{"message ID": request.ID, "recipient": request.Recipient} {
		if err := checkIdentifier(label, value); err != nil {
			return Envelope{}, err
		}
	}
	if request.Kind != "request" && request.Kind != "reply" && request.Kind != "status" && request.Kind != "result" {
		return Envelope{}, errors.New("mail kind must be request, reply, status, or result")
	}
	if strings.TrimSpace(request.Content) == "" || len(request.Content) > 64<<10 {
		return Envelope{}, errors.New("mail content must be nonempty and at most 64 KiB")
	}
	if recipient, ok := b.state.Endpoints[request.Recipient]; !ok || !recipient.Enabled || recipient.MailboxProtocolVersion != 1 {
		return Envelope{}, errors.New("recipient requires an enabled mailbox protocol 1 endpoint")
	}
	if request.CorrelationID == "" {
		request.CorrelationID = request.ID
	}
	if err := checkIdentifier("correlation ID", request.CorrelationID); err != nil {
		return Envelope{}, err
	}
	conversation, exists := b.state.MailConversations[request.CorrelationID]
	if exists {
		if conversation.Federation != nil {
			return Envelope{}, errors.New("federated conversations require a qualified remote recipient")
		}
		if !conversation.Participants[sender] {
			return Envelope{}, errors.New("sender is not a participant in this mail conversation")
		}
		if request.Deadline != "" && request.Deadline != conversation.Deadline || request.MaxMessages != 0 && request.MaxMessages != conversation.MaxMessages || request.OriginRef != "" && request.OriginRef != conversation.RootOrigin.RequestRef {
			return Envelope{}, errors.New("mail cannot replace the original context, deadline, or message limit")
		}
	} else {
		if request.CorrelationID != request.ID {
			return Envelope{}, errors.New("a new mail conversation must use its first message ID as correlation_id")
		}
		if strings.TrimSpace(request.OriginRef) == "" || len(request.OriginRef) > 512 {
			return Envelope{}, errors.New("new mail conversation requires origin_ref of at most 512 bytes")
		}
		if request.MaxMessages == 0 {
			request.MaxMessages = 16
		}
		if request.MaxMessages < 1 || request.MaxMessages > 64 {
			return Envelope{}, errors.New("max_messages must be between 1 and 64")
		}
		conversation = MailConversation{ID: request.CorrelationID, RootOrigin: OriginContext{Endpoint: sender, DeviceID: endpoint.DeviceID, SessionID: endpoint.SessionID, RequestRef: request.OriginRef}, Participants: map[string]bool{sender: true}, MaxMessages: request.MaxMessages, Deadline: request.Deadline}
	}
	request.Deadline, request.MaxMessages, request.OriginRef = conversation.Deadline, conversation.MaxMessages, conversation.RootOrigin.RequestRef
	encoded, _ := json.Marshal(request)
	digest := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(digest[:])
	if old, exists := b.state.Messages[request.ID]; exists {
		if !old.Mailbox || old.Sender != sender || old.MailFingerprint != fingerprint {
			return Envelope{}, errors.New("message ID was already used with different content")
		}
		return cloneEnvelope(old), nil
	}
	if request.Recipient == conversation.RootOrigin.Endpoint && !matchesRootSession(b.state.Endpoints[request.Recipient], conversation.RootOrigin) {
		return Envelope{}, errors.New("selected original endpoint now binds a different native session; original session context was preserved and no delivery was attempted")
	}
	if conversation.Closed {
		return Envelope{}, errors.New("mail conversation is closed")
	}
	deadline, err := time.Parse(time.RFC3339, conversation.Deadline)
	if err != nil || !time.Now().Before(deadline) {
		return Envelope{}, errors.New("mail requires a future RFC3339 deadline")
	}
	if conversation.UsedMessages >= conversation.MaxMessages {
		return Envelope{}, errors.New("mail conversation message limit is exhausted")
	}
	if request.ReplyTo != "" {
		previous, ok := b.state.Messages[request.ReplyTo]
		if !ok || !previous.Mailbox || previous.CorrelationID != request.CorrelationID || (previous.Sender != sender && previous.Recipient != sender) {
			return Envelope{}, errors.New("reply_to must reference a visible message in this conversation")
		}
	}
	origin := conversation.RootOrigin
	envelope := Envelope{Mailbox: true, MailFingerprint: fingerprint, ID: request.ID, CorrelationID: request.CorrelationID, RootOrigin: &origin, ReplyTo: request.ReplyTo, Kind: request.Kind, Sender: sender, Recipient: request.Recipient, Mode: "queue", Content: request.Content, Delivery: "pending"}
	next := cloneSnapshot(b.state)
	if next.Version < 3 {
		next.Version = 3 // older binaries must reject rather than drop mailbox fields
	}
	conversation.Participants = cloneParticipants(conversation.Participants)
	conversation.Participants[sender], conversation.Participants[request.Recipient] = true, true
	conversation.UsedMessages++
	next.MailConversations[conversation.ID] = conversation
	next.Messages[envelope.ID] = envelope
	if err := b.commit(next); err != nil {
		return Envelope{}, err
	}
	return cloneEnvelope(envelope), nil
}

// NextMail reserves transport capacity, not a business task or assistant turn.
// A native client acknowledges only after its real transport operation ends.
func (b *Broker) NextMail(endpoint string) (*Envelope, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.writable(); err != nil {
		return nil, err
	}
	registered, exists := b.state.Endpoints[endpoint]
	if !exists {
		return nil, errors.New("endpoint is not registered")
	}
	if !registered.Enabled || registered.MailboxProtocolVersion != 1 || deliveryInFlight(b.state, endpoint) {
		return nil, nil
	}
	for _, id := range b.order {
		message := b.state.Messages[id]
		if !message.Mailbox || message.Recipient != endpoint || message.Delivery != "pending" {
			continue
		}
		conversation, exists := b.state.MailConversations[message.CorrelationID]
		if !exists {
			return nil, errors.New("message references missing mail conversation")
		}
		if message.Recipient == conversation.RootOrigin.Endpoint && !matchesRootSession(registered, conversation.RootOrigin) {
			return nil, errors.New("selected original endpoint now binds a different native session; pending mail was not delivered")
		}
		deadline, err := time.Parse(time.RFC3339, conversation.Deadline)
		if err != nil {
			return nil, errors.New("stored mail deadline is invalid")
		}
		if conversation.Closed || !time.Now().Before(deadline) {
			next := cloneSnapshot(b.state)
			message.Delivery = "failed"
			message.Detail = "mail conversation closed or deadline expired before delivery"
			next.Messages[id] = message
			if err := b.commit(next); err != nil {
				return nil, err
			}
			continue
		}
		if message.Kind != "status" {
			session := b.state.Sessions[endpoint]
			idle := registered.IdleWakeVerified && session.State == "idle"
			busy := registered.MailboxWhileBusyVerified && session.State == "busy"
			if !idle && !busy {
				continue
			}
		}
		next := cloneSnapshot(b.state)
		message.Delivery = "dispatching"
		next.Messages[id] = message
		if err := b.commit(next); err != nil {
			return nil, err
		}
		result := cloneEnvelope(message)
		return &result, nil
	}
	return nil, nil
}

// CancelMail withdraws messages known never to have been dispatched. It makes
// no claim that previously delivered or uncertain native actions were stopped.
func (b *Broker) CancelMail(sender, correlationID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.writable(); err != nil {
		return err
	}
	conversation, exists := b.state.MailConversations[correlationID]
	if !exists || conversation.RootOrigin.Endpoint != sender {
		return errors.New("only the original endpoint may close this mail conversation")
	}
	if conversation.Closed {
		return nil
	}
	next := cloneSnapshot(b.state)
	conversation.Closed = true
	next.MailConversations[correlationID] = conversation
	for id, message := range next.Messages {
		if message.Mailbox && message.CorrelationID == correlationID && message.Delivery == "pending" {
			message.Delivery = "failed"
			message.Detail = "original endpoint closed mail conversation before delivery"
			next.Messages[id] = message
		}
	}
	return b.commit(next)
}

func cloneParticipants(original map[string]bool) map[string]bool {
	result := make(map[string]bool, len(original))
	for id, value := range original {
		result[id] = value
	}
	return result
}

func matchesRootSession(endpoint Endpoint, origin OriginContext) bool {
	return endpoint.DeviceID == origin.DeviceID && endpoint.SessionID == origin.SessionID
}
func cloneEnvelope(original Envelope) Envelope {
	if original.Network != nil {
		network := *original.Network
		original.Network = &network
	}
	if original.RootOrigin != nil {
		origin := *original.RootOrigin
		original.RootOrigin = &origin
	}
	return original
}
