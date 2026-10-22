// Package mesh implements an opt-in, mutually paired LAN message transport.
// A received receipt means durable Hub acceptance, never native execution.
package mesh

import (
	"context"
	"encoding/json"
	"time"

	"agihub/internal/language"
)

// Version covers the internal mesh protocol, not mailbox protocol 1.
const Version = 2

const (
	maxFrame   = 512 << 10
	maxPayload = 256 << 10
	maxQueue   = 1000
	maxPeers   = 128
	maxHops    = 8
)

type Options struct {
	DataDir     string
	LocalAgents func() []Agent
	// Receive must durably and idempotently accept by (FromHub, ID) before
	// returning nil. A crash can replay a pending callback after restart.
	Receive  func(context.Context, Message) error
	OnChange func()
}

type Config struct {
	Enabled       bool     `json:"enabled"`
	ListenAddr    string   `json:"listen_addr"`
	AdvertiseAddr string   `json:"advertise_addr,omitempty"`
	Name          string   `json:"name"`
	Exports       []string `json:"exports"`
}

type Agent struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Status                string `json:"status,omitempty"`
	Driver                string `json:"driver,omitempty"`
	CommunicationLanguage string `json:"communication_language,omitempty"`
}

func validAgentProfile(agent Agent) bool {
	if agent.Driver != "" && !validEndpoint(agent.Driver) {
		return false
	}
	normalized, err := language.Normalize(agent.CommunicationLanguage)
	return err == nil && (agent.CommunicationLanguage == "" || normalized == agent.CommunicationLanguage)
}

type Message struct {
	ID           string          `json:"id"`
	FromHub      string          `json:"from_hub"`
	ToHub        string          `json:"to_hub"`
	FromEndpoint string          `json:"from_endpoint"`
	ToEndpoint   string          `json:"to_endpoint"`
	ExpiresAt    time.Time       `json:"expires_at"`
	Payload      json.RawMessage `json:"payload,omitempty"`
}

type Receipt struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"` // queued, received, or failed
	Error     string `json:"error,omitempty"`
}

type Invite struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

type PairRequest struct {
	ID        string    `json:"id"`
	HubID     string    `json:"hub_id"`
	Name      string    `json:"name"`
	Address   string    `json:"address"`
	Direction string    `json:"direction"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Candidate struct {
	HubID     string    `json:"hub_id"`
	Name      string    `json:"name"`
	Address   string    `json:"address"`
	Version   int       `json:"version"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Peer struct {
	HubID     string  `json:"hub_id"`
	Name      string  `json:"name"`
	Address   string  `json:"address"`
	Online    bool    `json:"online"`
	Reachable bool    `json:"reachable"`
	Agents    []Agent `json:"agents"`
}

type Route struct {
	HubID     string    `json:"hub_id"`
	Path      []string  `json:"path"`
	ExpiresAt time.Time `json:"expires_at"`
}

type State struct {
	HubID      string        `json:"hub_id"`
	Name       string        `json:"name"`
	Address    string        `json:"address"`
	Config     Config        `json:"config"`
	Peers      []Peer        `json:"peers"`
	Pending    []PairRequest `json:"pending"`
	Routes     []Route       `json:"routes"`
	Candidates []Candidate   `json:"candidates"`
	Outbox     []Receipt     `json:"outbox"`
}
