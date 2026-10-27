package app

import (
	"errors"
	"net/http"

	"agihub/internal/manager"
)

func (s *Server) observeEndpointSession(w http.ResponseWriter, r *http.Request, endpoint string) error {
	var in manager.Session
	if err := decode(w, r, &in); err != nil {
		return badRequest("invalid_endpoint_session_observation")
	}
	nativeSession := r.Header.Get("X-AGI-Hub-Native-Session")
	if nativeSession == "" {
		return badRequest("native_session_binding_required")
	}
	err := s.withClientLease(r, endpoint, func() error {
		return s.Broker.ObserveSession(endpoint, nativeSession, in.State, in.RunID)
	})
	if errors.Is(err, manager.ErrNativeSessionBindingMismatch) {
		return &requestError{http.StatusConflict, "native_session_binding_mismatch"}
	}
	return err
}
