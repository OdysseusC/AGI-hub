package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"

	"agihub/internal/install"
	"agihub/internal/language"
)

// Peer omits native session addresses, workspace paths, credentials and history.
type Peer struct {
	ID                     string   `json:"id"`
	Driver                 string   `json:"driver"`
	Purpose                string   `json:"purpose,omitempty"`
	Modes                  []string `json:"modes"`
	IdleWakeVerified       bool     `json:"idle_wake_verified"`
	BusyResultVerified     bool     `json:"busy_result_verified"`
	CanSend                bool     `json:"can_send"`
	MailboxProtocolVersion int      `json:"mailbox_protocol_version"`
	CommunicationLanguage  string   `json:"communication_language"`
	SendLanguage           string   `json:"send_language"`
}

type Catalog struct {
	ProtocolVersion           int    `json:"protocol_version"`
	Version                   string `json:"version"`
	RulesVersion              string `json:"rules_version"`
	Endpoint                  string `json:"endpoint"`
	Peers                     []Peer `json:"peers"`
	SelfCommunicationLanguage string `json:"self_communication_language"`
}

func (s *Server) catalog(endpoint string) (Catalog, error) {
	state := s.Broker.Snapshot()
	self, ok := state.Endpoints[endpoint]
	if !ok || !self.Enabled {
		return Catalog{}, errors.New("directory requires an enabled endpoint")
	}
	bindings := map[string]Binding{}
	for _, binding := range s.Config.Bindings {
		bindings[binding.Endpoint.ID] = binding
	}
	peers := []Peer{}
	for _, id := range bindings[endpoint].VisiblePeers {
		e, exists := state.Endpoints[id]
		binding, configured := bindings[id]
		if !exists || !configured || !e.Enabled {
			continue
		}
		modes := append([]string{}, e.Modes...)
		sort.Strings(modes)
		allowed := false
		for _, recipient := range bindings[endpoint].AllowedPeers {
			allowed = allowed || recipient == id
		}
		peers = append(peers, Peer{ID: id, Driver: e.Driver, Purpose: binding.Purpose, Modes: modes,
			IdleWakeVerified: e.IdleWakeVerified, BusyResultVerified: e.ResultQueueWhileBusyVerified,
			CanSend: allowed && self.MailboxProtocolVersion == 1 && e.MailboxProtocolVersion == 1, MailboxProtocolVersion: e.MailboxProtocolVersion,
			CommunicationLanguage: language.Effective(e.Driver, e.CommunicationLanguage),
			SendLanguage:          language.Resolve(self.Driver, self.CommunicationLanguage, e.Driver, e.CommunicationLanguage)})
	}
	peers = append(peers, s.remoteCatalog(endpoint)...)
	sort.Slice(peers, func(i, j int) bool { return peers[i].ID < peers[j].ID })
	catalog := Catalog{ProtocolVersion: 1, Endpoint: endpoint, Peers: peers,
		SelfCommunicationLanguage: language.Effective(self.Driver, self.CommunicationLanguage)}
	content, _ := json.Marshal(catalog)
	digest := sha256.Sum256(content)
	catalog.Version = "sha256:" + hex.EncodeToString(digest[:])
	_, catalog.RulesVersion, _ = install.CurrentSkill()
	return catalog, nil
}

// Version checks do not copy the complete rules into every model request.
type SkillRules struct {
	ProtocolVersion int    `json:"protocol_version"`
	Revision        int    `json:"revision"`
	Version         string `json:"version"`
	Content         string `json:"content,omitempty"`
}

func (s *Server) skillRules(endpoint string, includeContent bool) (SkillRules, error) {
	self, ok := s.Broker.Snapshot().Endpoints[endpoint]
	if !ok || !self.Enabled {
		return SkillRules{}, errors.New("skill rules require an enabled endpoint")
	}
	return currentSkillRules(includeContent)
}

func currentSkillRules(includeContent bool) (SkillRules, error) {
	content, version, err := install.CurrentSkill()
	if err != nil {
		return SkillRules{}, err
	}
	rules := SkillRules{ProtocolVersion: 1, Revision: install.SkillRevision, Version: version}
	if includeContent {
		rules.Content = string(content)
	}
	return rules, nil
}
