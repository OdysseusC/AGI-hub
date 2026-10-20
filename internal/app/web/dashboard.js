(function (root, factory) {
  "use strict";
  const ui = factory();
  if (typeof module === "object" && module.exports) module.exports = ui;
  else ui.start(root);
})(typeof window === "undefined" ? globalThis : window, function () {
  "use strict";

  const status = (label, tone = "neutral") => ({ label, tone });
  const installationStates = {
    not_installed: status("尚未安装"), installed: status("文件已安装", "soft"),
    pending_binding: status("待绑定会话", "warn"), configured: status("已配置", "soft"),
    unknown: status("状态待核实", "warn"), validated: status("已验证", "good"),
  };
  const sessionStates = {
    unknown: status("待核实", "warn"), idle: status("空闲", "good"), busy: status("执行中", "soft"),
    needs_user: status("需要你处理", "warn"), offline: status("离线"), waiting: status("待接手"),
    accepted: status("已接手", "soft"), running: status("执行中", "soft"),
    succeeded: status("成功", "good"), failed: status("失败", "bad"), interrupted: status("已停止"),
    pending: status("排队中"), dispatching: status("正在投递", "soft"),
    delivered: status("已投递", "good"), uncertain: status("投递不确定", "warn"),
    network_pending: status("等待网络投递"), remote_received: status("目的 Hub 已保存", "soft"),
    connected: status("直连在线", "good"), relayed: status("经中继可达", "soft"),
    paired: status("已配对"), discovered: status("已发现"), disconnected: status("已断开"),
    connecting: status("连接中", "soft"), expired: status("已过期", "warn"),
  };
  const installationStatus = value => installationStates[value] || installationStates.unknown;
  const sessionStatus = value => sessionStates[value] || sessionStates.unknown;
  const agentStates = {
    connected: status("Client 已连接", "soft"), verified: status("原生唤醒已验证", "good"),
    disabled: status("未启用"),
  };
  const agentStatus = value => agentStates[value] || sessionStatus(value);
  const list = value => Array.isArray(value) ? value : [];
  const string = value => value == null ? "" : String(value);
  const values = value => value && typeof value === "object" ? Object.values(value) : [];
  const retryDelay = attempt => Math.min(1000 * 2 ** Math.min(Math.max(attempt, 0), 5), 30000);
  const languages = [
    ["en", "英语 · English"], ["zh", "中文"], ["zh-Hans", "简体中文"], ["zh-Hant", "繁體中文"],
    ["ja", "日语 · 日本語"], ["ko", "韩语 · 한국어"], ["fr", "法语 · Français"],
    ["de", "德语 · Deutsch"], ["es", "西班牙语 · Español"], ["pt", "葡萄牙语 · Português"],
    ["it", "意大利语 · Italiano"], ["nl", "荷兰语 · Nederlands"], ["ru", "俄语 · Русский"],
    ["uk", "乌克兰语 · Українська"], ["pl", "波兰语 · Polski"], ["cs", "捷克语 · Čeština"],
    ["sk", "斯洛伐克语 · Slovenčina"], ["ro", "罗马尼亚语 · Română"], ["hu", "匈牙利语 · Magyar"],
    ["bg", "保加利亚语 · Български"], ["el", "希腊语 · Ελληνικά"], ["tr", "土耳其语 · Türkçe"],
    ["sv", "瑞典语 · Svenska"], ["da", "丹麦语 · Dansk"], ["fi", "芬兰语 · Suomi"],
    ["nb", "挪威书面语 · Bokmål"], ["nn", "新挪威语 · Nynorsk"], ["is", "冰岛语 · Íslenska"],
    ["ar", "阿拉伯语 · العربية"], ["he", "希伯来语 · עברית"], ["fa", "波斯语 · فارسی"],
    ["hi", "印地语 · हिन्दी"], ["bn", "孟加拉语 · বাংলা"], ["ur", "乌尔都语 · اردو"],
    ["ta", "泰米尔语 · தமிழ்"], ["th", "泰语 · ไทย"], ["vi", "越南语 · Tiếng Việt"],
    ["id", "印度尼西亚语 · Bahasa Indonesia"], ["ms", "马来语 · Bahasa Melayu"],
    ["fil", "菲律宾语 · Filipino"], ["sw", "斯瓦希里语 · Kiswahili"],
  ];
  const languageName = value => value === "auto" || !value ? "自由协作" :
    (languages.find(([code]) => code === value)?.[1] || string(value));
  const languageTag = value => value.length <= 35 && /^[a-z]{2,3}(?:-[a-z]{4})?(?:-(?:[a-z]{2}|[0-9]{3}))?$/i.test(value);
  const defaultLanguage = agent => /claude/i.test(agent.driver || agent.id) ? "en" : "auto";

  function bootstrapToken(hash) {
    try { return decodeURIComponent(string(hash).replace(/^#/, "")); } catch (_) { return ""; }
  }

  function networkCounts(network = {}) {
    const peers = list(network.peers);
    return {
      paired: peers.length,
      connected: peers.filter(peer => peer.state === "connected").length,
      relayed: peers.filter(peer => peer.state === "relayed").length,
      discovered: list(network.discovered).length,
      pending: list(network.pending).length,
    };
  }

  function recentMessages(state, limit = 30) {
    return values(state.messages).filter(message => message.mailbox)
      .sort((a, b) => string(b.message_id).localeCompare(string(a.message_id)))
      .slice(0, limit);
  }

  function createAPI(fetcher, getToken) {
    return async function api(path, body, signal) {
      const response = await fetcher(path, {
        method: body === undefined ? "GET" : "POST", cache: "no-store", signal,
        headers: { Authorization: "Bearer " + getToken(), "Content-Type": "application/json" },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      if (!response.ok) {
        let detail = await response.text();
        try { detail = JSON.parse(detail).error || detail; } catch (_) { /* Plain HTTP errors are valid. */ }
        const error = new Error(response.status === 401 ? "管理密钥无效或已失效，请重新连接。" : (detail || "请求失败，请重试。"));
        error.status = response.status;
        throw error;
      }
      const result = await response.json();
      if (result?.ok === false) throw new Error(result.detail || result.error || "操作未完成。");
      return result;
    };
  }

  function createEventDecoder(onEvent) {
    let pending = "";
    return { push(chunk) {
      pending += chunk;
      // A CRLF can span chunks; normalize only complete frames.
      let match;
      while ((match = /\r?\n\r?\n/.exec(pending))) {
        const frame = pending.slice(0, match.index);
        pending = pending.slice(match.index + match[0].length);
        const line = frame.split(/\r?\n/).find(item => item.startsWith("event:"));
        if (line) onEvent(line.slice(6).trim());
      }
      if (pending.length > 65536) throw new Error("事件帧过大，正在重新连接。");
    } };
  }

  function createRefreshQueue(load, render) {
    let pending, requested = false;
    return function refresh() {
      requested = true;
      if (!pending) {
        pending = (async () => {
          while (requested) {
            requested = false;
            render(await load());
          }
        })().finally(() => { pending = undefined; });
      }
      return pending;
    };
  }

  function element(document, tag, className, text) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = string(text);
    return node;
  }

  function badge(document, value) {
    return element(document, "span", "badge " + value.tone, value.label);
  }

  function setBadge(node, value) {
    node.className = "badge " + value.tone;
    node.textContent = value.label;
  }

  function button(document, label, callback, className = "secondary") {
    const node = element(document, "button", className, label);
    node.type = "button";
    node.addEventListener("click", callback);
    return node;
  }

  function createAgentCard(document, initialAgent, action) {
    let agent = initialAgent;
    let languageDirty = false;
    const refs = {};
    const node = element(document, "article", "agent-card");
    const heading = element(document, "div", "agent-heading");
    refs.icon = element(document, "span", "agent-icon");
    refs.icon.setAttribute("aria-hidden", "true");
    const title = element(document, "div", "agent-name");
    refs.name = element(document, "h3");
    refs.identity = element(document, "p", "mono");
    title.append(refs.name, refs.identity);
    heading.append(refs.icon, title);
    const states = element(document, "div", "agent-states");
    refs.installation = badge(document, installationStatus(agent.installation_state));
    refs.state = badge(document, agentStatus(agent.state));
    states.append(refs.installation, refs.state);
    refs.detail = element(document, "p", "agent-detail");
    const meta = element(document, "div", "agent-meta");
    refs.enabled = element(document, "span");
    refs.shared = element(document, "span");
    meta.append(refs.enabled, refs.shared);
    const actions = element(document, "div", "agent-actions");
    for (const [key, label] of [["install", "安装协作技能"], ["enable", "启用"], ["disable", "停用"], ["share", "共享到网络"], ["unshare", "停止共享"]]) {
      refs[key] = button(document, label, () => action({ endpoint: agent.id, action: key }));
      actions.append(refs[key]);
    }
    refs.binding = element(document, "details", "binding");
    refs.binding.append(element(document, "summary", "", "绑定已有会话"));
    refs.binding.append(element(document, "p", "", "从原生应用确认现有会话 ID 后填写。此操作保存绑定并启用接收，不会创建会话或发送模型测试。"));
    refs.bindingForm = element(document, "form");
    const label = element(document, "label", "", "原生会话 ID");
    refs.session = element(document, "input");
    refs.session.type = "text";
    refs.session.autocomplete = "off";
    refs.session.spellcheck = false;
    refs.session.required = true;
    refs.session.placeholder = "填写已确认的真实会话 ID";
    label.append(refs.session);
    refs.bind = element(document, "button", "secondary", "绑定并启用");
    refs.bind.type = "submit";
    refs.validation = element(document, "p", "validation-error");
    refs.validation.setAttribute("role", "alert");
    refs.bindingForm.addEventListener("submit", event => {
      event.preventDefault();
      const session = refs.session.value.trim();
      if (!session) { refs.validation.textContent = "请填写已确认的原生会话 ID。"; return; }
      refs.validation.textContent = "";
      return action({ endpoint: agent.id, action: "enable", session_id: session });
    });
    refs.bindingForm.append(label, refs.bind, refs.validation);
    refs.binding.append(refs.bindingForm);
    refs.languageForm = element(document, "form", "language-form");
    const languageLabel = element(document, "label", "", "协作语言");
    refs.languageSelect = element(document, "select");
    refs.languageDefault = element(document, "option");
    refs.languageDefault.value = "auto";
    refs.languageSelect.append(refs.languageDefault);
    for (const [code, name] of [...languages, ["custom", "其他语言标签…"]]) {
      const option = element(document, "option", "", name);
      option.value = code;
      refs.languageSelect.append(option);
    }
    languageLabel.append(refs.languageSelect);
    refs.languageCustomLabel = element(document, "label", "", "自定义语言标签");
    refs.languageCustom = element(document, "input");
    refs.languageCustom.type = "text";
    refs.languageCustom.maxLength = 35;
    refs.languageCustom.autocomplete = "off";
    refs.languageCustom.spellcheck = false;
    refs.languageCustom.placeholder = "例如 sr-Latn-RS 或 es-419";
    refs.languageCustomLabel.append(refs.languageCustom);
    refs.languageSave = element(document, "button", "secondary", "保存语言");
    refs.languageSave.type = "submit";
    refs.languageHint = element(document, "p", "hint");
    refs.languageValidation = element(document, "p", "validation-error");
    refs.languageValidation.setAttribute("role", "alert");
    const languageScope = element(document, "p", "hint",
      "仅约束助手间的提示与回复；与人交流仍按用户指定的语言。代码、附件及引用原文保留。");
    refs.languageForm.append(languageLabel, refs.languageCustomLabel, refs.languageSave,
      refs.languageHint, refs.languageValidation, languageScope);
    function languageInput() {
      refs.languageCustomLabel.hidden = refs.languageSelect.value !== "custom";
      refs.languageHint.textContent = languageDirty ? "有尚未保存的语言设置。" : "保存后更新已登记的受管协作技能；运行中的会话须重新读取，不会自动调用模型。";
    }
    refs.languageSelect.addEventListener("change", () => {
      languageDirty = true;
      refs.languageValidation.textContent = "";
      languageInput();
    });
    refs.languageCustom.addEventListener("input", () => {
      languageDirty = true;
      refs.languageValidation.textContent = "";
      languageInput();
    });
    refs.languageForm.addEventListener("submit", event => {
      event.preventDefault();
      const language = refs.languageSelect.value === "custom" ? refs.languageCustom.value.trim() : refs.languageSelect.value;
      if (language !== "auto" && !languageTag(language)) {
        refs.languageValidation.textContent = "请输入语言标签，例如 en、zh、sr-Latn-RS 或 es-419。";
        return;
      }
      refs.languageValidation.textContent = "";
      return action({ endpoint: agent.id, action: "language", communication_language: language });
    });
    node.append(heading, states, refs.detail, meta, actions, refs.languageForm, refs.binding);
    function update(next) {
      agent = next;
      refs.name.textContent = agent.name || agent.id;
      refs.icon.textContent = string(agent.name || agent.id).slice(0, 1).toUpperCase();
      refs.identity.textContent = agent.id + " · " + (agent.driver || "未配置驱动");
      refs.detail.textContent = agent.detail || "等待本机状态信息。";
      refs.enabled.textContent = agent.enabled ? "接收已启用" : "接收未启用";
      refs.shared.textContent = agent.shared ? "已共享" : "仅本机";
      setBadge(refs.installation, installationStatus(agent.installation_state));
      setBadge(refs.state, agentStatus(agent.state));
      refs.install.textContent = /grok/i.test(agent.driver || agent.id) ? "查看配置步骤" : "安装协作技能";
      for (const key of ["install", "enable", "disable", "share", "unshare"]) {
        refs[key].hidden = !list(agent.actions).includes(key) || (key === "enable" && !agent.session_bound);
      }
      refs.binding.hidden = agent.session_bound || !list(agent.actions).includes("enable");
      refs.languageForm.hidden = !list(agent.actions).includes("language");
      refs.languageDefault.textContent = defaultLanguage(agent) === "en" ? "默认（英语）" : "自由协作（auto）";
      if (!languageDirty) {
        const language = agent.communication_language || defaultLanguage(agent);
        const known = language === "auto" || languages.some(([code]) => code === language);
        refs.languageSelect.value = known ? language : "custom";
        refs.languageCustom.value = known ? "" : language;
      }
      languageInput();
    }
    update(agent);
    return { node, refs, update, languageSaved() { languageDirty = false; languageInput(); } };
  }

  function createPeerCard(document, peer, action) {
    const node = element(document, "details", "peer-card");
    node.dataset.peer = peer.id;
    const summary = element(document, "summary");
    const title = element(document, "span", "peer-title");
    title.append(element(document, "strong", "", peer.name || peer.id), element(document, "span", "mono", peer.id));
    summary.append(title, badge(document, sessionStatus(peer.state)), element(document, "span", "count", list(peer.agents).length + " 个助手"));
    const body = element(document, "div", "peer-body");
    if (list(peer.route).length) body.append(element(document, "p", "route", "路径：" + peer.route.join(" → ")));
    for (const agent of list(peer.agents)) {
      const row = element(document, "div", "remote-agent");
      const label = element(document, "div");
      label.append(element(document, "strong", "", agent.name || agent.id),
        element(document, "p", "mono", peer.id + "/" + agent.id),
        element(document, "p", "remote-language", "协作语言：" + languageName(agent.communication_language)));
      row.append(label, badge(document, sessionStatus(agent.state)));
      body.append(row);
    }
    if (!list(peer.agents).length) body.append(element(document, "p", "hint", "此主机尚未公布共享助手，或目录仍待同步。"));
    const footer = element(document, "div", "row");
    footer.append(element(document, "span", "hint mono", peer.address || "地址尚未同步"),
      button(document, "断开并取消配对", () => action({ action: "disconnect", peer_id: peer.id }), "quiet danger"));
    body.append(footer);
    node.append(summary, body);
    return node;
  }

  function compactPeer(document, peer) {
    const row = element(document, "div", "compact-item");
    const title = element(document, "div");
    title.append(element(document, "strong", "", peer.name || peer.hub_id || peer.id),
      element(document, "p", "mono", (peer.hub_id || peer.id) + (peer.address ? " · " + peer.address : "")));
    row.append(title);
    return row;
  }

  function createPendingCard(document, peer, action) {
    const row = compactPeer(document, peer);
    if (peer.direction === "incoming") {
      row.append(button(document, "确认配对", () => action({ action: "accept", peer_id: peer.id })));
    } else {
      row.append(badge(document, status(peer.direction === "outgoing" ? "等待对方确认" : "配对方向待核实", "warn")));
    }
    return row;
  }

  function renderActionNotice(document, node, result, fallback) {
    const sync = list(result.skill_sync);
    const incomplete = result.skill_sync_complete === false || !!result.skill_sync_error ||
      sync.some(item => !["current", "updated"].includes(item.state));
    node.className = "notice " + (incomplete ? "warning" : "success");
    node.replaceChildren(element(document, "span", "", result.detail || fallback));
    const steps = list(result.steps);
    if (steps.length) {
      const stepsNode = element(document, "ol");
      for (const step of steps) stepsNode.append(element(document, "li", "", step));
      node.append(stepsNode);
    }
    if (sync.length || result.skill_sync_error) {
      const details = element(document, "details", "skill-sync");
      details.open = incomplete;
      details.append(element(document, "summary", "", "技能同步结果" + (incomplete ? " · 部分未同步" : "")));
      if (result.skill_sync_error) details.append(element(document, "p", "sync-error", result.skill_sync_error));
      const entries = element(document, "ul");
      for (const item of sync) {
        const entry = element(document, "li");
        const heading = element(document, "div", "row");
        heading.append(element(document, "strong", "", string(item.endpoint_id) + " · " + string(item.host)),
          badge(document, status(item.state || "unknown", ["current", "updated"].includes(item.state) ? "good" : "warn")));
        entry.append(heading, element(document, "p", "mono", item.skill_path || "路径未提供"));
        if (item.detail) entry.append(element(document, "p", "", item.detail));
        entry.append(element(document, "p", "hint", "会话读取状态：" + (item.host_load_state || "unknown")));
        entries.append(entry);
      }
      if (sync.length) details.append(entries);
      node.append(details);
    }
    node.hidden = false;
  }

  function start(window) {
    const document = window.document;
    const el = id => document.getElementById(id);
    let token = bootstrapToken(window.location.hash);
    window.history.replaceState(null, "", window.location.pathname);
    el("token").value = token;
    const api = createAPI(window.fetch.bind(window), () => token);
    const cards = new Map();
    let controller, generation = 0, reconnectTimer, streamAttempt = 0;
    let settingsDirty = false, authenticated = false;
    let actionInFlight = false;
    const errorBox = el("error");
    const noticeBox = el("notice");

    function error(failure) {
      if (failure.name === "AbortError") return;
      errorBox.textContent = failure.message || string(failure);
      errorBox.hidden = false;
      if (failure.status === 401) lock(false);
    }
    function clearError() { errorBox.hidden = true; errorBox.textContent = ""; }
    function notice(result, fallback) {
      renderActionNotice(document, noticeBox, result, fallback);
    }
    function connection(label, tone = "neutral") { setBadge(el("connection"), status(label, tone)); }

    function renderSettings(manage) {
      const hub = manage.hub || {};
      el("hub-name").textContent = hub.name || "本机 Hub";
      el("hub-id").textContent = hub.id || "稳定身份待生成";
      const counts = networkCounts(manage.network);
      for (const name of ["paired", "connected", "relayed", "discovered", "pending"]) el("count-" + name).textContent = counts[name];
      el("count-local").textContent = list(manage.agents).length;
      setBadge(el("network-status"), hub.network_enabled ? status("已开启", "good") : status("关闭"));
      setBadge(el("discovery-status"), hub.discovery_enabled ? status("已开启", "good") : status("关闭"));
      el("network-off").hidden = !!hub.network_enabled;
      const networkErrors = [hub.network_error ? "局域网连接：" + hub.network_error : "", hub.discovery_error ? "自动发现：" + hub.discovery_error : ""].filter(Boolean);
      el("discovery-error").hidden = !networkErrors.length;
      el("discovery-error").textContent = networkErrors.join("；");
      if (!settingsDirty) {
        el("setting-name").value = hub.name || "";
        el("network-enabled").checked = !!hub.network_enabled;
        el("discovery-enabled").checked = !!hub.discovery_enabled;
        el("setting-listen").value = hub.listen || "";
        const select = el("setting-interface");
        select.replaceChildren(element(document, "option", "", "选择网卡"));
        select.firstElementChild.value = "";
        const interfaces = [...list(manage.network?.interfaces)];
        if (hub.interface && !interfaces.some(item => item.name === hub.interface)) interfaces.push({ name: hub.interface, addresses: ["当前不可用"] });
        for (const item of interfaces) {
          const option = element(document, "option", "", item.name + (list(item.addresses).length ? " · " + item.addresses.join(", ") : ""));
          option.value = item.name;
          select.append(option);
        }
        select.value = hub.interface || "";
      }
      el("settings-hint").textContent = settingsDirty ? "有尚未保存的设置。" : "局域网连接与发现默认关闭。关闭发现会保留已有配对。";
    }

    function renderAgents(agents) {
      const container = el("agents");
      const seen = new Set();
      for (const agent of list(agents)) {
        seen.add(agent.id);
        if (!cards.has(agent.id)) cards.set(agent.id, createAgentCard(document, agent, body => mutate("/v1/manage/agent", body)));
        else cards.get(agent.id).update(agent);
      }
      for (const [id, card] of cards) if (!seen.has(id)) { card.node.remove(); cards.delete(id); }
      // Reuse cards and native form nodes so SSE cannot reset in-progress input.
      if (list(agents).length) {
        container.querySelectorAll(".empty").forEach(node => node.remove());
        for (const agent of agents) {
          const node = cards.get(agent.id).node;
          if (node.parentNode !== container) container.append(node);
        }
      } else container.replaceChildren(element(document, "p", "empty", "尚无本机助手。请先在 Hub 配置中添加受支持的接入。"));
    }

    function renderNetwork(network = {}) {
      const peers = el("peers");
      const open = new Set(Array.from(peers.querySelectorAll("details[open]"), node => node.dataset.peer));
      peers.replaceChildren();
      for (const peer of list(network.peers)) {
        const node = createPeerCard(document, peer, body => mutate("/v1/manage/network", body));
        node.open = open.has(peer.id);
        peers.append(node);
      }
      if (!list(network.peers).length) peers.append(element(document, "p", "empty", "还没有已配对主机。发送邀请，或使用另一台电脑的邀请开始连接。"));
      el("pending-group").hidden = !list(network.pending).length;
      el("pending").replaceChildren();
      for (const peer of list(network.pending)) {
        el("pending").append(createPendingCard(document, peer, body => mutate("/v1/manage/network", body)));
      }
      el("discovered").replaceChildren();
      el("discovered-label").textContent = list(network.discovered).length;
      for (const peer of list(network.discovered)) {
        const row = compactPeer(document, peer);
        row.append(badge(document, sessionStatus(peer.state || "discovered")), button(document, "填写配对", () => {
          el("pair-address").value = peer.address || "";
          el("pair-invitation").focus();
          el("pair-form").scrollIntoView({ block: "center", behavior: "smooth" });
        }, "quiet"));
        el("discovered").append(row);
      }
      if (!list(network.discovered).length) el("discovered").append(element(document, "p", "empty", "暂未发现未配对主机。可开启自动发现，也可直接填写对方地址。"));
    }

    function cell(row, value, className) {
      const node = element(document, "td", className, value);
      row.append(node);
      return node;
    }

    function renderMessages(state) {
      const messages = recentMessages(state);
      el("mail-empty").hidden = !!messages.length;
      const previousOpen = new Set(Array.from(el("mails").querySelectorAll("details[open]"), node => node.dataset.message));
      el("mails").replaceChildren();
      const kinds = { request: "请求", reply: "回复", status: "状态", result: "成果" };
      for (const message of messages) {
        const row = element(document, "tr");
        const identity = cell(row, message.message_id, "mono");
        if (message.created_at) identity.append(element(document, "span", "hint", formatDate(message.created_at)));
        cell(row, string(message.sender) + " → " + string(message.recipient));
        cell(row, kinds[message.kind] || message.kind);
        cell(row, "").append(badge(document, sessionStatus(message.delivery)));
        const content = cell(row, "");
        const detail = element(document, "details");
        detail.dataset.message = message.message_id;
        detail.open = previousOpen.has(message.message_id);
        detail.append(element(document, "summary", "", string(message.content).slice(0, 90) || "查看详情"),
          element(document, "pre", "", "关联：" + string(message.correlation_id) + "\n回复引用：" + (message.reply_to || "—") + "\n\n" + string(message.content)));
        content.append(detail);
        el("mails").append(row);
      }
      const tasks = values(state.tasks).reverse();
      el("count-tasks").textContent = tasks.length;
      el("tasks").replaceChildren();
      for (const task of tasks) {
        const row = element(document, "tr");
        cell(row, string(task.goal) + " · " + string(task.id));
        cell(row, task.recipient);
        cell(row, "").append(badge(document, sessionStatus(task.execution)));
        cell(row, list(task.artifacts).map(item => item.device_id + " / " + item.workspace_id + " / " + item.path).join("\n") || task.summary || "—");
        el("tasks").append(row);
      }
    }

    function formatDate(value) {
      const date = new Date(value);
      return Number.isNaN(date.getTime()) ? string(value) : date.toLocaleString("zh-CN", { hour12: false });
    }

    const refresh = createRefreshQueue(async () => {
      const expected = generation;
      const signal = controller?.signal;
      const [manage, state] = await Promise.all([api("/v1/manage", undefined, signal), api("/v1/state", undefined, signal)]);
      return { expected, manage, state };
    }, ({ expected, manage, state }) => {
      if (expected !== generation) return;
      renderSettings(manage);
      renderAgents(manage.agents);
      renderNetwork(manage.network);
      renderMessages(state);
      el("updated").textContent = "已同步 " + new Date().toLocaleTimeString("zh-CN", { hour12: false });
    });

    async function mutate(path, body) {
      if (actionInFlight || !authenticated) return;
      actionInFlight = true;
      clearError();
      noticeBox.hidden = true;
      el("workspace").setAttribute("aria-busy", "true");
      const controls = Array.from(el("workspace").querySelectorAll("button, input, textarea, select"));
      const disabled = controls.map(control => control.disabled);
      controls.forEach(control => { control.disabled = true; });
      try {
        const result = await api(path, body, controller?.signal);
        if (body.action === "invite" && result.invitation) {
          el("invitation-output").value = result.invitation;
          el("invitation-result").hidden = false;
        }
        if (body.action === "pair") el("pair-invitation").value = "";
        if (path === "/v1/manage/settings") settingsDirty = false;
        if (path === "/v1/manage/agent" && body.action === "language") cards.get(body.endpoint)?.languageSaved();
        notice(result, "操作已提交，当前状态见下方列表。");
        await refresh();
      } catch (failure) { error(failure); }
      finally {
        actionInFlight = false;
        el("workspace").setAttribute("aria-busy", "false");
        controls.forEach((control, index) => { control.disabled = disabled[index]; });
      }
    }

    function scheduleStream(expected) {
      if (!authenticated || expected !== generation) return;
      const delay = retryDelay(streamAttempt++);
      connection("重连中 · " + delay / 1000 + " 秒", "warn");
      reconnectTimer = window.setTimeout(() => stream(expected), delay);
    }

    async function stream(expected) {
      if (!authenticated || expected !== generation) return;
      try {
        const response = await window.fetch("/v1/events", {
          headers: { Authorization: "Bearer " + token }, cache: "no-store", signal: controller.signal,
        });
        if (!response.ok) {
          const failure = new Error(response.status === 401 ? "管理密钥无效或已失效，请重新连接。" : "事件连接暂不可用。");
          failure.status = response.status;
          throw failure;
        }
        if (!response.body) throw new Error("浏览器不支持事件流，请使用刷新按钮更新。");
        connection("实时连接", "good");
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        const events = createEventDecoder(type => {
          if (type === "change") refresh().catch(error);
        });
        const started = Date.now();
        try {
          while (expected === generation) {
            const part = await reader.read();
            if (part.done) break;
            // A stable connection resets backoff, not a repeated initial frame.
            if (Date.now() - started > 20000) streamAttempt = 0;
            events.push(decoder.decode(part.value, { stream: true }));
          }
        } finally { reader.releaseLock(); }
        scheduleStream(expected);
      } catch (failure) {
        if (failure.name === "AbortError" || expected !== generation) return;
        if (failure.status === 401) { error(failure); return; }
        scheduleStream(expected);
      }
    }

    function lock(clear = true) {
      generation++;
      authenticated = false;
      controller?.abort();
      window.clearTimeout(reconnectTimer);
      controller = undefined;
      settingsDirty = false;
      token = "";
      el("token").value = "";
      el("invitation-output").value = "";
      el("pair-invitation").value = "";
      el("invitation-result").hidden = true;
      el("workspace").hidden = true;
      el("login").hidden = false;
      el("refresh").hidden = true;
      el("lock").hidden = true;
      connection("尚未连接");
      if (clear) { clearError(); noticeBox.hidden = true; }
    }

    async function connect(event) {
      event?.preventDefault();
      if (!el("token").value.trim()) return;
      controller?.abort();
      window.clearTimeout(reconnectTimer);
      generation++;
      const expected = generation;
      controller = new AbortController();
      token = el("token").value.trim();
      el("connect").disabled = true;
      el("connect").textContent = "正在连接…";
      connection("正在连接", "soft");
      clearError();
      try {
        await refresh();
        if (expected !== generation) return;
        authenticated = true;
        el("token").value = "";
        el("login").hidden = true;
        el("workspace").hidden = false;
        el("refresh").hidden = false;
        el("lock").hidden = false;
        streamAttempt = 0;
        stream(generation);
      } catch (failure) {
        if (failure.name !== "AbortError") { connection("连接失败", "bad"); error(failure); }
      } finally {
        el("connect").disabled = false;
        el("connect").textContent = "连接 Hub";
      }
    }

    el("login-form").addEventListener("submit", connect);
    el("lock").addEventListener("click", () => lock());
    el("refresh").addEventListener("click", async () => {
      el("refresh").disabled = true;
      clearError();
      try { await refresh(); } catch (failure) { error(failure); }
      finally { el("refresh").disabled = false; }
    });
    el("settings-form").addEventListener("input", () => {
      settingsDirty = true;
      el("settings-hint").textContent = "有尚未保存的设置。";
    });
    el("settings-form").addEventListener("submit", event => {
      event.preventDefault();
      const body = {
        name: el("setting-name").value.trim(),
        network_enabled: el("network-enabled").checked,
        discovery_enabled: el("discovery-enabled").checked,
        listen: el("setting-listen").value.trim(), interface: el("setting-interface").value,
      };
      if (body.discovery_enabled && !body.network_enabled) { error(new Error("请先开启局域网连接，再启用自动发现。")); return; }
      if (body.discovery_enabled && !body.interface) { el("settings-details").open = true; error(new Error("请先选择自动发现使用的网卡。")); return; }
      mutate("/v1/manage/settings", body);
    });
    el("invite").addEventListener("click", () => mutate("/v1/manage/network", { action: "invite" }));
    el("pair-form").addEventListener("submit", event => {
      event.preventDefault();
      const invitation = el("pair-invitation").value.trim();
      if (!invitation) return;
      const body = { action: "pair", invitation };
      const address = el("pair-address").value.trim();
      if (address) body.address = address;
      mutate("/v1/manage/network", body);
    });
    el("copy-invitation").addEventListener("click", async () => {
      try {
        if (!window.navigator.clipboard) throw new Error("请在邀请框中手动选择并复制。");
        await window.navigator.clipboard.writeText(el("invitation-output").value);
        el("copy-invitation").textContent = "已复制";
        window.setTimeout(() => { el("copy-invitation").textContent = "复制"; }, 1800);
      } catch (_) {
        el("invitation-output").focus();
        el("invitation-output").select();
        error(new Error("自动复制不可用，已选中邀请，请手动复制。"));
      }
    });
    el("clear-invitation").addEventListener("click", () => {
      el("invitation-output").value = "";
      el("invitation-result").hidden = true;
    });
    window.addEventListener("pagehide", () => { controller?.abort(); window.clearTimeout(reconnectTimer); });
    if (token) connect();
  }

  return { start, installationStatus, sessionStatus, agentStatus, networkCounts, recentMessages, createAPI,
    createEventDecoder, createRefreshQueue, createAgentCard, createPeerCard, createPendingCard, renderActionNotice, retryDelay, bootstrapToken };
});
