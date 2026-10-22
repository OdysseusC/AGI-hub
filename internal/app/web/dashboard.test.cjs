const test = require("node:test");
const assert = require("node:assert/strict");

let ui;
try { ui = require("./dashboard.js"); } catch (error) {
  if (error.code !== "MODULE_NOT_FOUND") throw error;
  ui = {};
}

// A small DOM surface keeps these behavior checks offline and dependency-free.
class Element {
  constructor(tag) {
    this.tagName = tag.toUpperCase();
    this.children = [];
    this.attributes = {};
    this.dataset = {};
    this.className = "";
    this.listeners = {};
    this.textContent = "";
    this.value = "";
    this.disabled = false;
    this.hidden = false;
    this.open = false;
  }
  append(...nodes) { this.children.push(...nodes); }
  replaceChildren(...nodes) { this.children = nodes; }
  setAttribute(key, value) { this.attributes[key] = String(value); }
  addEventListener(type, listener) { this.listeners[type] = listener; }
  async trigger(type) { return this.listeners[type]?.({ preventDefault() {}, target: this }); }
}
const document = { createElement: tag => new Element(tag) };
const descendants = node => [node, ...node.children.flatMap(descendants)];
const text = node => descendants(node).map(item => item.textContent).join(" ");
const fixtureAgent = changes => ({
  id: "codex", name: "Codex", driver: "codex-desktop", enabled: false,
  shared: false, session_bound: false, installation_state: "installed",
  state: "unknown", detail: "组件已安装，等待绑定。",
  actions: ["install", "enable", "share", "language"], ...changes,
});

test("installation and runtime states never imply native validation", () => {
  assert.equal(typeof ui.installationStatus, "function");
  assert.equal(ui.installationStatus("installed").label, "文件已安装");
  assert.equal(ui.installationStatus("pending_binding").label, "待绑定会话");
  assert.equal(ui.installationStatus("configured").label, "已配置");
  assert.equal(ui.installationStatus("validated").label, "已验证");
  assert.equal(ui.installationStatus("new-state").label, "状态待核实");
  assert.equal(ui.sessionStatus("unknown").label, "待核实");
  assert.equal(ui.agentStatus("bound").label, "已绑定 · 连接待核实");
  assert.notEqual(ui.agentStatus("bound").label, ui.agentStatus("connected").label);
});

test("network counts distinguish paired, directly connected, relayed and discovered", () => {
  assert.equal(typeof ui.networkCounts, "function");
  assert.deepEqual(ui.networkCounts({
    peers: [{ state: "connected" }, { state: "offline" }, { state: "relayed", route: ["relay"] }],
    discovered: [{ id: "untrusted" }], pending: [{ id: "pending" }],
  }), { paired: 3, connected: 1, relayed: 1, discovered: 1, pending: 1 });
});

test("agent card treats untrusted names as text and requires explicit session before enable", async () => {
  assert.equal(typeof ui.createAgentCard, "function");
  const requests = [];
  const card = ui.createAgentCard(document, fixtureAgent({ name: "<img src=x onerror=alert(1)>" }),
    request => requests.push(request));
  assert.ok(text(card.node).includes("<img src=x onerror=alert(1)>"));
  assert.equal(descendants(card.node).some(node => node.tagName === "IMG"), false);
  assert.equal(card.refs.enable.hidden, true);
  assert.equal(card.refs.binding.hidden, false);
  card.refs.session.value = "  ";
  await card.refs.bindingForm.trigger("submit");
  assert.equal(requests.length, 0);
  card.refs.session.value = " native-existing-session ";
  await card.refs.bindingForm.trigger("submit");
  assert.deepEqual(requests, [{ endpoint: "codex", action: "enable", session_id: "native-existing-session" }]);
});

test("live refresh updates agent state without discarding unfinished binding input", () => {
  assert.equal(typeof ui.createAgentCard, "function");
  const card = ui.createAgentCard(document, fixtureAgent(), () => {});
  card.refs.session.value = "unfinished-session";
  card.refs.binding.open = true;
  card.update(fixtureAgent({ detail: "新状态", state: "busy" }));
  assert.equal(card.refs.session.value, "unfinished-session");
  assert.equal(card.refs.binding.open, true);
  assert.ok(text(card.node).includes("执行中"));
  assert.ok(text(card.node).includes("新状态"));
});

