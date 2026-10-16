package app

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"agihub/internal/manager"
)

const clientLeaseTTL = 60 * time.Second

type clientLease struct {
	ownerID   string
	id        string
	expiresAt time.Time
	binding   leaseBinding
}

type leaseBinding struct{ deviceID, sessionID, driver string }

func fingerprint(binding manager.Endpoint) leaseBinding {
	return leaseBinding{binding.DeviceID, binding.SessionID, binding.Driver}
}

type leaseReceipt struct {
	LeaseID   string `json:"lease_id"`
	ExpiresAt string `json:"expires_at"`
	TTLMS     int64  `json:"ttl_ms"`
}

type requestError struct {
	status int
	code   string
}

func (e *requestError) Error() string { return e.code }
func badRequest(code string) error    { return &requestError{http.StatusBadRequest, code} }
func leaseConflict() error            { return &requestError{http.StatusConflict, "client_lease_conflict"} }

func validLeaseIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 256 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._:-", c)) {
			return false
		}
	}
	return true
}

func (s *Server) changeClientLease(w http.ResponseWriter, r *http.Request, endpoint string) (any, error) {
	binding, ok := s.Broker.Snapshot().Endpoints[endpoint]
	if !ok {
		return nil, &requestError{http.StatusNotFound, "endpoint_not_found"}
	}
	if !binding.Enabled {
		return nil, &requestError{http.StatusUnprocessableEntity, "endpoint_disabled"}
	}
	var in struct {
		OwnerID string `json:"owner_id"`
		LeaseID string `json:"lease_id,omitempty"`
		Release bool   `json:"release,omitempty"`
	}
	if err := decode(w, r, &in); err != nil {
		return nil, badRequest("invalid_client_lease_request")
	}
	if !validLeaseIdentifier(in.OwnerID) || in.LeaseID != "" && !validLeaseIdentifier(in.LeaseID) || in.Release && in.LeaseID == "" {
		return nil, badRequest("invalid_client_lease_request")
	}
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	if s.leases == nil {
		s.leases = make(map[string]clientLease)
	}
	now := time.Now()
	current, exists := s.leases[endpoint]
	active := exists && now.Before(current.expiresAt) && current.binding == fingerprint(binding)
	if in.LeaseID != "" {
		// A stale generation cannot acquire a new lease, renew, or release it.
		if !active || current.ownerID != in.OwnerID || current.id != in.LeaseID {
			return nil, leaseConflict()
		}
		if in.Release {
			delete(s.leases, endpoint)
			return leaseReceipt{current.id, now.UTC().Format(time.RFC3339Nano), clientLeaseTTL.Milliseconds()}, nil
		}
	} else if active && current.ownerID != in.OwnerID {
		return nil, leaseConflict()
	} else if !active {
		var token [32]byte
		if _, err := rand.Read(token[:]); err != nil {
			return nil, err
		}
		current = clientLease{ownerID: in.OwnerID, id: hex.EncodeToString(token[:]), binding: fingerprint(binding)}
	}
	current.expiresAt = now.Add(clientLeaseTTL)
	s.leases[endpoint] = current
	return leaseReceipt{current.id, current.expiresAt.UTC().Format(time.RFC3339Nano), clientLeaseTTL.Milliseconds()}, nil
}

// Keep the generation check and mutation in the same critical section. Never
// hold this mutex while a request waits for an inbox change.
func (s *Server) withClientLease(r *http.Request, endpoint string, mutation func() error) error {
	if err := r.Context().Err(); err != nil {
		return err
	}
	binding, ok := s.Broker.Snapshot().Endpoints[endpoint]
	if !ok || binding.Driver != "claude-code" && r.Header.Get("X-AGI-Hub-Lease") == "" {
		return mutation()
	}
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	if err := r.Context().Err(); err != nil {
		return err
	}
	lease := s.leases[endpoint]
	if !binding.Enabled || lease.id == "" || r.Header.Get("X-AGI-Hub-Lease") != lease.id || !time.Now().Before(lease.expiresAt) || lease.binding != fingerprint(binding) {
		return leaseConflict()
	}
	return mutation()
}
