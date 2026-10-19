// An endpoint-fixed collaboration tool. No administrator credential or native
// assistant execution is available through this binary.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agihub/internal/manager"
)

const maxJSON = 1 << 20

type binding struct {
	Endpoint      string `json:"endpoint"`
	BaseURL       string `json:"base_url"`
	EndpointToken string `json:"endpoint_token"`
	WorkspaceRoot string `json:"workspace_root"`
	DataDir       string `json:"data_dir"`
}

type mailSubmission struct {
	ID            string `json:"id"`
	Recipient     string `json:"recipient"`
	Kind          string `json:"kind"`
	Content       string `json:"content"`
	CorrelationID string `json:"correlation_id,omitempty"`
	OriginRef     string `json:"origin_ref,omitempty"`
	ReplyTo       string `json:"reply_to,omitempty"`
	Deadline      string `json:"deadline,omitempty"`
	MaxMessages   int    `json:"max_messages,omitempty"`
}

func mailKind(kind string) bool {
	return kind == "request" || kind == "reply" || kind == "status" || kind == "result"
}

func main() {
	executable, err := os.Executable()
	if err == nil {
		err = run(filepath.Join(filepath.Dir(executable), "binding.json"), os.Args[1:], os.Stdin, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(filename string, args []string, in io.Reader, out io.Writer) error {
	var cfg binding
	raw, err := readFile(filename)
	if err != nil {
		return errors.New("scoped binding unavailable")
	}
	if err := strictJSON(raw, &cfg); err != nil {
		return errors.New("invalid scoped binding")
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	if len(args) == 0 {
		return errors.New("command required: report, peers, mail-send, mail-state")
	}
	command := args[0]
	if command != "report" && command != "peers" && command != "mail-send" && command != "mail-state" {
		return errors.New("unsupported scoped command")
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	endpoint := fs.String("endpoint", cfg.Endpoint, "fixed endpoint")
	data := fs.String("data", cfg.DataDir, "fixed Hub data directory")
	var file, correlation string
	if command == "report" || command == "mail-send" {
		fs.StringVar(&file, "file", "", "JSON file within the selected workspace")
	}
	if command == "mail-state" {
		fs.StringVar(&correlation, "correlation-id", "", "known collaboration ID")
	}
	if err := fs.Parse(args[1:]); err != nil {
		if command == "mail-send" {
			return fmt.Errorf("unsupported_mail_flags: %s", mailSchema)
		}
		return errors.New("invalid scoped command flags")
	}
	if fs.NArg() != 0 || *endpoint != cfg.Endpoint || !filepath.IsAbs(*data) || filepath.Clean(*data) != filepath.Clean(cfg.DataDir) {
		return errors.New("cannot replace the fixed endpoint or data directory")
	}
	method, route := http.MethodGet, "catalog"
	var body []byte
	if command == "mail-state" {
		if correlation == "" {
			return errors.New("correlation-id is required")
		}
		route = "mail/state"
	}
	if command == "report" || command == "mail-send" {
		if file == "" || file == "-" {
			if command == "mail-send" {
				return errors.New("mail-send requires a saved request file")
			}
			body, err = io.ReadAll(io.LimitReader(in, maxJSON+1))
		} else {
			body, err = workspaceFile(cfg.WorkspaceRoot, file)
		}
		if err != nil {
			return err
		}
		if len(body) > maxJSON {
			return errors.New("JSON exceeds size limit")
		}
		if command == "report" {
			var report manager.Report
			if err := strictJSON(body, &report); err != nil {
				return err
			}
			if report.Endpoint != cfg.Endpoint {
				return errors.New("report endpoint mismatch")
			}
			route = "report"
		} else {
			if err := validateMail(body); err != nil {
				return err
			}
			route = "mail/send"
		}
		method = http.MethodPost
	}
	target, _ := url.Parse(cfg.BaseURL + "/v1/" + route)
	query := target.Query()
	query.Set("endpoint", cfg.Endpoint)
	if command == "mail-state" {
		query.Set("correlation_id", correlation)
	}
	target.RawQuery = query.Encode()
	request, err := http.NewRequest(method, target.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+cfg.EndpointToken)
	request.Header.Set("Origin", cfg.BaseURL)
	request.Header.Set("Content-Type", "application/json")
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 10 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("request unconfirmed; preserve saved request/event ID and reconcile")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Hub rejected scoped request (HTTP %d); keep saved ID", response.StatusCode)
	}
	value, err := io.ReadAll(io.LimitReader(response.Body, maxJSON+1))
	if err != nil || len(value) > maxJSON {
		return errors.New("response unconfirmed; preserve saved ID")
	}
	if !json.Valid(value) {
		return errors.New("invalid Hub response; preserve saved ID")
	}
	_, err = out.Write(value)
	return err
}

const mailSchema = `mail-send --file <workspace JSON> requires {"id":"saved-stable-id","recipient":"endpoint","kind":"request|reply|status|result","content":"brief message"}; optional fields: correlation_id, origin_ref, reply_to, deadline, max_messages. Recipient is a JSON field; no --peer/--to/--collaboration-flag.`

func validateMail(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := strictJSON(raw, &fields); err != nil || fields == nil {
		return fmt.Errorf("mail_json_object_required: %s", mailSchema)
	}
	allowed := map[string]bool{"id": true, "recipient": true, "kind": true, "content": true, "correlation_id": true, "origin_ref": true, "reply_to": true, "deadline": true, "max_messages": true}
	for name := range fields {
		if !allowed[name] {
			return fmt.Errorf("unsupported_mail_fields: %s", mailSchema)
		}
	}
	var request mailSubmission
	if err := strictJSON(raw, &request); err != nil {
		return fmt.Errorf("mail_field_type_invalid: %s", mailSchema)
	}
	if strings.TrimSpace(request.ID) == "" {
		return fmt.Errorf("mail_id_required: save a stable caller-generated id before sending; %s", mailSchema)
	}
	if strings.TrimSpace(request.Recipient) == "" {
		return fmt.Errorf("mail_recipient_required: %s", mailSchema)
	}
	if !mailKind(request.Kind) {
		return fmt.Errorf("mail_kind_invalid: %s", mailSchema)
	}
	if strings.TrimSpace(request.Content) == "" {
		return fmt.Errorf("mail_content_required: %s", mailSchema)
	}
	return nil
}

func (cfg binding) validate() error {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return errors.New("binding requires a fixed loopback HTTP URL")
	}
	host, _, err := net.SplitHostPort(u.Host)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return errors.New("binding requires a numeric loopback address")
	}
	if cfg.Endpoint == "" || len(cfg.EndpointToken) < 32 || !filepath.IsAbs(cfg.DataDir) || !filepath.IsAbs(cfg.WorkspaceRoot) {
		return errors.New("incomplete scoped binding")
	}
	if err := rejectLinks(cfg.WorkspaceRoot); err != nil {
		return err
	}
	info, err := os.Stat(cfg.WorkspaceRoot)
	if err != nil || !info.IsDir() {
		return errors.New("selected workspace unavailable")
	}
	return nil
}

func strictJSON(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return errors.New("invalid JSON schema")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return errors.New("expected one JSON object")
	}
	return nil
}

func rejectLinks(filename string) error {
	for current := filename; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symbolic links are not accepted")
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}

func readFile(filename string) ([]byte, error) {
	if err := rejectLinks(filename); err != nil {
		return nil, err
	}
	before, err := os.Lstat(filename)
	if err != nil || !before.Mode().IsRegular() || before.Size() > maxJSON {
		return nil, errors.New("JSON must be a regular file within size limit")
	}
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, errors.New("JSON file changed before reading")
	}
	body, err := io.ReadAll(io.LimitReader(f, maxJSON+1))
	if err != nil || len(body) > maxJSON {
		return nil, errors.New("JSON read failed or exceeded size limit")
	}
	after, err := os.Lstat(filename)
	if err != nil || !os.SameFile(before, after) {
		return nil, errors.New("JSON file changed while reading")
	}
	if err := rejectLinks(filename); err != nil {
		return nil, err
	}
	return body, nil
}

func workspaceFile(root, filename string) ([]byte, error) {
	if !filepath.IsAbs(filename) {
		filename = filepath.Join(root, filename)
	}
	rel, err := filepath.Rel(root, filename)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, errors.New("JSON file is outside selected workspace")
	}
	return readFile(filename)
}
