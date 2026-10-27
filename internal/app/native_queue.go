package app

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"agihub/internal/manager"
)

// NativeQueueVerification is the last local administrator decision and its
// binding anchor. EvidenceRef is an opaque reference, never a path to read.
type NativeQueueVerification struct {
	DeviceID        string `json:"device_id"`
	Driver          string `json:"driver"`
	NativeSessionID string `json:"native_session_id"`
	EvidenceRef     string `json:"evidence_ref"`
}

func validNativeQueueEvidence(value string) bool {
	return value != "" && len(value) <= 256 && strings.TrimSpace(value) == value && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}

func nativeQueueVerificationMatches(binding Binding) bool {
	e, proof := binding.Endpoint, binding.NativeQueueVerification
	queue := len(e.Modes) == 1 && e.Modes[0] == "queue"
	return proof != nil && e.MailboxProtocolVersion == 1 && queue && e.SessionID != "" && proof.DeviceID == e.DeviceID && proof.Driver == e.Driver && proof.NativeSessionID == e.SessionID && validNativeQueueEvidence(proof.EvidenceRef)
}

func configuredNativeQueueVerification(stored Binding, current manager.Endpoint) bool {
	return stored.Endpoint.MailboxNativeQueueVerified && nativeQueueVerificationMatches(stored) && stored.Endpoint.DeviceID == current.DeviceID && stored.Endpoint.Driver == current.Driver && stored.Endpoint.SessionID == current.SessionID
}

type nativeQueueDecision struct {
	Endpoint        string `json:"endpoint"`
	NativeSessionID string `json:"native_session_id"`
	Verified        *bool  `json:"verified"`
	EvidenceRef     string `json:"evidence_ref"`
}

func (s *Server) recordNativeQueueDecision(in nativeQueueDecision, save func(string, any) error) error {
	if in.Endpoint == "" || in.NativeSessionID == "" || in.Verified == nil || !validNativeQueueEvidence(in.EvidenceRef) {
		return badRequest("invalid_native_queue_verification")
	}
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	old, ok := s.Broker.Snapshot().Endpoints[in.Endpoint]
	if !ok || old.SessionID == "" || old.SessionID != in.NativeSessionID {
		return &requestError{http.StatusConflict, "native_session_binding_mismatch"}
	}
	cfg, err := LoadConfig(s.management.dir)
	if err != nil {
		return err
	}
	index := -1
	for i := range cfg.Bindings {
		b := &cfg.Bindings[i]
		if b.Endpoint.ID != in.Endpoint {
			continue
		}
		if b.Endpoint.DeviceID != old.DeviceID || b.Endpoint.Driver != old.Driver || b.Endpoint.SessionID != old.SessionID {
			return &requestError{http.StatusConflict, "native_session_binding_mismatch"}
		}
		index = i
	}
	if index < 0 {
		return errors.New("endpoint missing from local configuration")
	}
	updated := old
	updated.MailboxNativeQueueVerified = *in.Verified
	cfg.Bindings[index].Endpoint.MailboxNativeQueueVerified = *in.Verified
	cfg.Bindings[index].NativeQueueVerification = &NativeQueueVerification{old.DeviceID, old.Driver, old.SessionID, in.EvidenceRef}
	if *in.Verified && !nativeQueueVerificationMatches(cfg.Bindings[index]) {
		return errors.New("native queue verification requires a bound mailbox protocol 1 queue endpoint")
	}
	if err := s.Broker.Register(updated); err != nil {
		return err
	}
	if err := save(filepath.Join(s.management.dir, "config.json"), cfg); err != nil {
		return errors.Join(err, s.Broker.Register(old))
	}
	return nil
}
