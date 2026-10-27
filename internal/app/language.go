package app

import (
	"errors"
	"path/filepath"

	"agihub/internal/install"
	"agihub/internal/language"
)

// The management mutex is held by the authenticated caller. Language changes
// neither rebind a native session nor acquire/release a Client lease.
func (s *Server) manageAgentLanguage(id, value string) (any, error) {
	normalized, err := language.Normalize(value)
	if err != nil {
		return nil, err
	}
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	old, ok := s.Broker.Snapshot().Endpoints[id]
	if !ok {
		return nil, errors.New("unknown local endpoint")
	}
	cfg, err := LoadConfig(s.management.dir)
	if err != nil {
		return nil, err
	}
	updated := old
	updated.CommunicationLanguage = normalized
	found := false
	for i := range cfg.Bindings {
		if cfg.Bindings[i].Endpoint.ID == id {
			updated.MailboxNativeQueueVerified = configuredNativeQueueVerification(cfg.Bindings[i], updated)
			cfg.Bindings[i].Endpoint = updated
			found = true
		}
	}
	if !found {
		return nil, errors.New("endpoint missing from local configuration")
	}
	if err := s.Broker.Register(updated); err != nil {
		return nil, err
	}
	if err := writePrivateJSON(filepath.Join(s.management.dir, "config.json"), cfg); err != nil {
		return nil, errors.Join(err, s.Broker.Register(old))
	}
	results, syncErr := install.Sync(s.management.dir)
	complete := syncErr == nil
	for _, result := range results {
		complete = complete && (result.State == "current" || result.State == "updated")
	}
	detail := "语言已保存，已同步登记的受管技能。运行中的 Agent 需重新读取技能和成员目录；面向用户仍使用用户指定的语言。"
	if len(results) == 0 && syncErr == nil {
		detail = "语言已保存。尚无已登记的技能安装；安装后会带上当前语言规则。面向用户仍使用用户指定的语言。"
	}
	if !complete {
		detail = "语言已保存，但部分技能未能同步。请查看同步结果并处理冲突；未覆盖手动改动，也未调用模型。"
	}
	response := map[string]any{
		"ok": true, "communication_language": language.Effective(updated.Driver, updated.CommunicationLanguage),
		"skill_sync": results, "skill_sync_complete": complete, "detail": detail,
	}
	if syncErr != nil {
		response["skill_sync_error"] = syncErr.Error()
	}
	s.Broker.Signal()
	return response, nil
}
