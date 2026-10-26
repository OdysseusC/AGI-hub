package app

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agihub/internal/discovery"
	"agihub/internal/install"
	"agihub/internal/language"
)

type discoveryPreferences struct {
	Enabled   bool   `json:"enabled"`
	Interface string `json:"interface"`
}

func readDiscoveryPreferences(dir string) (discoveryPreferences, error) {
	var prefs discoveryPreferences
	b, err := os.ReadFile(filepath.Join(dir, "discovery.json"))
	if os.IsNotExist(err) {
		return prefs, nil
	}
	if err != nil {
		return prefs, err
	}
	err = json.Unmarshal(b, &prefs)
	return prefs, err
}

// Keep private configuration writes on the same volume and never truncate the
// previous file in place. No arbitrary destination is accepted by the API.
func writePrivateJSON(path string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".hub-settings-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(append(body, '\n'))
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (s *Server) manage(w http.ResponseWriter, r *http.Request) (any, error) {
	m := s.management
	if m == nil {
		return nil, errors.New("management runtime unavailable; start the Hub with the current executable")
	}
	if r.URL.Path == "/v1/manage" && r.Method == http.MethodGet {
		return s.managementState()
	}
	if r.Method != http.MethodPost {
		return nil, errors.New("unsupported management operation")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch r.URL.Path {
	case "/v1/manage/settings":
		var in struct {
			Name             string `json:"name"`
			NetworkEnabled   bool   `json:"network_enabled"`
			DiscoveryEnabled bool   `json:"discovery_enabled"`
			Listen           string `json:"listen"`
			Interface        string `json:"interface"`
		}
		if err := decode(w, r, &in); err != nil {
			return nil, err
		}
		if in.DiscoveryEnabled && !in.NetworkEnabled {
			return nil, errors.New("enable LAN before discovery")
		}
		old := m.mesh.Snapshot().Config
		oldDiscovery, oldInterface, _ := m.discovery.State()
		cfg := old
		cfg.Name = in.Name
		cfg.Enabled = in.NetworkEnabled
		cfg.ListenAddr = in.Listen
		cfg.AdvertiseAddr = in.Listen
		if !in.NetworkEnabled {
			// No announcement is made while disabled. In particular a wildcard
			// listener must not be validated as an advertised destination.
			cfg.AdvertiseAddr = ""
		}
		if host, port, err := net.SplitHostPort(in.Listen); err == nil && (host == "" || net.ParseIP(host) != nil && net.ParseIP(host).IsUnspecified()) && in.NetworkEnabled {
			found := ""
			if interfaces, err := discovery.Interfaces(); err == nil {
				for _, candidate := range interfaces {
					if candidate.Name == in.Interface {
						for _, address := range candidate.Addresses {
							if ip := net.ParseIP(address); ip != nil && ip.To4() != nil {
								found = address
								break
							}
						}
					}
				}
			}
			if found == "" {
				return nil, errors.New("select a LAN interface or enter its numeric IP address for the listener")
			}
			cfg.AdvertiseAddr = net.JoinHostPort(found, port)
		}
		if err := m.discovery.Configure(m.ctx, false, ""); err != nil {
			return nil, err
		}
		if err := m.mesh.Configure(cfg); err != nil {
			rollback := m.mesh.Configure(old)
			if rollback == nil {
				rollback = m.discovery.Configure(m.ctx, oldDiscovery, oldInterface)
			}
			return nil, errors.Join(err, rollback)
		}
		if err := m.discovery.Configure(m.ctx, in.DiscoveryEnabled, in.Interface); err != nil {
			rollback := m.mesh.Configure(old)
			if rollback == nil {
				rollback = m.discovery.Configure(m.ctx, oldDiscovery, oldInterface)
			}
			return nil, errors.Join(err, rollback)
		}
		if err := writePrivateJSON(filepath.Join(m.dir, "discovery.json"), discoveryPreferences{in.DiscoveryEnabled, in.Interface}); err != nil {
			_ = m.discovery.Configure(m.ctx, false, "")
			rollback := m.mesh.Configure(old)
			if rollback == nil {
				rollback = m.discovery.Configure(m.ctx, oldDiscovery, oldInterface)
			}
			return nil, errors.Join(err, rollback)
		}
		s.Broker.Signal()
		m.networkError = ""
		return map[string]any{"ok": true, "detail": "已保存。网络连接和发现状态不代表原生 Agent 已完成执行。"}, nil
	case "/v1/manage/agent":
		var in struct {
			Endpoint              string  `json:"endpoint"`
			Action                string  `json:"action"`
			SessionID             string  `json:"session_id,omitempty"`
			CommunicationLanguage *string `json:"communication_language,omitempty"`
		}
		if err := decode(w, r, &in); err != nil {
			return nil, err
		}
		if in.Action == "language" {
			if in.CommunicationLanguage == nil || in.SessionID != "" {
				return nil, errors.New("language action requires communication_language and cannot change a session binding")
			}
			return s.manageAgentLanguage(in.Endpoint, *in.CommunicationLanguage)
		}
		if in.CommunicationLanguage != nil {
			return nil, errors.New("communication_language requires the language action")
		}
		return s.manageAgent(in.Endpoint, in.Action, in.SessionID)
	case "/v1/manage/network":
		var in struct {
			Action     string `json:"action"`
			PeerID     string `json:"peer_id,omitempty"`
			Address    string `json:"address,omitempty"`
			Invitation string `json:"invitation,omitempty"`
		}
		if err := decode(w, r, &in); err != nil {
			return nil, err
		}
		switch in.Action {
		case "invite":
			invite, err := m.mesh.Invite(5 * time.Minute)
			return map[string]any{"ok": err == nil, "invitation": invite.Code, "expires_at": invite.ExpiresAt, "detail": "将邀请交给另一台电脑；收到请求后仍需在本机确认。"}, err
		case "pair":
			request, err := m.mesh.RequestPairAt(r.Context(), in.Address, in.Invitation)
			return map[string]any{"ok": err == nil, "detail": "已提交配对请求，请在对方 Hub 确认身份。", "request": request}, err
		case "accept":
			err := m.mesh.AcceptPair(r.Context(), in.PeerID)
			return map[string]any{"ok": err == nil, "detail": "配对完成。只有明确共享的 Agent 会进入目录。"}, err
		case "disconnect":
			err := m.mesh.Disconnect(in.PeerID)
			return map[string]any{"ok": err == nil, "detail": "已断开并撤销该 Hub 的信任；重新连接需要重新配对。"}, err
		}
	}
	return nil, errors.New("unsupported management action")
}

func (s *Server) manageAgent(id, action, sessionID string) (any, error) {
	m := s.management
	e, ok := s.Broker.Snapshot().Endpoints[id]
	if !ok {
		return nil, errors.New("unknown local endpoint")
	}
	switch action {
	case "share", "unshare":
		if action == "share" && (!e.Enabled || e.SessionID == "" || e.MailboxProtocolVersion != 1) {
			return nil, errors.New("sharing requires an enabled, bound mailbox protocol 1 endpoint")
		}
		cfg := m.mesh.Snapshot().Config
		exports := []string{}
		for _, v := range cfg.Exports {
			if v != id {
				exports = append(exports, v)
			}
		}
		if action == "share" {
			exports = append(exports, id)
		}
		cfg.Exports = exports
		err := m.mesh.Configure(cfg)
		s.Broker.Signal()
		return map[string]any{"ok": err == nil, "detail": "共享允许此 Agent 与已配对 Hub 的共享 Agent 交换消息；不会增加其工具权限。"}, err
	case "enable", "disable":
		s.leaseMu.Lock()
		defer s.leaseMu.Unlock()
		cfg, err := LoadConfig(m.dir)
		if err != nil {
			return nil, err
		}
		old := e
		if sessionID != "" && sessionID != e.SessionID {
			e.SessionID = sessionID
			e.IdleWakeVerified = false
			e.ResultQueueWhileBusyVerified = false
			e.MailboxWhileBusyVerified = false
			e.PeerWhileBusyVerified = false
		}
		e.Enabled = action == "enable"
		if e.Enabled && e.MailboxProtocolVersion == 0 {
			// Enable only capabilities implemented by this release's Clients.
			// In particular Grok Routine still consumes the legacy task inbox.
			switch e.Driver {
			case "codex", "workbuddy", "claude-code":
				e.MailboxProtocolVersion = 1
			}
		}
		if e.Enabled && strings.TrimSpace(e.SessionID) == "" {
			return nil, errors.New("a real native session ID is required; installation cannot create or guess it")
		}
		found := false
		for i := range cfg.Bindings {
			if cfg.Bindings[i].Endpoint.ID == id {
				cfg.Bindings[i].Endpoint = e
				found = true
			}
		}
		if !found {
			return nil, errors.New("endpoint missing from local configuration")
		}
		if err = s.Broker.Register(e); err != nil {
			return nil, err
		}
		if err = writePrivateJSON(filepath.Join(m.dir, "config.json"), cfg); err != nil {
			return nil, errors.Join(err, s.Broker.Register(old))
		}
		if old.SessionID != e.SessionID {
			_ = s.Broker.SetSession(id, "unknown", "")
		}
		s.Broker.Signal()
		return map[string]any{"ok": true, "detail": "已保存绑定。请按接入文档运行独立 Client；原生唤醒能力须另外验收。"}, nil
	case "install":
		host := ""
		switch e.Driver {
		case "codex":
			host = "codex"
		case "workbuddy":
			host = "workbuddy"
		case "claude-code":
			host = "claude"
		}
		if host == "" {
			return map[string]any{"ok": true, "detail": "此平台需要手动配置原生入口；未执行安装。", "steps": []string{"按该 Client 的 README 配置原生例程和私有配置。", "填写真实会话 ID 后再启用。"}}, nil
		}
		registered, err := install.StatusEndpoint(m.dir, id)
		if err != nil {
			return nil, err
		}
		if len(registered) != 0 {
			// Registered inputs include the scoped helper, custom profile and
			// maintenance binary. Do not replace them with the current Hub.
			results, err := install.SyncEndpoint(m.dir, id)
			if err != nil {
				return nil, err
			}
			complete := len(results) != 0
			for _, result := range results {
				complete = complete && (result.State == "current" || result.State == "updated")
			}
			detail := "协作技能已按原登记同步，保留原安装目录和固定身份程序；运行会话是否重新读取仍须核实。"
			if !complete {
				detail = "部分已登记技能未能同步；保留原安装位置、固定身份程序、手动修改和卸载，请查看同步结果。"
			}
			s.Broker.Signal()
			return map[string]any{"ok": true, "detail": detail, "skill_sync": results, "skill_sync_complete": complete}, nil
		}
		executable, err := os.Executable()
		if err != nil {
			return nil, err
		}
		_, err = install.Skill(install.Options{Host: host, Endpoint: id, BinaryPath: executable, DataDir: m.dir})
		s.Broker.Signal()
		return map[string]any{"ok": err == nil, "detail": "协作技能已写入磁盘；原生 Client、插件加载和会话唤醒尚未验证。", "steps": []string{
			"按照 clients/" + e.Driver + " 文档配置独立 Client 和私有 endpoint 凭据。",
			"填写真实原生会话 ID。不得用安装成功代替原生接入验收。",
			"安装过程不会调用任何模型。Claude 调试仍须遵守用户指定的模型和预算。",
		}}, err
	}
	return nil, errors.New("unsupported agent action")
}

func (s *Server) managementState() (any, error) {
	m := s.management
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.mesh.Snapshot()
	discoveryEnabled, iface, discoveryErr := m.discovery.State()
	agents := []map[string]any{}
	snapshot := s.Broker.Snapshot()
	installed := map[string]string{}
	results, installErr := install.Status(m.dir)
	for _, r := range results {
		installed[r.EndpointID] = r.State
	}
	for _, e := range snapshot.Endpoints {
		status := "disabled"
		detail := "尚未启用"
		if e.Enabled {
			status = "unknown"
			detail = "已启用，绑定和原生 Client 连接状态尚未核实"
			if e.SessionID != "" {
				status = "bound"
				detail = "已有会话已绑定，Client 连接状态尚未验证；请核对本机 Client 输出和实际消息。"
			}
			s.leaseMu.Lock()
			lease, exists := s.leases[e.ID]
			connected := exists && time.Now().Before(lease.expiresAt) && lease.binding == fingerprint(e)
			s.leaseMu.Unlock()
			if connected {
				status = "connected"
				detail = "独立 Client 已连接，原生执行须另看真实回报"
			}
			if connected && e.IdleWakeVerified && snapshot.Sessions[e.ID].State == "idle" {
				status = "verified"
				detail = "当前会话空闲，已配置经验证的唤醒能力"
			}
		}
		installation := "not_installed"
		if installed[e.ID] == "current" {
			installation = "installed"
		} else if installed[e.ID] != "" {
			installation = installed[e.ID]
		}
		if installErr != nil {
			installation = "unknown"
			detail = "安装记录读取失败：" + installErr.Error()
		}
		actions := []string{"install", "language"}
		if e.Enabled {
			actions = append(actions, "disable")
		} else {
			actions = append(actions, "enable")
		}
		if contains(state.Config.Exports, e.ID) {
			actions = append(actions, "unshare")
		} else if e.Enabled && e.MailboxProtocolVersion == 1 {
			actions = append(actions, "share")
		}
		agents = append(agents, map[string]any{"id": e.ID, "name": e.ID, "driver": e.Driver, "enabled": e.Enabled, "shared": contains(state.Config.Exports, e.ID), "session_bound": e.SessionID != "", "installation_state": installation, "state": status, "detail": detail, "actions": actions,
			"communication_language": language.Effective(e.Driver, e.CommunicationLanguage)})
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i]["id"].(string) < agents[j]["id"].(string) })
	peers := []map[string]any{}
	routes := map[string][]string{}
	for _, route := range state.Routes {
		routes[route.HubID] = route.Path
	}
	for _, p := range state.Peers {
		status := "offline"
		if p.Online {
			status = "connected"
		} else if p.Reachable {
			status = "relayed"
		}
		children := []map[string]string{}
		for _, a := range p.Agents {
			children = append(children, map[string]string{"id": a.ID, "name": a.Name, "state": a.Status,
				"communication_language": language.Effective(a.Driver, a.CommunicationLanguage)})
		}
		peers = append(peers, map[string]any{"id": p.HubID, "name": p.Name, "address": p.Address, "state": status, "route": routes[p.HubID], "agents": children})
	}
	pending := []map[string]string{}
	for _, p := range state.Pending {
		direction := "outgoing"
		if p.Direction == "inbound" {
			direction = "incoming"
		}
		pending = append(pending, map[string]string{"id": p.ID, "hub_id": p.HubID, "name": p.Name, "address": p.Address, "direction": direction})
	}
	discovered := []map[string]string{}
	for _, p := range state.Candidates {
		discovered = append(discovered, map[string]string{"id": p.HubID, "name": p.Name, "address": p.Address, "state": "discovered"})
	}
	interfaces := []map[string]any{}
	if list, err := discovery.Interfaces(); err == nil {
		for _, v := range list {
			interfaces = append(interfaces, map[string]any{"name": v.Name, "addresses": v.Addresses})
		}
	}
	return map[string]any{
		"hub":    map[string]any{"id": state.HubID, "name": state.Name, "network_enabled": state.Config.Enabled, "discovery_enabled": discoveryEnabled, "listen": state.Config.ListenAddr, "interface": iface, "discovery_error": discoveryErr, "network_error": m.networkError},
		"agents": agents, "network": map[string]any{"peers": peers, "pending": pending, "discovered": discovered, "interfaces": interfaces},
	}, nil
}
