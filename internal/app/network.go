package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"time"

	"agihub/internal/language"
	"agihub/internal/manager"
	"agihub/internal/mesh"
)

type managementRuntime struct {
	mu           sync.Mutex // serialize explicit administrative mutations
	dir          string
	mesh         *mesh.Service
	discovery    *DiscoveryController
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
	networkError string // guarded by mu after initialization
}

// StartManagement creates a private persistent identity. No LAN listener or
// discovery socket is opened until explicitly enabled in saved settings.
func (s *Server) StartManagement(ctx context.Context, dir string) error {
	ctx, cancel := context.WithCancel(ctx)
	m := &managementRuntime{dir: dir, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	service, err := mesh.New(mesh.Options{DataDir: filepath.Join(dir, "network"), Manage: s.executeRemoteManagement, LocalAgents: func() []mesh.Agent {
		out := []mesh.Agent{}
		state := s.Broker.Snapshot()
		for _, e := range state.Endpoints {
			if e.Enabled && e.MailboxProtocolVersion == 1 {
				out = append(out, mesh.Agent{ID: e.ID, Name: e.ID, Status: state.Sessions[e.ID].State,
					Driver: e.Driver, CommunicationLanguage: language.Effective(e.Driver, e.CommunicationLanguage)})
			}
		}
		return out
	}, Receive: func(ctx context.Context, message mesh.Message) error {
		var wire manager.NetworkMail
		decoder := json.NewDecoder(bytes.NewReader(message.Payload))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&wire); err != nil {
			return errors.New("invalid network mailbox payload")
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return errors.New("extra network mailbox payload")
		}
		deadline, err := time.Parse(time.RFC3339, wire.Deadline)
		if err != nil || !deadline.Equal(message.ExpiresAt) || wire.ID != message.ID || wire.FromHub != message.FromHub || wire.ToHub != message.ToHub || wire.Sender != message.FromEndpoint || wire.Recipient != message.ToEndpoint {
			return errors.New("authenticated network header differs from mailbox payload")
		}
		_, err = s.Broker.ImportRemote(message.ToHub, wire)
		return err
	}, OnChange: s.Broker.Signal})
	if err != nil {
		cancel()
		return err
	}
	m.mesh = service
	m.discovery = NewDiscoveryController(service, s.Broker.Signal)
	s.management = m
	if err = service.Start(ctx); err != nil {
		// Network addresses may disappear when a laptop changes Wi-Fi. Keep
		// loopback mail and the repair UI available, with LAN explicitly off.
		m.networkError = err.Error()
		cfg := service.Snapshot().Config
		cfg.Enabled = false
		if disableErr := service.Configure(cfg); disableErr != nil {
			cancel()
			s.management = nil
			_ = service.Close()
			return errors.Join(err, disableErr)
		}
		if startErr := service.Start(ctx); startErr != nil {
			cancel()
			s.management = nil
			_ = service.Close()
			return startErr
		}
	}
	prefs, err := readDiscoveryPreferences(dir)
	if err != nil {
		cancel()
		_ = service.Close()
		s.management = nil
		return err
	}
	if prefs.Enabled {
		// A missing Wi-Fi adapter must not make the existing local mailbox
		// unavailable. The controller retains this error for the dashboard.
		_ = m.discovery.Configure(ctx, true, prefs.Interface)
	}
	go s.networkLoop(m)
	return nil
}

func (s *Server) CloseManagement() error {
	m := s.management
	if m == nil {
		return nil
	}
	m.cancel()
	<-m.done
	return errors.Join(m.discovery.Close(), m.mesh.Close())
}

func (s *Server) networkLoop(m *managementRuntime) {
	defer close(m.done)
	changes, cancel := s.Broker.Subscribe()
	defer cancel()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		s.flushNetwork(m)
		select {
		case <-m.ctx.Done():
			return
		case <-changes:
		case <-ticker.C:
		}
	}
}

