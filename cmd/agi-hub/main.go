package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"agihub/internal/app"
	"agihub/internal/install"
	"agihub/internal/manager"
)

const version = "0.4.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		fmt.Printf("AGI Hub %s\nCommands: init, serve, doctor, status, submit, report, mail-send, mail-state, mail-cancel, session, leader, dashboard, install-skill, sync-skills, skills-status, peers, skill-rules, client-config, inbox, delivery\nUse <command> --help for options.\n", version)
		return nil
	}
	if args[0] == "--version" || args[0] == "version" {
		fmt.Println("AGI Hub "+version, runtime.GOOS+"/"+runtime.GOARCH)
		return nil
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	data := fs.String("data", app.DefaultDataDir(), "private data directory")
	endpoint := fs.String("endpoint", "", "assistant endpoint ID")
	to := fs.String("to", "", "recipient endpoint ID")
	origin := fs.String("origin", "", "original entry (defaults to current leader)")
	goal := fs.String("goal", "", "task goal or control instruction")
	contextText := fs.String("context", "", "necessary task context")
	id := fs.String("id", "", "stable task ID for retries")
	deadline := fs.String("deadline", "", "RFC3339 dispatch deadline (defaults to 30 minutes)")
	mode := fs.String("mode", "queue", "queue, steer, or interrupt")
	target := fs.String("target-run", "", "active native run ID for steer/interrupt")
	state := fs.String("state", "", "verified native session state")
	file := fs.String("file", "", "JSON file (report defaults to stdin)")
	resource := fs.String("resource", "", "shared workspace/desktop resource ID")
	requireArtifact := fs.Bool("require-artifact", false, "require a verified artifact before success")
	open := fs.Bool("open", false, "open the local status page")
	host := fs.String("host", "", "native host for skill installation")
	skillsDir := fs.String("skills-dir", "", "explicit native skill directory")
	binaryPath := fs.String("binary", "", "absolute installed AGI Hub executable path")
	hubBinaryPath := fs.String("hub-binary", "", "absolute Hub CLI path when --binary is a scoped report-only helper")
	messageID := fs.String("message-id", "", "message for delivery reconciliation")
	outcome := fs.String("outcome", "", "delivered, uncertain, or failed")
	detail := fs.String("detail", "", "minimal nonsecret delivery evidence")
	includeContent := fs.Bool("content", false, "include current skill rules (only fetch on a version change)")
	knownVersion := fs.String("known-version", "", "omit skill content when this digest is current")
	originRef := fs.String("origin-ref", "", "original human request reference (root request only)")
	correlation := fs.String("correlation-id", "", "existing collaboration ID")
	maxMessages := fs.Int("max-messages", 0, "mail conversation message limit (default 16, maximum 64)")
	mailKind := fs.String("kind", "request", "request, reply, status, or result")
	replyTo := fs.String("reply-to", "", "related message ID (does not choose the recipient)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	absoluteData, err := filepath.Abs(*data)
	if err != nil {
		return err
	}
	*data = absoluteData
	if args[0] == "init" {
		if err := app.InitConfig(*data); err != nil {
			return err
		}
		fmt.Println("Created private configuration:", filepath.Join(*data, "config.json"))
		return nil
	}
	cfg, err := app.LoadConfig(*data)
	if err != nil {
		return fmt.Errorf("load config (run init first): %w", err)
	}
	switch args[0] {
	case "mail-send":
		if *endpoint == "" {
			return errors.New("mail-send requires --endpoint")
		}
		requestBody := manager.MailSubmission{ID: *id, Recipient: *to, Kind: *mailKind, Content: *goal,
			CorrelationID: *correlation, OriginRef: *originRef, ReplyTo: *replyTo, MaxMessages: *maxMessages, Deadline: *deadline}
		if *file != "" {
			body, err := os.ReadFile(*file)
			if err != nil {
				return err
			}
			decoder := json.NewDecoder(bytes.NewReader(body))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&requestBody); err != nil {
				return err
			}
			var trailing any
			if err := decoder.Decode(&trailing); err != io.EOF {
				return errors.New("mail must contain exactly one JSON object")
			}
		}
		if requestBody.ID == "" {
			return errors.New("mail-send requires a saved stable --id or a message JSON containing id")
		}
		return request(cfg, "POST", "/v1/mail/send?endpoint="+url.QueryEscape(*endpoint), *endpoint, requestBody)
	case "mail-state", "mail-cancel":
		if *endpoint == "" || *correlation == "" {
			return errors.New("mail-state and mail-cancel require --endpoint and --correlation-id")
		}
		if args[0] == "mail-state" {
			return request(cfg, "GET", "/v1/mail/state?endpoint="+url.QueryEscape(*endpoint)+"&correlation_id="+url.QueryEscape(*correlation), *endpoint, nil)
		}
		return request(cfg, "POST", "/v1/mail/cancel?endpoint="+url.QueryEscape(*endpoint), *endpoint, map[string]string{"correlation_id": *correlation})
	case "peers", "skill-rules":
		if *endpoint == "" {
			return errors.New("peers and skill-rules require --endpoint")
		}
		path := "/v1/catalog?endpoint=" + url.QueryEscape(*endpoint)
		if args[0] == "skill-rules" {
			path = "/v1/skills?endpoint=" + url.QueryEscape(*endpoint)
			if *includeContent {
				path += "&content=1&known_version=" + url.QueryEscape(*knownVersion)
			}
		}
		return request(cfg, "GET", path, *endpoint, nil)
	case "sync-skills":
		if err := checkSkillSource(cfg); err != nil {
			return err
		}
		var results []install.SyncResult
		var err error
		if *endpoint == "" {
			results, err = install.Sync(*data)
		} else {
			results, err = install.SyncEndpoint(*data, *endpoint)
		}
		if err != nil {
			return err
		}
		if err := output(results); err != nil {
			return err
		}
		return skillSyncError(results)
	case "skills-status":
		results, err := install.Status(*data)
		if err != nil {
			return err
		}
		if *endpoint != "" {
			filtered := []install.SyncResult{}
			for _, result := range results {
				if result.EndpointID == *endpoint {
					filtered = append(filtered, result)
				}
			}
			results = filtered
		}
		return output(results)
	case "client-config":
		path, err := writeClientConfig(cfg, *endpoint, *file)
		if err != nil {
			return err
		}
		return output(map[string]string{"private_client_config": path, "next": "fill only this client's native settings; no native messages sent"})
	case "install-skill":
		if err := checkSkillSource(cfg); err != nil {
			return err
		}
		configured := false
		for _, binding := range cfg.Bindings {
			configured = configured || binding.Endpoint.ID == *endpoint
		}
		if !configured {
			return errors.New("install-skill requires an endpoint registered in this Hub configuration")
		}
		if *binaryPath == "" {
			*binaryPath, err = os.Executable()
			if err != nil {
				return err
			}
		}
		absoluteData, err := filepath.Abs(*data)
		if err != nil {
			return err
		}
		path, err := install.Skill(install.Options{Host: *host, Endpoint: *endpoint, BinaryPath: *binaryPath, HubBinaryPath: *hubBinaryPath, DataDir: absoluteData, SkillsDir: *skillsDir})
		if err != nil {
			return err
		}
		results, err := install.Sync(absoluteData)
		if err != nil {
			return fmt.Errorf("skill installed, but managed sync failed: %w", err)
		}
		if err := output(map[string]any{"installed_skill": path, "native_discovery": "must be verified in the host", "managed_sync": results}); err != nil {
			return err
		}
		return skillSyncError(results)
	case "inbox":
		if *endpoint == "" {
			return errors.New("inbox requires --endpoint")
		}
		return request(cfg, "POST", "/v1/inbox?endpoint="+url.QueryEscape(*endpoint), *endpoint, nil)
	case "delivery":
		if *endpoint == "" {
			return errors.New("delivery requires --endpoint")
		}
		return request(cfg, "POST", "/v1/delivery?endpoint="+url.QueryEscape(*endpoint), *endpoint, map[string]string{"message_id": *messageID, "outcome": *outcome, "detail": *detail})
	case "serve":
		return serve(*data, cfg, *open)
	case "doctor":
		result := map[string]any{"platform": runtime.GOOS + "/" + runtime.GOARCH, "device_id": cfg.DeviceID, "bindings": []any{}, "note": "No native messages sent. Wake verification is a configuration assertion until tested in the specified native session."}
		rows := []any{}
		for _, b := range cfg.Bindings {
			rows = append(rows, map[string]any{"endpoint": b.Endpoint.ID, "enabled": b.Endpoint.Enabled, "session_bound": b.Endpoint.SessionID != "", "idle_wake_declared_verified": b.Endpoint.IdleWakeVerified, "busy_result_delivery_declared_verified": b.Endpoint.ResultQueueWhileBusyVerified, "driver": b.Endpoint.Driver, "client_kind": b.Driver.Type})
		}
		result["bindings"] = rows
		return output(result)
	case "dashboard":
		return openDashboard(cfg)
	case "status":
		return request(cfg, "GET", "/v1/state", "", nil)
	case "leader":
		return request(cfg, "POST", "/v1/leader", "", map[string]string{"endpoint": *endpoint})
	case "session":
		return request(cfg, "POST", "/v1/session", "", map[string]string{"endpoint": *endpoint, "state": *state, "run_id": *target})
	case "submit":
		stableID := *id != ""
		if *id == "" {
			*id = newID()
		}
		sub := manager.Submission{ID: *id, Origin: *origin, Recipient: *to, Goal: *goal, Context: *contextText, Mode: *mode, TargetRunID: *target, RequireArtifact: *requireArtifact, MaxHandoffs: 1, Deadline: time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339)}
		if *deadline != "" {
			sub.Deadline = *deadline
		} else if stableID && *file == "" {
			// Repeating the same CLI request must not invent a new deadline.
			value, err := requestValue(cfg, "GET", "/v1/state", "", nil)
			if err != nil {
				return err
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				return err
			}
			var snapshot manager.Snapshot
			if err = json.Unmarshal(encoded, &snapshot); err != nil {
				return err
			}
			if old, exists := snapshot.Tasks[*id]; exists {
				sub.Deadline = old.Deadline
			}
		}
		if *resource != "" {
			sub.Resources = []string{*resource}
		}
		if *file != "" {
			b, err := os.ReadFile(*file)
			if err != nil {
				return err
			}
			if err = json.Unmarshal(b, &sub); err != nil {
				return err
			}
		}
		return request(cfg, "POST", "/v1/tasks", "", sub)
	case "report":
		if *endpoint == "" {
			return errors.New("report requires --endpoint")
		}
		var b []byte
		var err error
		if *file != "" {
			b, err = os.ReadFile(*file)
		} else {
			b, err = io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		}
		if err != nil {
			return err
		}
		var report manager.Report
		if err = json.Unmarshal(b, &report); err != nil {
			return err
		}
		if report.Endpoint != "" && report.Endpoint != *endpoint {
			return errors.New("report endpoint mismatch")
		}
		report.Endpoint = *endpoint
		return request(cfg, "POST", "/v1/report?endpoint="+url.QueryEscape(*endpoint), *endpoint, report)
	default:
		return errors.New("unknown command")
	}
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func output(v any) error { e := json.NewEncoder(os.Stdout); e.SetIndent("", "  "); return e.Encode(v) }
func request(c app.Config, method, path, endpoint string, body any) error {
	value, err := requestValue(c, method, path, endpoint, body)
	if err != nil {
		return err
	}
	return output(value)
}

