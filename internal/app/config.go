package app

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"agihub/internal/language"
	"agihub/internal/manager"
)

// ClientBinding selects independent consumption, not a native transport. The
// Hub never loads executable paths, webhook secrets, or assistant credentials.
type ClientBinding struct {
	Type string `json:"type"`
}

type Binding struct {
	Endpoint manager.Endpoint `json:"endpoint"`
	Driver   ClientBinding    `json:"driver"`
	// Directory visibility does not grant task submission rights.
	VisiblePeers            []string                 `json:"visible_peers,omitempty"`
	AllowedPeers            []string                 `json:"allowed_peers,omitempty"`
	Purpose                 string                   `json:"purpose,omitempty"`
	NativeQueueVerification *NativeQueueVerification `json:"mailbox_native_queue_verification,omitempty"`
}

type Config struct {
	Version    int               `json:"version"`
	DeviceID   string            `json:"device_id"`
	Listen     string            `json:"listen"`
	AdminToken string            `json:"admin_token"`
	Leader     string            `json:"leader,omitempty"`
	Workspaces map[string]string `json:"workspaces"`
	Bindings   []Binding         `json:"bindings"`
}

func DefaultDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".", "private")
	}
	return filepath.Join(base, "agi-hub")
}

func EndpointToken(admin, endpoint string) string {
	mac := hmac.New(sha256.New, []byte(admin))
	mac.Write([]byte("agi-hub/endpoint/" + endpoint))
	return hex.EncodeToString(mac.Sum(nil))
}

func InitConfig(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	cfg := Config{Version: 1, DeviceID: "this-device", Listen: "127.0.0.1:7331", AdminToken: hex.EncodeToString(buf), Workspaces: map[string]string{}, Bindings: []Binding{
		{Endpoint: manager.Endpoint{ID: "codex", DeviceID: "this-device", Modes: []string{"queue"}, Driver: "codex"}, Driver: ClientBinding{Type: "external"}},
		{Endpoint: manager.Endpoint{ID: "workbuddy", DeviceID: "this-device", Modes: []string{"queue"}, Driver: "workbuddy"}, Driver: ClientBinding{Type: "external"}},
		{Endpoint: manager.Endpoint{ID: "claude", DeviceID: "this-device", Modes: []string{"queue"}, Driver: "claude-code"}, Driver: ClientBinding{Type: "external"}},
		{Endpoint: manager.Endpoint{ID: "grok", DeviceID: "this-device", Modes: []string{"queue"}, Driver: "grok-routine"}, Driver: ClientBinding{Type: "external"}},
	}}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "config.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("create config without overwriting: %w", err)
	}
	_, werr := f.Write(append(data, '\n'))
	if werr == nil {
		werr = f.Sync()
	}
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

func LoadConfig(dir string) (Config, error) {
	var c Config
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return c, err
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&c); err != nil {
		return c, fmt.Errorf("invalid Hub configuration: %w; native transport settings belong in a private Client config; binding driver.type must be external", err)
	}
	var trailing any
	if err = decoder.Decode(&trailing); err != io.EOF {
		return c, errors.New("Hub configuration must contain exactly one JSON object")
	}
	if c.Version != 1 || c.DeviceID == "" || len(c.AdminToken) < 32 {
		return c, errors.New("config requires version 1, device_id, and a private admin_token of at least 32 characters")
	}
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return c, errors.New("listen must be a numeric loopback address, such as 127.0.0.1:7331")
	}
	seen := map[string]bool{}
	for i := range c.Bindings {
		b := &c.Bindings[i]
		value, err := language.Normalize(b.Endpoint.CommunicationLanguage)
		if err != nil {
			return c, err
		}
		if b.Endpoint.CommunicationLanguage != "" {
			b.Endpoint.CommunicationLanguage = value
		}
		if b.Endpoint.ID == "" || seen[b.Endpoint.ID] {
			return c, errors.New("endpoint IDs must be nonempty and unique")
		}
		seen[b.Endpoint.ID] = true
		if b.Endpoint.DeviceID != c.DeviceID {
			return c, errors.New("v0 bindings must belong to this device; remote file transport is not implemented")
		}
		if b.Endpoint.PeerProtocolVersion < 0 || b.Endpoint.PeerProtocolVersion > 1 || b.Endpoint.PeerWhileBusyVerified && b.Endpoint.PeerProtocolVersion != 1 {
			return c, errors.New("peer_protocol_version must be 0 or 1; peer_while_busy_verified requires version 1")
		}
		if b.Endpoint.MailboxProtocolVersion < 0 || b.Endpoint.MailboxProtocolVersion > 1 || b.Endpoint.MailboxWhileBusyVerified && b.Endpoint.MailboxProtocolVersion != 1 {
			return c, errors.New("mailbox_protocol_version must be 0 or 1; mailbox_while_busy_verified requires version 1")
		}
		if b.Endpoint.MailboxNativeQueueVerified && !nativeQueueVerificationMatches(*b) {
			b.Endpoint.MailboxNativeQueueVerified = false
		}
		if b.Driver.Type == "" {
			b.Driver.Type = "external"
		}
		if b.Driver.Type != "external" {
			return c, errors.New("binding driver.type must be external; built-in native drivers have moved to independent Clients with private client.json settings")
		}
	}
	for id, root := range c.Workspaces {
		if id == "" || !filepath.IsAbs(root) {
			return c, errors.New("workspace roots must have an ID and an absolute local path")
		}
	}
	for _, binding := range c.Bindings {
		if len(binding.Purpose) > 240 || strings.IndexFunc(binding.Purpose, unicode.IsControl) != -1 {
			return c, errors.New("binding purpose must be short single-line text (at most 240 bytes)")
		}
		for _, list := range [][]string{binding.VisiblePeers, binding.AllowedPeers} {
			peers := map[string]bool{}
			for _, id := range list {
				if !seen[id] || id == binding.Endpoint.ID || peers[id] {
					return c, errors.New("visible_peers and allowed_peers must contain unique configured endpoints other than self")
				}
				peers[id] = true
			}
		}
	}
	return c, nil
}
