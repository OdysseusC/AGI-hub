package mesh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const MaxManagementBody = 1 << 20
const maxManagementFrame = 2 << 20

type ManagementError struct{ Status int }

func (e *ManagementError) Error() string { return "remote management request rejected" }

// ValidateManagementRequest is deliberately narrower than a general HTTP proxy.
// Queries, forwarded URLs, credentials, network settings and recursive proxies
// are never accepted, even from a peer with management permission.
func ValidateManagementRequest(in ManagementRequest) error {
	if len(in.Body) > MaxManagementBody {
		return errors.New("management body exceeds limit")
	}
	switch {
	case in.Method == http.MethodGet && (in.Path == "/v1/manage" || in.Path == "/v1/state"):
		if len(in.Body) != 0 {
			return errors.New("management GET body forbidden")
		}
	case in.Method == http.MethodPost && (in.Path == "/v1/manage/agent" || in.Path == "/v1/manage/autostart"):
		body := bytes.TrimSpace(in.Body)
		if len(body) == 0 || body[0] != '{' || !json.Valid(body) {
			return errors.New("management POST requires a JSON object")
		}
	default:
		return errors.New("remote management operation forbidden")
	}
	return nil
}

func (s *Service) SetManagementAllowed(peerID string, allowed bool) error {
	s.mu.Lock()
	if _, ok := s.disk.Peers[peerID]; !ok {
		s.mu.Unlock()
		return errors.New("management requires a directly paired peer")
	}
	old := s.disk.Management[peerID]
	if allowed {
		s.disk.Management[peerID] = true
	} else {
		delete(s.disk.Management, peerID)
	}
	if err := s.persistLocked(); err != nil {
		if old {
			s.disk.Management[peerID] = true
		} else {
			delete(s.disk.Management, peerID)
		}
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	s.changed()
	return nil
}

// RemoteManagement uses only the selected peer's pinned direct connection.
// It has no route lookup, forwarding, browser headers or automatic retry.
func (s *Service) RemoteManagement(ctx context.Context, peerID string, in ManagementRequest) (ManagementResponse, error) {
	if err := ValidateManagementRequest(in); err != nil {
		return ManagementResponse{}, err
	}
	s.mu.Lock()
	peer, paired := s.disk.Peers[peerID]
	enabled := s.disk.Config.Enabled && s.server != nil
	s.mu.Unlock()
	if !enabled || !paired {
		return ManagementResponse{}, errors.New("remote management requires enabled LAN and a directly paired peer")
	}
	var out ManagementResponse
	if err := s.requestLimited(ctx, peer, "/v1/manage", in, &out, maxManagementFrame); err != nil {
		var remote *networkError
		if errors.As(err, &remote) {
			return ManagementResponse{}, &ManagementError{Status: remote.Code}
		}
		return ManagementResponse{}, err
	}
	if !validManagementResponse(out) {
		return ManagementResponse{}, errors.New("invalid remote management response")
	}
	return out, nil
}

func validManagementResponse(out ManagementResponse) bool {
	return out.Status >= 200 && out.Status <= 599 && len(out.Body) <= MaxManagementBody && json.Valid(out.Body)
}

func (s *Service) managementAllowed(peerID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, paired := s.disk.Peers[peerID]
	return paired && s.disk.Config.Enabled && s.disk.Management[peerID]
}

func (s *Service) handleManagement(w http.ResponseWriter, r *http.Request, peerID string) {
	// Re-read the current grant on every call. Existing keep-alive TLS connections
	// never retain administrative permission after revocation.
	if !s.managementAllowed(peerID) {
		http.Error(w, "remote management permission required", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost || r.URL.RawQuery != "" {
		http.Error(w, "management RPC requires POST without query", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxManagementFrame)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var in ManagementRequest
	if err := decoder.Decode(&in); err != nil {
		http.Error(w, "invalid management request", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		http.Error(w, "trailing management request data", http.StatusBadRequest)
		return
	}
	if err := ValidateManagementRequest(in); err != nil {
		http.Error(w, "remote management operation forbidden", http.StatusBadRequest)
		return
	}
	if s.opts.Manage == nil {
		http.Error(w, "remote management unavailable", http.StatusServiceUnavailable)
		return
	}
	// An administrator may revoke permission while a bounded body is arriving.
	if !s.managementAllowed(peerID) {
		http.Error(w, "remote management permission required", http.StatusForbidden)
		return
	}
	out, err := s.opts.Manage(r.Context(), in)
	if err != nil || !validManagementResponse(out) {
		http.Error(w, "remote management failed", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respond(w, out)
}
