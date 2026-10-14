package driver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agihub/internal/manager"
)

type processCapture struct {
	Args  []string `json:"args"`
	Stdin string   `json:"stdin"`
}

// The test binary is the local mock executable, on every supported OS. No
// native assistant, shell, or real notification endpoint is invoked.
func TestMain(m *testing.M) {
	if mode := os.Getenv("AIMANAGER_DRIVER_TEST_HELPER"); mode != "" {
		switch mode {
		case "capture":
			input, err := io.ReadAll(os.Stdin)
			if err != nil {
				os.Exit(4)
			}
			data, _ := json.Marshal(processCapture{Args: os.Args[1:], Stdin: string(input)})
			if os.WriteFile(os.Getenv("AIMANAGER_DRIVER_TEST_OUTPUT"), data, 0600) != nil {
				os.Exit(5)
			}
		case "fail":
			_, _ = io.WriteString(os.Stderr, "PRIVATE-CONVERSATION-SECRET")
			os.Exit(7)
		case "wait":
			time.Sleep(10 * time.Second)
		default:
			os.Exit(6)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fixture() (manager.Endpoint, manager.Envelope) {
	return manager.Endpoint{ID: "codex", SessionID: "thread-123", Modes: []string{"queue"}},
		manager.Envelope{ID: "message-123", TaskID: "task-123", Kind: "task", Sender: "leader", Recipient: "codex", Mode: "queue", Content: "finish this task", Delivery: "dispatching"}
}

func helper(t *testing.T, mode string) (string, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "capture.json")
	t.Setenv("AIMANAGER_DRIVER_TEST_HELPER", mode)
	t.Setenv("AIMANAGER_DRIVER_TEST_OUTPUT", path)
	return executable, path
}

func readCapture(t *testing.T, path string) processCapture {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var capture processCapture
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	return capture
}

func TestCommandUsesLiteralArgvAndCompleteStdin(t *testing.T) {
	executable, path := helper(t, "capture")
	endpoint, envelope := fixture()
	marker := filepath.Join(t.TempDir(), "must-not-exist")
	attack := "quotes \" ' spaces; $(touch " + marker + ") `touch " + marker + "` & |"
	args := []string{"literal", attack, "--not-a-flag"}
	status, detail := Send(context.Background(), Config{Type: "command", Executable: executable, Args: args}, endpoint, envelope)
	if status != "delivered" || !strings.Contains(detail, "unverified") {
		t.Fatalf("got %s, %s", status, detail)
	}
	capture := readCapture(t, path)
	if !reflect.DeepEqual(capture.Args, args) {
		t.Fatalf("argument boundaries changed: %#v", capture.Args)
	}
	var actual manager.Envelope
	if err := json.Unmarshal([]byte(capture.Stdin), &actual); err != nil || actual != envelope {
		t.Fatalf("incomplete envelope: %#v, %v", actual, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("argument was executed by a shell: %v", err)
	}
}

func TestCodexQueueUsesBoundThreadAndCompleteNotification(t *testing.T) {
	executable, path := helper(t, "capture")
	endpoint, envelope := fixture()
	endpoint.SessionID = "thread with spaces; $(unexpected)"
	envelope.Content = "private \"quoted\" content\nUnicode 中文"
	status, _ := Send(context.Background(), Config{Type: "codex-queue", Executable: executable}, endpoint, envelope)
	if status != "delivered" {
		t.Fatal(status)
	}
	capture := readCapture(t, path)
	if len(capture.Args) != 5 || !reflect.DeepEqual(capture.Args[:4], []string{"queue", "--thread", endpoint.SessionID, "--message"}) {
		t.Fatalf("unexpected argv: %#v", capture.Args)
	}
	encoded, _ := json.Marshal(envelope)
	if !strings.Contains(capture.Args[4], string(encoded)) || !strings.Contains(capture.Args[4], "agi-hub report --endpoint <endpoint ID>") || !strings.Contains(capture.Args[4], "do not prove business completion") {
		t.Fatalf("notification omits the complete envelope or reporting guidance")
	}
}

func TestResultNotificationDoesNotStartAnotherExecution(t *testing.T) {
	executable, path := helper(t, "capture")
	endpoint, envelope := fixture()
	envelope.Kind = "result"
	status, _ := Send(context.Background(), Config{Type: "codex-queue", Executable: executable}, endpoint, envelope)
	if status != "delivered" {
		t.Fatal(status)
	}
	message := readCapture(t, path).Args[4]
	if !strings.Contains(message, "not a new task") || strings.Contains(message, "report --endpoint") {
		t.Fatal("result was sent with instructions to execute/report the old task again")
	}
}

func TestProcessStartedFailuresAreUncertainAndPrivate(t *testing.T) {
	endpoint, envelope := fixture()
	t.Run("nonzero", func(t *testing.T) {
		executable, _ := helper(t, "fail")
		status, detail := Send(context.Background(), Config{Type: "command", Executable: executable}, endpoint, envelope)
		if status != "uncertain" || strings.Contains(detail, "PRIVATE") {
			t.Fatalf("got %s, %s", status, detail)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		executable, _ := helper(t, "wait")
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		status, _ := Send(ctx, Config{Type: "command", Executable: executable}, endpoint, envelope)
		if status != "uncertain" {
			t.Fatal(status)
		}
	})
	t.Run("not-started", func(t *testing.T) {
		status, detail := Send(context.Background(), Config{Type: "command", Executable: filepath.Join(t.TempDir(), "missing")}, endpoint, envelope)
		if status != "failed" || !strings.Contains(detail, "no process") {
			t.Fatalf("got %s, %s", status, detail)
		}
	})
}

func TestWebhookTemplatePreservesEscapingAndLiteralContent(t *testing.T) {
	endpoint, envelope := fixture()
	envelope.Content = "quotes \" \\ newlines\n中文 {{task_id}} {{envelope}}"
	envelope.TaskID = "task\"quoted"
	envelope.ID = "message\nnewline"
	encoded, _ := json.Marshal(envelope)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-only" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request method or headers")
		}
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		var body map[string]any
		if err := decoder.Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if body["text"] != "Task: "+envelope.Content || body["json"] != string(encoded) || body["task"] != envelope.TaskID || body["message"] != envelope.ID || body["number"] != json.Number("9007199254740993") {
			t.Errorf("template corrupted strings or number: %#v", body)
		}
		object, ok := body["nested"].([]any)[0].(map[string]any)
		if !ok || object["content"] != envelope.Content || object["message_id"] != envelope.ID {
			t.Errorf("envelope must be a complete object: %#v", body["nested"])
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	config := Config{Type: "webhook", URL: server.URL, Headers: map[string]string{"Authorization": "Bearer test-only"}, Body: json.RawMessage(`{"text":"Task: {{content}}","json":"{{envelope_json}}","task":"{{task_id}}","message":"{{message_id}}","nested":["{{envelope}}"],"number":9007199254740993}`)}
	status, detail := Send(context.Background(), config, endpoint, envelope)
	if status != "delivered" || calls.Load() != 1 || !strings.Contains(detail, "unverified") {
		t.Fatalf("got %s, %s, calls=%d", status, detail, calls.Load())
	}
}

func TestWebhookDoesNotFollowRedirectOrLeakHeaders(t *testing.T) {
	endpoint, envelope := fixture()
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationCalls.Add(1)
		w.WriteHeader(200)
	}))
	defer destination.Close()
	var firstCalls atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls.Add(1)
		w.Header().Set("Location", destination.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer first.Close()
	status, detail := Send(context.Background(), Config{Type: "webhook", URL: first.URL, Headers: map[string]string{"Authorization": "Bearer test-only", "X-Private": "test-only"}, Body: json.RawMessage(`{"message":"{{content}}"}`)}, endpoint, envelope)
	if status != "uncertain" || !strings.Contains(detail, "307") || firstCalls.Load() != 1 || destinationCalls.Load() != 0 {
		t.Fatalf("status=%s, detail=%s, first=%d, destination=%d", status, detail, firstCalls.Load(), destinationCalls.Load())
	}
}

func TestWebhookUnconfirmedResponsesRemainUncertain(t *testing.T) {
	endpoint, envelope := fixture()
	t.Run("disconnect", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
		}))
		defer server.Close()
		status, _ := Send(context.Background(), Config{Type: "webhook", URL: server.URL, Body: json.RawMessage(`"{{content}}"`)}, endpoint, envelope)
		if status != "uncertain" {
			t.Fatal(status)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			select {
			case <-r.Context().Done():
			case <-time.After(time.Second):
			}
		}))
		defer server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		status, _ := Send(ctx, Config{Type: "webhook", URL: server.URL, Body: json.RawMessage(`"{{content}}"`)}, endpoint, envelope)
		if status != "uncertain" {
			t.Fatal(status)
		}
	})
	t.Run("server-rejection", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "PRIVATE-CONVERSATION-SECRET")
		}))
		defer server.Close()
		status, detail := Send(context.Background(), Config{Type: "webhook", URL: server.URL, Body: json.RawMessage(`"{{content}}"`)}, endpoint, envelope)
		if status != "uncertain" || strings.Contains(detail, "PRIVATE") {
			t.Fatalf("got %s, %s", status, detail)
		}
	})
}

