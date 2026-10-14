package app

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"agihub/internal/manager"
)

type Server struct {
	Broker *manager.Broker
	Config Config
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.handle)
}

func (s *Server) authorized(r *http.Request, endpoint string, adminOnly bool) bool {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if len(token) == 0 {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.Config.AdminToken)) == 1 {
		return true
	}
	if adminOnly || endpoint == "" {
		return false
	}
	if _, ok := s.Broker.Snapshot().Endpoints[endpoint]; !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(EndpointToken(s.Config.AdminToken, endpoint))) == 1
}

func jsonResponse(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func decode(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("expected exactly one JSON object")
	}
	return nil
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	// Host and Origin checks prevent a web page from using the local control plane.
	if r.Host != s.Config.Listen {
		http.Error(w, "invalid host", http.StatusForbidden)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != "http" || u.Host != s.Config.Listen {
			http.Error(w, "invalid origin", http.StatusForbidden)
			return
		}
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.URL.Path == "/" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'")
		_, _ = io.WriteString(w, dashboard)
		return
	}
	endpoint := r.URL.Query().Get("endpoint")
	adminOnly := r.URL.Path != "/v1/inbox" && r.URL.Path != "/v1/delivery" && r.URL.Path != "/v1/report" && r.URL.Path != "/v1/events"
	if !s.authorized(r, endpoint, adminOnly) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var result any = map[string]bool{"ok": true}
	var err error
	switch {
	case r.URL.Path == "/v1/state" && r.Method == http.MethodGet:
		result = s.Broker.Snapshot()
	case r.URL.Path == "/v1/tasks" && r.Method == http.MethodPost:
		var in manager.Submission
		if err = decode(w, r, &in); err == nil {
			result, err = s.Broker.Submit(in)
		}
	case r.URL.Path == "/v1/inbox" && r.Method == http.MethodPost:
		result, err = s.Broker.Next(endpoint)
	case r.URL.Path == "/v1/delivery" && r.Method == http.MethodPost:
		var in struct {
			MessageID string `json:"message_id"`
			Outcome   string `json:"outcome"`
			Detail    string `json:"detail"`
		}
		if err = decode(w, r, &in); err == nil {
			m, ok := s.Broker.Snapshot().Messages[in.MessageID]
			if !ok || m.Recipient != endpoint {
				err = errors.New("message does not belong to this client")
			} else {
				err = s.Broker.Delivery(in.MessageID, in.Outcome, in.Detail)
			}
		}
	case r.URL.Path == "/v1/report" && r.Method == http.MethodPost:
		var in manager.Report
		if err = decode(w, r, &in); err == nil {
			if endpoint == "" || in.Endpoint != endpoint {
				err = errors.New("report endpoint must match authenticated client")
			} else {
				err = s.Broker.Report(in)
			}
		}
	case r.URL.Path == "/v1/leader" && r.Method == http.MethodPost:
		var in struct {
			Endpoint string `json:"endpoint"`
		}
		if err = decode(w, r, &in); err == nil {
			err = s.Broker.SetLeader(in.Endpoint)
		}
	case r.URL.Path == "/v1/session" && r.Method == http.MethodPost:
		var in struct {
			Endpoint string `json:"endpoint"`
			State    string `json:"state"`
			RunID    string `json:"run_id"`
		}
		if err = decode(w, r, &in); err == nil {
			err = s.Broker.SetSession(in.Endpoint, in.State, in.RunID)
		}
	case r.URL.Path == "/v1/events" && r.Method == http.MethodGet:
		s.stream(w, r)
		return
	default:
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		jsonResponse(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	changes, cancel := s.Broker.Subscribe()
	defer cancel()
	_, _ = fmt.Fprint(w, "event: change\ndata: {}\n\n")
	flusher.Flush()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-changes:
			if _, err := fmt.Fprint(w, "event: change\ndata: {}\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": connection keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