test("server action availability controls buttons and shared state is explicit", async () => {
  assert.equal(typeof ui.createAgentCard, "function");
  const requests = [];
  const card = ui.createAgentCard(document,
    fixtureAgent({ session_bound: true, enabled: true, shared: true, actions: ["disable", "unshare"] }),
    request => requests.push(request));
  assert.equal(card.refs.install.hidden, true);
  assert.equal(card.refs.enable.hidden, true);
  assert.equal(card.refs.disable.hidden, false);
  assert.equal(card.refs.unshare.hidden, false);
  await card.refs.disable.trigger("click");
  assert.deepEqual(requests, [{ endpoint: "codex", action: "disable" }]);
  assert.ok(text(card.node).includes("已共享"));
});

test("network tree shows a qualified endpoint address and relay path without trusting markup", () => {
  assert.equal(typeof ui.createPeerCard, "function");
  const card = ui.createPeerCard(document, {
    id: "hub-2", name: "<script>bad()</script>", state: "relayed", address: "192.0.2.2:7443",
    route: ["hub-relay", "hub-2"], agents: [{ id: "codex", name: "Codex", state: "idle" }],
  }, () => {});
  assert.ok(text(card).includes("hub-2/codex"));
  assert.ok(text(card).includes("hub-relay → hub-2"));
  assert.equal(descendants(card).some(node => node.tagName === "SCRIPT"), false);
});

test("mailbox uses deterministic IDs when creation timestamps are unavailable", () => {
  assert.equal(typeof ui.recentMessages, "function");
  const messages = {
    old: { message_id: "a-old", mailbox: true },
    task: { message_id: "task", mailbox: false },
    newer: { message_id: "z-new", mailbox: true },
  };
  assert.deepEqual(ui.recentMessages({ messages }, 1).map(message => message.message_id), ["z-new"]);
  assert.equal(Object.keys(messages).length, 3);
});

test("client connection and remote Hub receipt do not imply native delivery", () => {
  assert.equal(typeof ui.agentStatus, "function");
  assert.equal(ui.agentStatus("connected").label, "Client 已连接");
  assert.equal(ui.agentStatus("verified").label, "原生唤醒已验证");
  assert.equal(ui.sessionStatus("network_pending").label, "等待网络投递");
  assert.equal(ui.sessionStatus("remote_received").label, "目的 Hub 已保存");
});

test("only an incoming pairing request offers local acceptance", () => {
  assert.equal(typeof ui.createPendingCard, "function");
  const incoming = ui.createPendingCard(document, { id: "in", hub_id: "other", direction: "incoming" }, () => {});
  const outgoing = ui.createPendingCard(document, { id: "out", hub_id: "other", direction: "outgoing" }, () => {});
  assert.ok(descendants(incoming).some(node => node.tagName === "BUTTON"));
  assert.equal(descendants(outgoing).some(node => node.tagName === "BUTTON"), false);
  assert.ok(text(outgoing).includes("等待对方确认"));
});

test("API sends bearer authorization only in headers and preserves meaningful failures", async () => {
  assert.equal(typeof ui.createAPI, "function");
  let captured;
  const api = ui.createAPI(async (...args) => {
    captured = args;
    return { ok: true, status: 200, json: async () => ({ ok: true }) };
  }, () => "private-token");
  await api("/v1/manage/agent", { endpoint: "codex", action: "install" });
  assert.equal(captured[0], "/v1/manage/agent");
  assert.equal(captured[1].headers.Authorization, "Bearer private-token");
  assert.deepEqual(JSON.parse(captured[1].body), { endpoint: "codex", action: "install" });
  const denied = ui.createAPI(async () => ({
    ok: false, status: 422, text: async () => '{"error":"需要先绑定原生会话"}',
  }), () => "private-token");
  await assert.rejects(denied("/v1/manage"), /需要先绑定原生会话/);
});

test("SSE decoder handles split CRLF frames and ignores keepalives", () => {
  assert.equal(typeof ui.createEventDecoder, "function");
  const frames = [];
  const decoder = ui.createEventDecoder(frame => frames.push(frame));
  decoder.push(": keepalive\r\n\r\n");
  decoder.push("event: cha");
  decoder.push("nge\r\ndata: {}\r");
  decoder.push("\n\r\n");
  assert.deepEqual(frames, ["change"]);
});