func TestUnsupportedModesDoNotStartTransport(t *testing.T) {
	for _, driverType := range []string{"codex-queue", "webhook"} {
		for _, mode := range []string{"steer", "interrupt", ""} {
			t.Run(driverType+"/"+mode, func(t *testing.T) {
				executable, path := helper(t, "capture")
				endpoint, envelope := fixture()
				envelope.Mode = mode
				status, detail := Send(context.Background(), Config{Type: driverType, Executable: executable}, endpoint, envelope)
				if status != "failed" || !strings.Contains(detail, "queue only") {
					t.Fatalf("got %s, %s", status, detail)
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("unsupported mode started a process")
				}
			})
		}
	}
	t.Run("command-unverified", func(t *testing.T) {
		executable, path := helper(t, "capture")
		endpoint, envelope := fixture()
		envelope.Mode = "steer"
		status, _ := Send(context.Background(), Config{Type: "command", Executable: executable}, endpoint, envelope)
		if status != "failed" {
			t.Fatal(status)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("unverified mode started a process")
		}
	})
	t.Run("command-verified", func(t *testing.T) {
		executable, _ := helper(t, "capture")
		endpoint, envelope := fixture()
		endpoint.Modes = []string{"steer"}
		envelope.Mode = "steer"
		status, _ := Send(context.Background(), Config{Type: "command", Executable: executable}, endpoint, envelope)
		if status != "delivered" {
			t.Fatal(status)
		}
	})
}

