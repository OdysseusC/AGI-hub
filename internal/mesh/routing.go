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
	HubID     string    `json:"hub_id"`
	PublicKey []byte    `json:"public_key"`
	Name      string    `json:"name"`
	Address   string    `json:"address"`
	Agents    []Agent   `json:"agents"`
	Neighbors []string  `json:"neighbors"`
	ExpiresAt time.Time `json:"expires_at"`
	Signature []byte    `json:"signature"`
}
type directoryFrame struct {
	Version int             `json:"version"`
	Entries []advertisement `json:"entries"`
}

func advertisementBytes(ad advertisement) []byte {
	ad.Signature = nil
	b, _ := json.Marshal(ad)
	return append([]byte("agi-hub-directory-v1\x00"), b...)
}
func (s *Service) directory() directoryFrame {
	agents := s.exportedAgents()
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	neighbors := []string{}
	for id := range s.disk.Peers {
		if s.isOnlineLocked(id) {
			neighbors = append(neighbors, id)
		}
	}
	sort.Strings(neighbors)
	self := s.selfLocked()
	ad := advertisement{HubID: s.id, PublicKey: self.PublicKey, Name: self.Name, Address: self.Address, Agents: agents, Neighbors: neighbors, ExpiresAt: now.Add(10 * time.Second).UTC()}
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
		if candidate := s.ads[id]; candidate.ExpiresAt.After(now) && len(entries) < maxPeers {
			encoded, _ := json.Marshal(candidate)
			if frameBytes+len(encoded)+1 > maxFrame {
				continue
			}
			entries = append(entries, candidate)
			frameBytes += len(encoded) + 1
		}
	}
	return directoryFrame{Version: Version, Entries: entries}
}
func (s *Service) acceptDirectory(frame directoryFrame) {
	if frame.Version != Version || len(frame.Entries) > maxPeers {
		return
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, ad := range s.ads {
		if !ad.ExpiresAt.After(now) {
			delete(s.ads, id)
		}
	}
	for _, ad := range frame.Entries {
		if ad.HubID == s.id || len(ad.PublicKey) != ed25519.PublicKeySize || hubID(ad.PublicKey) != ad.HubID || !ad.ExpiresAt.After(now) || ad.ExpiresAt.After(now.Add(15*time.Second)) || len(ad.Agents) > 128 || len(ad.Neighbors) > maxPeers || len(ad.Name) > 120 {
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
			if !validEndpoint(a.ID) || len(a.Name) > 120 || len(a.Status) > 100 {
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
		if pinned, ok := s.disk.Peers[ad.HubID]; ok && !bytes.Equal(pinned.PublicKey, ad.PublicKey) {
			continue
		}
		if old, ok := s.ads[ad.HubID]; ok && !ad.ExpiresAt.After(old.ExpiresAt) {
			continue
		}
		if _, ok := s.ads[ad.HubID]; !ok && len(s.ads) >= maxPeers {
			continue
		}
		s.ads[ad.HubID] = ad
	}
}
func (s *Service) isOnlineLocked(id string) bool {
	return s.disk.Config.Enabled && s.server != nil && s.online[id].After(time.Now())
}

// routeLocked uses BFS: an existing usable direct link wins, otherwise the
// fewest valid advertised hops. The first hop must always be locally paired.
func (s *Service) routeLocked(destination string, visited []string) ([]string, time.Time) {
	now := time.Now()
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
	queue := []candidate{{path: []string{s.id}, expiry: now.Add(10 * time.Second)}}
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
			if !ok || !ad.ExpiresAt.After(now) {
				continue
			}
			next = append(next, ad.Neighbors...)
			if ad.ExpiresAt.Before(expiry) {
				expiry = ad.ExpiresAt
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