test("reconnection backoff is bounded and hash bootstrap handles malformed input", () => {
  assert.equal(typeof ui.retryDelay, "function");
  assert.equal(ui.retryDelay(0), 1000);
  assert.equal(ui.retryDelay(20), 30000);
  assert.equal(ui.bootstrapToken("#hello%20world"), "hello world");
  assert.equal(ui.bootstrapToken("#%malformed"), "");
});

test("a change arriving during a refresh triggers a fresh read before resolving", async () => {
  assert.equal(typeof ui.createRefreshQueue, "function");
  let reads = 0, release;
  const rendered = [];
  const refresh = ui.createRefreshQueue(async () => {
    const read = ++reads;
    if (read === 1) await new Promise(resolve => { release = resolve; });
    return read;
  }, value => rendered.push(value));
  const first = refresh();
  const changed = refresh();
  release();
  await Promise.all([first, changed]);
  assert.equal(reads, 2);
  assert.deepEqual(rendered, [1, 2]);
});

test("language selection includes Chinese, English and Norwegian while Claude defaults to English", async () => {
  const requests = [];
  const card = ui.createAgentCard(document,
    fixtureAgent({ id: "claude", name: "Claude", driver: "claude-code" }),
    request => requests.push(request));
  assert.ok(card.refs.languageSelect, "agent card exposes a collaboration language control");
  assert.equal(card.refs.languageSelect.value, "en");
  const options = card.refs.languageSelect.children;
  assert.ok(options.length >= 35);
  for (const value of ["en", "zh", "zh-Hant", "nb", "nn", "ru", "ar", "ja", "uk"]) {
    assert.ok(options.some(option => option.value === value), value + " is available");
  }
  assert.ok(options.find(option => option.value === "auto").textContent.includes("英语"));
  card.refs.languageSelect.value = "zh";
  await card.refs.languageSelect.trigger("change");
  await card.refs.languageForm.trigger("submit");
  assert.deepEqual(requests, [{ endpoint: "claude", action: "language", communication_language: "zh" }]);
  assert.ok(text(card.refs.languageForm).includes("尚未保存"));
});

test("custom language validates standard tags without accepting prompts as settings", async () => {
  const requests = [];
  const card = ui.createAgentCard(document, fixtureAgent(), request => requests.push(request));
  assert.ok(card.refs.languageCustom, "language control allows a custom language tag");
  assert.equal(card.refs.languageSelect.value, "auto");
  card.refs.languageSelect.value = "custom";
  await card.refs.languageSelect.trigger("change");
  assert.equal(card.refs.languageCustomLabel.hidden, false);
  for (const invalid of ["", "Please use English", "zh_CN", "<script>", "abcd", "en-US-ignore"]) {
    card.refs.languageCustom.value = invalid;
    await card.refs.languageCustom.trigger("input");
    await card.refs.languageForm.trigger("submit");
    assert.equal(requests.length, 0, "reject " + invalid);
  }
  for (const valid of ["sr-Latn-RS", "es-419", "yue-Hant", "nb"]) {
    card.refs.languageCustom.value = " " + valid + " ";
    await card.refs.languageCustom.trigger("input");
    await card.refs.languageForm.trigger("submit");
    assert.equal(requests.at(-1).communication_language, valid);
  }
});

test("refresh preserves a language draft and server values replace it only after successful save", async () => {
  const card = ui.createAgentCard(document, fixtureAgent({ communication_language: "en" }), () => {});
  assert.ok(card.refs.languageSelect, "language control exists");
  card.refs.languageSelect.value = "custom";
  await card.refs.languageSelect.trigger("change");
  card.refs.languageCustom.value = "sr-Latn-RS";
  await card.refs.languageCustom.trigger("input");
  card.update(fixtureAgent({ communication_language: "ru", state: "busy" }));
  assert.equal(card.refs.languageSelect.value, "custom");
  assert.equal(card.refs.languageCustom.value, "sr-Latn-RS");
  assert.ok(text(card.node).includes("执行中"));
  card.languageSaved();
  assert.equal(card.refs.languageHint.textContent.includes("尚未保存"), false);
  card.update(fixtureAgent({ communication_language: "sr-Latn-RS" }));
  assert.equal(card.refs.languageSelect.value, "custom");
  assert.equal(card.refs.languageCustom.value, "sr-Latn-RS");
  assert.equal(card.refs.languageHint.textContent.includes("尚未保存"), false);
  card.update(fixtureAgent({ communication_language: "en" }));
  assert.equal(card.refs.languageSelect.value, "en");
});

