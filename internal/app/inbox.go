package app

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"agihub/internal/manager"
)

func inboxWait(r *http.Request) (time.Duration, error) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return 0, badRequest("invalid_wait_ms")
	}
	values, present := query["wait_ms"]
	if !present {
		return 0, nil
	}
	if len(values) != 1 || values[0] == "" {
		return 0, badRequest("invalid_wait_ms")
	}
	for _, digit := range values[0] {
		if digit < '0' || digit > '9' {
			return 0, badRequest("invalid_wait_ms")
		}
	}
	value, err := strconv.Atoi(values[0])
	if err != nil || value < 1 || value > 25000 {
		return 0, badRequest("invalid_wait_ms")
	}
	return time.Duration(value) * time.Millisecond, nil
}

func (s *Server) nextForRequest(r *http.Request, endpoint string, mailbox bool) (*manager.Envelope, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	var envelope *manager.Envelope
	err := s.withClientLease(r, endpoint, func() error {
		var err error
		if mailbox {
			envelope, err = s.Broker.NextMail(endpoint)
		} else {
			envelope, err = s.Broker.Next(endpoint)
		}
		return err
	})
	if envelope != nil {
		filtered := clientEnvelope(*envelope)
		envelope = &filtered
	}
	return envelope, err
}

func (s *Server) inbox(r *http.Request, endpoint string, wait time.Duration, mailbox bool) (*manager.Envelope, error) {
	envelope, err := s.nextForRequest(r, endpoint, mailbox)
	if err != nil || envelope != nil || wait == 0 {
		return envelope, err
	}
	changes, cancel := s.Broker.Subscribe()
	defer cancel()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	// Subscribe before the second check so a concurrent commit cannot be lost.
	envelope, err = s.nextForRequest(r, endpoint, mailbox)
	if err != nil || envelope != nil {
		return envelope, err
	}
	for {
		select {
		case <-r.Context().Done():
			return nil, r.Context().Err()
		case <-timer.C:
			return nil, s.withClientLease(r, endpoint, func() error { return nil })
		case _, open := <-changes:
			if !open {
				return nil, errors.New("broker is closed")
			}
			envelope, err = s.nextForRequest(r, endpoint, mailbox)
			if err != nil || envelope != nil {
				return envelope, err
			}
		}
	}
}