func (s *Server) flushNetwork(m *managementRuntime) {
	state := m.mesh.Snapshot()
	receipts := map[string]string{}
	for _, r := range state.Outbox {
		receipts[r.MessageID] = r.Status
	}
	snapshot := s.Broker.Snapshot()
	for _, e := range snapshot.Messages {
		if e.Network == nil || e.Delivery != "network_pending" {
			continue
		}
		if status := receipts[e.ID]; status == "received" || status == "failed" {
			_ = s.Broker.NetworkReceipt(e.ID, status)
			continue
		}
		wire := *e.Network
		deadline, err := time.Parse(time.RFC3339, wire.Deadline)
		if err != nil || !time.Now().Before(deadline) {
			_ = s.Broker.NetworkReceipt(e.ID, "failed")
			continue
		}
		if !state.Config.Enabled || snapshot.MailConversations[e.CorrelationID].Closed {
			continue
		}
		body, err := json.Marshal(wire)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(m.ctx, 3*time.Second)
		receipt, err := m.mesh.Send(ctx, mesh.Message{ID: wire.ID, FromHub: wire.FromHub, ToHub: wire.ToHub, FromEndpoint: wire.Sender, ToEndpoint: wire.Recipient, ExpiresAt: deadline, Payload: body})
		cancel()
		if err == nil {
			_ = s.Broker.NetworkReceipt(e.ID, receipt.Status)
		}
		if m.ctx.Err() != nil {
			return
		}
	}
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// Native clients need one copy of the addressed content, not the durable mesh
// frame inside it. Preserve IDs/context while omitting transport-only payload.
func clientEnvelope(envelope manager.Envelope) manager.Envelope {
	if envelope.Network != nil {
		envelope.SourceMessageID = envelope.Network.SourceID
		envelope.Network = nil
		envelope.MailFingerprint = ""
	}
	return envelope
}

func (s *Server) remoteCatalog(endpoint string) []Peer {
	out := []Peer{}
	if s.management == nil {
		return out
	}
	state := s.management.mesh.Snapshot()
	if !state.Config.Enabled || !contains(state.Config.Exports, endpoint) {
		return out
	}
	self := s.Broker.Snapshot().Endpoints[endpoint]
	for _, hub := range state.Peers {
		for _, agent := range hub.Agents {
			driver := agent.Driver
			if driver == "" {
				driver = "remote"
			}
			out = append(out, Peer{ID: manager.RemoteAddress(hub.HubID, agent.ID), Driver: driver, Purpose: hub.Name + " / " + agent.Name,
				Modes: []string{"queue"}, CanSend: hub.Reachable, MailboxProtocolVersion: 1,
				CommunicationLanguage: language.Effective(driver, agent.CommunicationLanguage),
				SendLanguage:          language.Resolve(self.Driver, self.CommunicationLanguage, driver, agent.CommunicationLanguage)})
		}
	}
	return out
}

func (s *Server) publishRemote(endpoint string, in manager.MailSubmission) (manager.Envelope, error) {
	if s.management == nil {
		return manager.Envelope{}, errors.New("LAN management is unavailable")
	}
	state := s.management.mesh.Snapshot()
	if !state.Config.Enabled || !contains(state.Config.Exports, endpoint) {
		return manager.Envelope{}, errors.New("enable LAN and explicitly share this endpoint first")
	}
	hub, recipient, err := manager.ParseRemoteAddress(in.Recipient)
	if err != nil {
		return manager.Envelope{}, err
	}
	allowed := false
	sendLanguage := ""
	self := s.Broker.Snapshot().Endpoints[endpoint]
	for _, peer := range state.Peers {
		if peer.HubID == hub {
			for _, a := range peer.Agents {
				if a.ID == recipient {
					allowed = true
					sendLanguage = language.Resolve(self.Driver, self.CommunicationLanguage, a.Driver, a.CommunicationLanguage)
				}
			}
		}
	}
	if !allowed {
		return manager.Envelope{}, errors.New("recipient must be an exported endpoint of a directly trusted Hub")
	}
	return s.Broker.PublishRemote(state.HubID, endpoint, in, sendLanguage)
}
