package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"agihub/internal/manager"
	"agihub/internal/mesh"
)

// remoteManagement is reached only through the current Hub's loopback handler.
// The browser authenticates locally; its headers never cross the LAN.
func (s *Server) remoteManagement(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r, "", true) {
		jsonResponse(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/remote/"), "/", 2)
	if !strings.HasPrefix(r.URL.Path, "/remote/") || len(parts) != 2 || !validRemoteHubID(parts[0]) || r.URL.RawQuery != "" {
		jsonResponse(w, http.StatusNotFound, map[string]string{"error": "remote_management_path_forbidden"})
		return
	}
	source := r.Body
	if source == nil {
		source = http.NoBody
	}
	body, err := io.ReadAll(io.LimitReader(source, mesh.MaxManagementBody+1))
	if err != nil || len(body) > mesh.MaxManagementBody {
		jsonResponse(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "remote_management_body_too_large"})
		return
	}
	in := mesh.ManagementRequest{Method: r.Method, Path: "/" + parts[1], Body: body}
	if err := mesh.ValidateManagementRequest(in); err != nil {
		jsonResponse(w, http.StatusForbidden, map[string]string{"error": "remote_management_operation_forbidden"})
		return
	}
	if s.management == nil {
		jsonResponse(w, http.StatusServiceUnavailable, map[string]string{"error": "remote_management_unavailable"})
		return
	}
	out, err := s.management.mesh.RemoteManagement(r.Context(), parts[0], in)
	if err != nil {
		status, code := http.StatusBadGateway, "remote_management_unavailable"
		var rejected *mesh.ManagementError
		if errors.As(err, &rejected) && rejected.Status == http.StatusForbidden {
			status, code = http.StatusForbidden, "remote_management_permission_required"
		}
		jsonResponse(w, status, map[string]string{"error": code})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(out.Status)
	_, _ = w.Write(out.Body)
}

func validRemoteHubID(id string) bool {
	if len(id) != 64 || id != strings.ToLower(id) {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

// executeRemoteManagement reuses only the fixed local administrative API. The
// target's token exists solely on this in-process request, never in the RPC.
func (s *Server) executeRemoteManagement(ctx context.Context, in mesh.ManagementRequest) (mesh.ManagementResponse, error) {
	if err := mesh.ValidateManagementRequest(in); err != nil {
		return mesh.ManagementResponse{}, err
	}
	if in.Method == http.MethodGet && in.Path == "/v1/state" {
		body, err := remoteStatePreview(s.Broker.Snapshot())
		return mesh.ManagementResponse{Status: http.StatusOK, Body: body}, err
	}
	r, err := http.NewRequestWithContext(ctx, in.Method, "http://"+s.Config.Listen+in.Path, bytes.NewReader(in.Body))
	if err != nil {
		return mesh.ManagementResponse{}, errors.New("invalid local management request")
	}
	r.Header.Set("Authorization", "Bearer "+s.Config.AdminToken)
	r.Header.Set("Content-Type", "application/json")
	w := &managementResponseBuffer{header: make(http.Header)}
	s.Handler().ServeHTTP(w, r)
	if w.overflow {
		return mesh.ManagementResponse{Status: http.StatusRequestEntityTooLarge, Body: json.RawMessage(`{"error":"remote_management_response_too_large"}`)}, nil
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if !json.Valid(w.body.Bytes()) {
		return mesh.ManagementResponse{Status: w.status, Body: json.RawMessage(`{"error":"remote_management_request_failed"}`)}, nil
	}
	return mesh.ManagementResponse{Status: w.status, Body: append(json.RawMessage(nil), w.body.Bytes()...)}, nil
}

type remoteMailboxPreview struct {
	Mailbox       bool   `json:"mailbox"`
	ID            string `json:"message_id"`
	Sender        string `json:"sender"`
	Recipient     string `json:"recipient"`
	Kind          string `json:"kind"`
	CorrelationID string `json:"correlation_id,omitempty"`
	ReplyTo       string `json:"reply_to,omitempty"`
	Delivery      string `json:"delivery"`
	Content       string `json:"content"`
	Truncated     bool   `json:"truncated,omitempty"`
}

type remoteArtifactPreview struct {
	DeviceID    string `json:"device_id"`
	WorkspaceID string `json:"workspace_id"`
	Path        string `json:"path"`
}

type remoteTaskPreview struct {
	ID        string                  `json:"id"`
	Goal      string                  `json:"goal"`
	Recipient string                  `json:"recipient"`
	Execution string                  `json:"execution"`
	Summary   string                  `json:"summary,omitempty"`
	Artifacts []remoteArtifactPreview `json:"artifacts,omitempty"`
	Truncated bool                    `json:"truncated,omitempty"`
}

// The remote dashboard needs a bounded view, not the durable mailbox journal.
// Explicit DTOs exclude network envelopes, native context, resource state,
// endpoint configuration and every other unused field. The local state API
// continues to return its complete snapshot.
func remoteStatePreview(snapshot manager.Snapshot) (json.RawMessage, error) {
	preview := struct {
		RemotePreview bool                            `json:"remote_preview"`
		Truncated     bool                            `json:"truncated"`
		TotalMessages int                             `json:"total_messages"`
		TotalTasks    int                             `json:"total_tasks"`
		Messages      map[string]remoteMailboxPreview `json:"messages"`
		Tasks         map[string]remoteTaskPreview    `json:"tasks"`
	}{RemotePreview: true, Messages: map[string]remoteMailboxPreview{}, Tasks: map[string]remoteTaskPreview{}, TotalTasks: len(snapshot.Tasks)}
	messageIDs := make([]string, 0, len(snapshot.Messages))
	for id, message := range snapshot.Messages {
		if message.Mailbox {
			messageIDs = append(messageIDs, id)
		}
	}
	preview.TotalMessages = len(messageIDs)
	sort.Sort(sort.Reverse(sort.StringSlice(messageIDs)))
	if len(messageIDs) > 30 {
		messageIDs = messageIDs[:30]
		preview.Truncated = true
	}
	for _, id := range messageIDs {
		message := snapshot.Messages[id]
		clipped := false
		preview.Messages[id] = remoteMailboxPreview{
			Mailbox:       true,
			ID:            previewText(message.ID, 128, &clipped),
			Sender:        previewText(message.Sender, 128, &clipped),
			Recipient:     previewText(message.Recipient, 128, &clipped),
			Kind:          previewText(message.Kind, 32, &clipped),
			CorrelationID: previewText(message.CorrelationID, 128, &clipped),
			ReplyTo:       previewText(message.ReplyTo, 128, &clipped),
			Delivery:      previewText(message.Delivery, 32, &clipped),
			Content:       previewText(message.Content, 2048, &clipped),
			Truncated:     clipped,
		}
		preview.Truncated = preview.Truncated || clipped
	}
	taskIDs := make([]string, 0, len(snapshot.Tasks))
	for id := range snapshot.Tasks {
		taskIDs = append(taskIDs, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(taskIDs)))
	if len(taskIDs) > 30 {
		taskIDs = taskIDs[:30]
		preview.Truncated = true
	}
	for _, id := range taskIDs {
		task := snapshot.Tasks[id]
		clipped := len(task.Artifacts) > 5
		item := remoteTaskPreview{
			ID:        previewText(task.ID, 128, &clipped),
			Goal:      previewText(task.Goal, 256, &clipped),
			Recipient: previewText(task.Recipient, 128, &clipped),
			Execution: previewText(task.Execution, 32, &clipped),
			Summary:   previewText(task.Summary, 1024, &clipped),
		}
		for i, artifact := range task.Artifacts {
			if i == 5 {
				break
			}
			item.Artifacts = append(item.Artifacts, remoteArtifactPreview{
				DeviceID:    previewText(artifact.DeviceID, 128, &clipped),
				WorkspaceID: previewText(artifact.WorkspaceID, 128, &clipped),
				Path:        previewText(artifact.Path, 256, &clipped),
			})
		}
		item.Truncated = clipped
		preview.Truncated = preview.Truncated || clipped
		preview.Tasks[id] = item
	}
	for {
		body, err := json.Marshal(preview)
		if err != nil {
			return nil, err
		}
		if len(body) < mesh.MaxManagementBody {
			return body, nil
		}
		// JSON escaping can expand control characters sixfold. Retain the most
		// recent entries within the existing transport budget, even in that case.
		preview.Truncated = true
		if len(messageIDs) >= len(taskIDs) && len(messageIDs) > 0 {
			delete(preview.Messages, messageIDs[len(messageIDs)-1])
			messageIDs = messageIDs[:len(messageIDs)-1]
		} else if len(taskIDs) > 0 {
			delete(preview.Tasks, taskIDs[len(taskIDs)-1])
			taskIDs = taskIDs[:len(taskIDs)-1]
		} else {
			return nil, errors.New("remote state preview exceeds its fixed transport budget")
		}
	}
}

func previewText(value string, limit int, truncated *bool) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	*truncated = true
	for index := range value {
		if limit == 0 {
			return value[:index]
		}
		limit--
	}
	return value
}

type managementResponseBuffer struct {
	header   http.Header
	status   int
	body     bytes.Buffer
	overflow bool
}

func (w *managementResponseBuffer) Header() http.Header { return w.header }
func (w *managementResponseBuffer) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
}
func (w *managementResponseBuffer) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.body.Len()+len(body) > mesh.MaxManagementBody {
		w.overflow = true
		return 0, errors.New("management response exceeds limit")
	}
	return w.body.Write(body)
}
