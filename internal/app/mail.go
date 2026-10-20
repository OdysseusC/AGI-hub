package app

import (
	"errors"
	"net/http"
	"sort"
	"time"

	"agihub/internal/manager"
)

func mailRoute(path string) bool {
	switch path {
	case "/v1/mail/send", "/v1/mail/inbox", "/v1/mail/delivery", "/v1/mail/state", "/v1/mail/cancel":
		return true
	}
	return false
}

func (s *Server) mail(w http.ResponseWriter, r *http.Request, endpoint string) (any, error) {
	self, ok := s.Broker.Snapshot().Endpoints[endpoint]
	if !ok || !self.Enabled || self.MailboxProtocolVersion != 1 {
		return nil, errors.New("mailbox requires an enabled endpoint with mailbox_protocol_version=1")
	}
	switch {
	case r.URL.Path == "/v1/mail/send" && r.Method == http.MethodPost:
		var in manager.MailSubmission
		if err := decode(w, r, &in); err != nil {
			return nil, err
		}
		if _, _, remoteErr := manager.ParseRemoteAddress(in.Recipient); remoteErr == nil {
			var message manager.Envelope
			err := s.withClientLease(r, endpoint, func() error {
				var err error
				message, err = s.publishRemote(endpoint, in)
				return err
			})
			return message, err
		}
		allowed := false
		for _, binding := range s.Config.Bindings {
			if binding.Endpoint.ID == endpoint {
				for _, id := range binding.AllowedPeers {
					allowed = allowed || id == in.Recipient
				}
			}
		}
		if !allowed {
			return nil, errors.New("recipient is not in this endpoint's allowed_peers")
		}
		if (in.CorrelationID == "" || in.CorrelationID == in.ID) && in.Deadline == "" {
			state := s.Broker.Snapshot()
			if old, exists := state.Messages[in.ID]; exists && old.Mailbox {
				in.Deadline = state.MailConversations[old.CorrelationID].Deadline
			} else {
				in.Deadline = time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339)
			}
		}
		var message manager.Envelope
		err := s.withClientLease(r, endpoint, func() error {
			var err error
			message, err = s.Broker.Publish(endpoint, in)
			return err
		})
		return message, err
	case r.URL.Path == "/v1/mail/inbox" && r.Method == http.MethodPost:
		wait, err := inboxWait(r)
		if err != nil {
			return nil, err
		}
		return s.inbox(r, endpoint, wait, true)
	case r.URL.Path == "/v1/mail/delivery" && r.Method == http.MethodPost:
		var in struct {
			MessageID string `json:"message_id"`
			Outcome   string `json:"outcome"`
			Detail    string `json:"detail"`
		}
		if err := decode(w, r, &in); err != nil {
			return nil, err
		}
		message, exists := s.Broker.Snapshot().Messages[in.MessageID]
		if !exists || !message.Mailbox || message.Recipient != endpoint {
			return nil, errors.New("mail does not belong to this mailbox")
		}
		return map[string]bool{"ok": true}, s.withClientLease(r, endpoint, func() error {
			return s.Broker.Delivery(in.MessageID, in.Outcome, in.Detail)
		})
	case r.URL.Path == "/v1/mail/state" && r.Method == http.MethodGet:
		state := s.Broker.Snapshot()
		id := r.URL.Query().Get("correlation_id")
		conversation, exists := state.MailConversations[id]
		if !exists || !conversation.Participants[endpoint] {
			return nil, errors.New("mail conversation is not visible to this endpoint")
		}
		messages := []mailStatus{}
		for _, message := range state.Messages {
			if message.Mailbox && message.CorrelationID == id && (message.Sender == endpoint || message.Recipient == endpoint) {
				entry := mailStatus{ID: message.ID, Sender: message.Sender, Recipient: message.Recipient, Kind: message.Kind, ReplyTo: message.ReplyTo, Delivery: message.Delivery, Detail: message.Detail}
				if message.Kind == "status" {
					entry.Content = message.Content
				}
				messages = append(messages, entry)
			}
		}
		sort.Slice(messages, func(i, j int) bool { return messages[i].ID < messages[j].ID })
		// Only the caller's own status bodies are returned on explicit query.
		// Ordinary work context and other participants' mail are not copied.
		result := map[string]any{"protocol_version": 1, "correlation_id": id, "root_origin": conversation.RootOrigin,
			"max_messages": conversation.MaxMessages, "used_messages": conversation.UsedMessages,
			"deadline": conversation.Deadline, "closed": conversation.Closed, "messages": messages}
		if conversation.Federation != nil {
			result["federation"] = conversation.Federation
			result["budget_policy"] = "fixed_pair_origin_ceil_peer_floor"
			result["used_messages_scope"] = "messages_observed_by_this_hub"
			result["cancel_scope"] = "local_only; already_network_queued_or_dispatched_messages_may_continue"
		}
		return result, nil
	case r.URL.Path == "/v1/mail/cancel" && r.Method == http.MethodPost:
		var in struct {
			CorrelationID string `json:"correlation_id"`
		}
		if err := decode(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, s.withClientLease(r, endpoint, func() error {
			return s.Broker.CancelMail(endpoint, in.CorrelationID)
		})
	}
	return nil, errors.New("unsupported mailbox operation")
}

type mailStatus struct {
	ID        string `json:"message_id"`
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Kind      string `json:"kind"`
	ReplyTo   string `json:"reply_to,omitempty"`
	Delivery  string `json:"delivery"`
	Detail    string `json:"detail,omitempty"`
	Content   string `json:"content,omitempty"`
}