func TestInvalidWebhookConfigurationFailsBeforeDispatch(t *testing.T) {
	endpoint, envelope := fixture()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(200)
	}))
	defer server.Close()
	cases := []Config{
		{Type: "webhook", URL: "http://example.com", Body: json.RawMessage(`{}`)},
		{Type: "webhook", URL: "http://localhost.evil.invalid", Body: json.RawMessage(`{}`)},
		{Type: "webhook", URL: "https://user:secret@example.com", Body: json.RawMessage(`{}`)},
		{Type: "webhook", URL: server.URL},
		{Type: "webhook", URL: server.URL, Body: json.RawMessage(`{"broken"}`)},
		{Type: "webhook", URL: server.URL, Body: json.RawMessage(`{} {}`)},
		{Type: "webhook", URL: server.URL, Body: json.RawMessage(`"prefix {{envelope}}"`)},
		{Type: "webhook", URL: server.URL, Body: json.RawMessage(`{}`), Headers: map[string]string{"bad\nheader": "value"}},
		{Type: "webhook", URL: server.URL, Body: json.RawMessage(`{}`), Headers: map[string]string{"X-Test": "value\r\nInjected: value"}},
		{Type: "webhook", URL: server.URL, Body: json.RawMessage(`{}`), TimeoutSeconds: -1},
	}
	for i, config := range cases {
		status, _ := Send(context.Background(), config, endpoint, envelope)
		if status != "failed" {
			t.Errorf("configuration %d got %s", i, status)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid configuration dispatched %d requests", calls.Load())
	}
}