test("language settings stay separate from human replies and remote labels are safe text", () => {
  const card = ui.createAgentCard(document, fixtureAgent({ actions: ["disable"] }), () => {});
  assert.ok(card.refs.languageForm, "language form is available");
  assert.equal(card.refs.languageForm.hidden, true);
  const peer = ui.createPeerCard(document, {
    id: "remote", agents: [
      { id: "claude", communication_language: "en" },
      { id: "bad", communication_language: "<img src=x onerror=alert(1)>" },
    ],
  }, () => {});
  assert.ok(text(peer).includes("协作语言：英语"));
  assert.ok(text(peer).includes("<img src=x onerror=alert(1)>"));
  assert.equal(descendants(peer).some(node => node.tagName === "IMG"), false);
  assert.ok(text(card.node).includes("用户指定"));
  assert.ok(text(card.node).includes("附件"));
});

test("partial skill synchronization displays locations and errors without losing saved language status", async () => {
  assert.equal(typeof ui.renderActionNotice, "function");
  const notice = new Element("div");
  const card = ui.createAgentCard(document, fixtureAgent(), () => {});
  card.refs.languageSelect.value = "en";
  await card.refs.languageSelect.trigger("change");
  const result = {
    ok: true, detail: "语言已保存，但部分技能未能同步。", skill_sync_complete: false,
    skill_sync: [{
      endpoint_id: "claude", host: "claude-code", state: "conflict",
      skill_path: "C:\\review\\claude\\SKILL.md", host_load_state: "unknown",
      detail: "<script>conflicting manual edit</script>",
    }, {
      endpoint_id: "codex", host: "codex-desktop", state: "updated",
      skill_path: "C:\\review\\codex\\SKILL.md", host_load_state: "unknown",
    }],
    skill_sync_error: "<img src=x onerror=alert(1)> registry unavailable",
  };
  card.languageSaved();
  ui.renderActionNotice(document, notice, result, "操作完成");
  assert.ok(notice.className.includes("warning"));
  assert.equal(notice.hidden, false);
  assert.equal(card.refs.languageHint.textContent.includes("尚未保存"), false);
  const details = descendants(notice).find(node => node.tagName === "DETAILS");
  assert.ok(details);
  assert.equal(details.open, true);
  const rendered = text(notice);
  for (const value of ["语言已保存", "技能同步结果", "claude-code", "conflict",
    "C:\\review\\claude\\SKILL.md", "codex-desktop", "updated", "unknown",
    "<script>conflicting manual edit</script>", "<img src=x onerror=alert(1)> registry unavailable"]) {
    assert.ok(rendered.includes(value), value + " is visible");
  }
  assert.equal(descendants(notice).some(node => ["SCRIPT", "IMG"].includes(node.tagName)), false);
});

test("skill synchronization details are collapsed after complete success and absent for ordinary notices", () => {
  assert.equal(typeof ui.renderActionNotice, "function");
  const notice = new Element("div");
  ui.renderActionNotice(document, notice, {
    detail: "已保存", skill_sync_complete: true,
    skill_sync: [{ endpoint_id: "codex", host: "codex-desktop", state: "current", skill_path: "/review/SKILL.md" }],
  });
  assert.ok(notice.className.includes("success"));
  assert.equal(descendants(notice).find(node => node.tagName === "DETAILS").open, false);
  ui.renderActionNotice(document, notice, { detail: "已提交", steps: ["第一步"] });
  assert.equal(descendants(notice).some(node => node.tagName === "DETAILS"), false);
  assert.ok(text(notice).includes("第一步"));
  ui.renderActionNotice(document, notice, { skill_sync: [], skill_sync_complete: true }, "保存完成");
  assert.equal(descendants(notice).some(node => node.tagName === "DETAILS"), false);
  assert.ok(text(notice).includes("保存完成"));
});

test("a global synchronization error is visible even without per-installation results", () => {
  assert.equal(typeof ui.renderActionNotice, "function");
  const notice = new Element("div");
  ui.renderActionNotice(document, notice, {
    detail: "语言已保存", skill_sync: [], skill_sync_complete: false, skill_sync_error: "registry locked",
  });
  assert.ok(text(notice).includes("registry locked"));
  assert.ok(notice.className.includes("warning"));
  assert.equal(descendants(notice).find(node => node.tagName === "DETAILS").open, true);
});