func requestValue(c app.Config, method, path, endpoint string, body any) (any, error) {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	r, err := http.NewRequest(method, "http://"+c.Listen+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	token := c.AdminToken
	if endpoint != "" {
		token = app.EndpointToken(c.AdminToken, endpoint)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	const maxResponseBytes = 8 << 20 // Above the broker's 4 MiB snapshot limit.
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResponseBytes {
		return nil, errors.New("response exceeds the 8 MiB client limit")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("request failed (%d): %s", resp.StatusCode, data)
	}
	var value any
	if err = json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func serve(dir string, cfg app.Config, open bool) error {
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	// Only explicitly registered, unchanged installations are synchronized.
	// A conflict in a host skill must not prevent durable message recovery.
	if results, syncErr := install.Sync(dir); syncErr != nil {
		fmt.Fprintln(os.Stderr, "Managed skill sync:", syncErr)
	} else {
		for _, result := range results {
			fmt.Fprintln(os.Stderr, "Managed skill:", result.EndpointID, result.State, "(host load unknown)")
		}
	}
	b, err := manager.Open(dir, cfg.DeviceID, cfg.Workspaces)
	if err != nil {
		return err
	}
	defer b.Close()
	configured := map[string]bool{}
	for _, binding := range cfg.Bindings {
		if err = b.Register(binding.Endpoint); err != nil {
			return err
		}
		configured[binding.Endpoint.ID] = true
	}
	for id, e := range b.Snapshot().Endpoints {
		if !configured[id] {
			e.Enabled = false
			if err = b.Register(e); err != nil {
				return err
			}
		}
	}
	if b.Snapshot().Leader == "" && cfg.Leader != "" {
		if err = b.SetLeader(cfg.Leader); err != nil {
			return err
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	appServer := &app.Server{Broker: b, Config: cfg}
	if err = appServer.StartManagement(ctx, dir); err != nil {
		return err
	}
	defer appServer.CloseManagement()
	server := http.Server{Handler: appServer.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	errs := make(chan error, 1)
	go func() { errs <- server.Serve(listener) }()
	fmt.Println("AGI Hub listening on http://" + cfg.Listen + " (native session status must be verified after startup)")
	if open {
		if err = openDashboard(cfg); err != nil {
			fmt.Fprintln(os.Stderr, "Could not open browser:", err)
		}
	}
	select {
	case <-ctx.Done():
	case err = <-errs:
		stop()
	}
	stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	_ = server.Close()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func skillSyncError(results []install.SyncResult) error {
	for _, result := range results {
		if result.State != "current" && result.State != "updated" {
			return errors.New("some managed skills were not synchronized; inspect the per-installation results")
		}
	}
	return nil
}

// Refuse to overwrite files using an executable that disagrees with the running
// Hub. An absent Hub permits offline maintenance, still guarded by the registry's
// monotonic source revision. Timeouts/auth failures are not treated as absence.
func checkSkillSource(cfg app.Config) error {
	value, err := requestValue(cfg, "GET", "/v1/skill-release", "", nil)
	if err != nil {
		if connectionRefused(err) {
			return nil
		}
		return fmt.Errorf("cannot verify skill source against running Hub; update/restart the Hub before syncing: %w", err)
	}
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var release app.SkillRules
	if err := json.Unmarshal(body, &release); err != nil {
		return err
	}
	_, version, err := install.CurrentSkill()
	if err != nil {
		return err
	}
	if release.ProtocolVersion != 1 || release.Revision != install.SkillRevision || release.Version != version {
		return errors.New("running Hub and this CLI provide different skill rules; use the matching Hub executable before syncing")
	}
	return nil
}

func openDashboard(c app.Config) error {
	address := "http://" + c.Listen + "/#" + url.PathEscape(c.AdminToken)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", address)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", address)
	default:
		cmd = exec.Command("xdg-open", address)
	}
	return cmd.Run()
}
