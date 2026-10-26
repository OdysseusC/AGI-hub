package mesh

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"sort"
	"time"
)

// A directory entry is signed by its own Hub. Relaying an entry conveys
// visibility only; it does not add that Hub to any pairing/trust set.
type advertisement struct {
	HubID     string   `json:"hub_id"`
	PublicKey []byte   `json:"public_key"`
	Name      string   `json:"name"`
	Address   string   `json:"address"`
	Agents    []Agent  `json:"agents"`
	Neighbors []string `json:"neighbors"`
	Revision  uint64   `json:"revision"`
	Signature []byte   `json:"signature"`
}
type directoryFrame struct {
	Version int             `json:"version"`
	Entries []advertisement `json:"entries"`
}

func advertisementBytes(ad advertisement) []byte {
	ad.Signature = nil
	b, _ := json.Marshal(ad)
	return append([]byte("agi-hub-directory-v2\x00"), b...)
}

const directoryLease = 10 * time.Second

func (s *Service) directory() (directoryFrame, error) {
	agents := s.exportedAgents()
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	revision, err := s.nextDirectoryRevisionLocked()
	if err != nil {
		return directoryFrame{}, err
	}
	neighbors := []string{}
	for id := range s.disk.Peers {
		if s.isOnlineLocked(id) {
			neighbors = append(neighbors, id)
		}
	}
	sort.Strings(neighbors)
	self := s.selfLocked()
	ad := advertisement{HubID: s.id, PublicKey: self.PublicKey, Name: self.Name, Address: self.Address, Agents: agents, Neighbors: neighbors, Revision: revision}
	ad.Signature = ed25519.Sign(s.key, advertisementBytes(ad))
	entries := []advertisement{ad}
	initial, _ := json.Marshal(directoryFrame{Version: Version, Entries: entries})
	frameBytes := len(initial)
	ids := []string{}
	for id := range s.ads {
		if id != s.id {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		if candidate := s.ads[id]; s.adLeases[id].After(now) && len(entries) < maxPeers {
			encoded, _ := json.Marshal(candidate)
			if frameBytes+len(encoded)+1 > maxFrame {
				continue
			}
			entries = append(entries, candidate)
			frameBytes += len(encoded) + 1
		}
	}
	return directoryFrame{Version: Version, Entries: entries}, nil
}
func (s *Service) acceptDirectory(frame directoryFrame) {
	s.acceptDirectoryFrom("", frame)
}

// source is the paired Hub authenticated by the directory request's TLS pin.
// Its validly signed self advertisement is a live response. Relayed entries
// only convey bounded route hints; neither kind grants pairing or mail access.
func (s *Service) acceptDirectoryFrom(source string, frame directoryFrame) {
	if frame.Version != Version || len(frame.Entries) > maxPeers {
		return
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.ads {
		if !s.adLeases[id].After(now) {
			delete(s.ads, id)
			delete(s.adLeases, id)
		}
	}
	for _, ad := range frame.Entries {
		if ad.HubID == s.id || ad.Revision == 0 || len(ad.PublicKey) != ed25519.PublicKeySize || hubID(ad.PublicKey) != ad.HubID || len(ad.Agents) > 128 || len(ad.Neighbors) > maxPeers || len(ad.Name) > 120 {
			continue
		}
		if !ed25519.Verify(ad.PublicKey, advertisementBytes(ad), ad.Signature) {
			continue
		}
		if !validAddress(ad.Address, false) {
			continue
		}
		valid := true
		for _, a := range ad.Agents {
			if !validEndpoint(a.ID) || len(a.Name) > 120 || len(a.Status) > 100 || !validAgentProfile(a) {
				valid = false
				break
			}
		}
		for _, id := range ad.Neighbors {
			if len(id) != 64 {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		pinned, paired := s.disk.Peers[ad.HubID]
		if paired && !bytes.Equal(pinned.PublicKey, ad.PublicKey) {
			continue
		}
		directSelf := paired && ad.HubID == source
		previous, seen := s.adRevisions[ad.HubID]
		if ad.Revision < previous || (seen && ad.Revision == previous && !directSelf) {
			continue
		}
		if !seen && !paired {
			// Keep expired high-water marks instead of evicting and re-accepting
			// old revisions. Reserve another maxPeers slots for paired origins.
			relayOrigins := 0
			for id := range s.adRevisions {
				if _, paired := s.disk.Peers[id]; !paired {
					relayOrigins++
				}
			}
			if relayOrigins >= maxPeers {
				continue
			}
		}
		s.adRevisions[ad.HubID] = ad.Revision
		s.ads[ad.HubID] = ad
		s.adLeases[ad.HubID] = now.Add(directoryLease)
	}
}
func (s *Service) isOnlineLocked(id string) bool {
	return s.disk.Config.Enabled && s.server != nil && s.online[id].After(s.now())
}

// routeLocked uses BFS: an existing usable direct link wins, otherwise the
// fewest valid advertised hops. The first hop must always be locally paired.
func (s *Service) routeLocked(destination string, visited []string) ([]string, time.Time) {
	now := s.now()
	if !s.disk.Config.Enabled || s.server == nil {
		return nil, time.Time{}
	}
	excluded := map[string]bool{}
	for _, id := range visited {
		excluded[id] = true
	}
	delete(excluded, s.id)
	if excluded[destination] {
		return nil, time.Time{}
	}
	type candidate struct {
		path   []string
		expiry time.Time
	}
	queue := []candidate{{path: []string{s.id}, expiry: now.Add(directoryLease)}}
	seen := map[string]bool{s.id: true}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		last := current.path[len(current.path)-1]
		if last == destination {
			return current.path, current.expiry
		}
		if len(current.path) >= maxHops || len(current.path)+len(visited) > maxHops+1 {
			continue
		}
		next := []string{}
		expiry := current.expiry
		if last == s.id {
			for id := range s.disk.Peers {
				if s.isOnlineLocked(id) {
					next = append(next, id)
				}
			}
		} else {
			ad, ok := s.ads[last]
			if !ok || !s.adLeases[last].After(now) {
				continue
			}
			next = append(next, ad.Neighbors...)
			if s.adLeases[last].Before(expiry) {
				expiry = s.adLeases[last]
			}
		}
		sort.Strings(next)
		for _, id := range next {
			if seen[id] || excluded[id] {
				continue
			}
			seen[id] = true
			path := append(append([]string{}, current.path...), id)
			queue = append(queue, candidate{path: path, expiry: expiry})
		}
	}
	return nil, time.Time{}
}
