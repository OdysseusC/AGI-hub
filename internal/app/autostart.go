package app

import (
	"errors"
	"os"

	"agihub/internal/autostart"
)

// StartupStatus deliberately omits executable arguments, environment and native
// credentials. Process state and actual mailbox readiness remain separate.
func StartupStatus(dir string) (map[string]any, error) {
	p, err := autostart.Load(dir)
	if err != nil {
		return nil, err
	}
	binary := p.Binary
	if binary == "" {
		binary, _ = os.Executable()
	}
	platform, err := autostart.PlatformStatus(autostart.RegistrationFor(dir, binary))
	if err != nil {
		return nil, err
	}
	detail := "尚未配置系统登录项，默认关闭。"
	if !platform.Supported {
		detail = "当前系统不支持原生登录自启动；Windows 和 macOS 分别使用任务计划与 launchd。"
	} else if platform.Installed {
		if p.Enabled && platform.Enabled {
			detail = "已配置下次用户登录时启动；当前进程和业务任务不受设置变更影响。"
		} else if p.Enabled != platform.Enabled {
			detail = "保存的设置与系统登录项状态不一致，请在目标本机核对稳定安装路径并重新保存。"
		} else {
			detail = "系统登录触发已关闭，当前运行状态单独记录。"
		}
	}
	run, err := autostart.ReadRuntime(dir)
	if err != nil {
		return nil, err
	}
	active, err := autostart.RuntimeActive(dir)
	if err != nil {
		return nil, err
	}
	clients := []map[string]any{}
	for _, c := range p.Clients {
		state, detail := "configured", "已登记，启动成功仍须核对 Client 原生连接。"
		if c.RequiresHost {
			state, detail = "needs_host_environment", "需要原生宿主启动；登录项不保存或伪造宿主 IPC 环境。"
		}
		clients = append(clients, map[string]any{"endpoint": c.Endpoint, "enabled": c.Enabled, "requires_host": c.RequiresHost, "state": state, "detail": detail})
	}
	return map[string]any{"supported": platform.Supported, "installed": platform.Installed, "enabled": p.Enabled && platform.Enabled, "configured_enabled": p.Enabled, "mechanism": platform.Mechanism, "detail": detail, "clients": clients, "runtime": run, "runtime_active": active, "applies": "next_user_login"}, nil
}

// RegisterStartupClient is a local CLI operation, never a remote HTTP command.
func RegisterStartupClient(dir string, client autostart.Client) error {
	cfg, err := LoadConfig(dir)
	if err != nil {
		return err
	}
	found := false
	for _, b := range cfg.Bindings {
		if b.Endpoint.ID != client.Endpoint {
			continue
		}
		found = true
		if b.Endpoint.Driver == "codex" || b.Endpoint.Driver == "claude-code" {
			client = autostart.Client{Endpoint: client.Endpoint, RequiresHost: true}
		}
	}
	if !found {
		return errors.New("startup Client must reference a configured local endpoint")
	}
	return autostart.Register(dir, client)
}

func ConfigureStartup(dir, binary string, enabled bool, selected []string) error {
	cfg, err := LoadConfig(dir)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, b := range cfg.Bindings {
		known[b.Endpoint.ID] = true
	}
	for _, id := range selected {
		if !known[id] {
			return errors.New("startup selection contains an unknown local endpoint")
		}
	}
	return autostart.Configure(dir, binary, enabled, selected)
}
