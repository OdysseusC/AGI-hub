const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

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
  append(...nodes) { for (const node of nodes) { node.remove(); node.parentNode = this; this.children.push(node); } }
  replaceChildren(...nodes) { for (const node of this.children) node.parentNode = null; this.children = []; this.append(...nodes); }
  remove() { if (this.parentNode) this.parentNode.children = this.parentNode.children.filter(node => node !== this); this.parentNode = null; }
  get firstElementChild() { return this.children[0]; }
  querySelectorAll(selector) {
    const matches = (node, part) => {
      if (part.startsWith(".")) return node.className.split(" ").includes(part.slice(1));
      const match = /^([a-z]+)(?:\[([a-z-]+)\])?$/.exec(part);
      if (!match || node.tagName !== match[1].toUpperCase()) return false;
      if (!match[2]) return true;
      if (match[2] === "open") return node.open;
      const key = match[2].replace(/^data-/, "").replace(/-([a-z])/g, (_, letter) => letter.toUpperCase());
      return Object.hasOwn(node.dataset, key);
    };
    return descendants(this).slice(1).filter(node => selector.split(",").some(part => matches(node, part.trim())));
  }
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

function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

async function dashboardFixture(agent, handlers = {}) {
  const ids = new Map();
  const html = fs.readFileSync(path.join(__dirname, "index.html"), "utf8");
  for (const match of html.matchAll(/<([a-z][a-z0-9-]*)\b[^>]*\bid="([^"]+)"[^>]*>/g)) ids.set(match[2], new Element(match[1]));
  const dom = { createElement: tag => new Element(tag), getElementById(id) { assert.ok(ids.has(id), id); return ids.get(id); } };
  // Only card controls need DOM ancestry for the production busy-state query.
  ids.get("workspace").append(ids.get("agents"));
  const calls = [];
  const window = {
    document: dom, location: { pathname: "/remote/" + "a".repeat(64) + "/", hash: "" },
    history: { replaceState() {} }, addEventListener() {}, setTimeout() { return 1; }, clearTimeout() {},
    async fetch(url, options) {
      const route = url.replace(/^\/remote\/[a-f0-9]{64}/, "");
      calls.push({ route, ...options });
      const body = options.body === undefined ? undefined : JSON.parse(options.body);
      if (options.method === "POST") return handlers.post(route, body);
      if (route === "/v1/manage") {
        const result = handlers.manage ? await handlers.manage() : { agents: [agent] };
        return { ok: true, json: async () => ({ hub: {}, network: {}, autostart: { supported: true }, ...result }) };
      }
      assert.equal(route, "/v1/state");
      return { ok: true, json: async () => ({ messages: {}, tasks: {} }) };
    },
  };
  ui.start(window);
  ids.get("token").value = "offline-test-token";
  await ids.get("login-form").trigger("submit");
  return { ids, calls, card: ids.get("agents").children[0] };
}

const cardButton = (card, label) => descendants(card).find(node => node.tagName === "BUTTON" && node.textContent === label);
const cardFeedback = card => descendants(card).find(node => node.className.split(" ").includes("agent-feedback"));

test("Grok configuration button shows returned manual steps inside its own card", async () => {
  const agent = fixtureAgent({ id: "grok", driver: "grok-routine", installation_state: "not_installed" });
  const result = { ok: true, detail: "仅展示官方 Routine 配置步骤，未执行安装。", steps: ["填写真实 webhook。", "保持 queue；邮箱协议为 0。"] };
  let reads = 0;
  const page = await dashboardFixture(agent, {
    post: async () => ({ ok: true, json: async () => result }),
    manage: async () => ({ agents: [{ ...agent, communication_language: ++reads > 1 ? "ru" : "en" }] }),
  });
  const session = descendants(page.card).find(node => node.placeholder === "填写已确认的真实会话 ID");
  const select = descendants(page.card).find(node => node.tagName === "SELECT");
  session.value = "unsaved-existing-session";
  select.value = "nb";
  await select.trigger("change");
  const returned = await cardButton(page.card, "查看配置步骤").trigger("click");
  const feedback = cardFeedback(page.card);
  assert.ok(feedback && !feedback.hidden, "the clicked card must show feedback without finding the global notice");
  for (const value of [result.detail, ...result.steps]) assert.ok(text(feedback).includes(value), value);
  assert.ok(text(page.card).includes("尚未安装"), "manual instructions must not imply an installed Client");
  assert.equal(session.value, "unsaved-existing-session");
  assert.equal(select.value, "nb", "a state refresh must preserve the unsaved language draft");
  assert.deepEqual(returned, result, "the production mutation returns its API result");
  assert.equal(page.calls.filter(call => call.method === "POST").length, 1);
  await page.ids.get("refresh").trigger("click");
  assert.equal(cardFeedback(page.card), feedback, "state refresh must preserve the same card feedback");
  assert.ok(text(feedback).includes(result.steps[0]));
});

test("card action shows loading and its result before an asynchronous refresh completes", async () => {
  const response = deferred(), refreshing = deferred(), refreshStarted = deferred();
  let reads = 0;
  const agent = fixtureAgent({ id: "grok", driver: "grok-routine" });
  const page = await dashboardFixture(agent, {
    post: () => response.promise,
    manage: async () => { if (++reads > 1) { refreshStarted.resolve(); await refreshing.promise; } return { agents: [agent] }; },
  });
  const button = cardButton(page.card, "查看配置步骤");
  const pending = button.trigger("click");
  try {
    assert.ok(cardFeedback(page.card) && !cardFeedback(page.card).hidden, "loading feedback belongs to the clicked card");
    assert.ok(text(cardFeedback(page.card)).includes("正在读取配置步骤"));
    assert.equal(button.disabled, true);
    response.resolve({ ok: true, json: async () => ({ ok: true, detail: "配置步骤已读取", steps: ["核对 Routine。"] }) });
    await refreshStarted.promise;
    assert.ok(text(cardFeedback(page.card)).includes("核对 Routine。"), "show the API result while refresh is still pending");
    assert.equal(button.disabled, true, "keep the existing busy gate through the refresh");
    await button.trigger("click");
    assert.equal(page.calls.filter(call => call.method === "POST").length, 1);
  } finally {
    response.resolve({ ok: true, json: async () => ({ ok: true, detail: "已读取" }) });
    refreshing.resolve();
    await pending;
  }
  assert.equal(button.disabled, false);
});

test("API failures are visible in the clicked card and do not discard drafts", async () => {
  const page = await dashboardFixture(fixtureAgent(), {
    post: async () => ({ ok: false, status: 422, text: async () => '{"error":"当前权限不足，请在原生宿主核对。"}' }),
  });
  const session = descendants(page.card).find(node => node.placeholder === "填写已确认的真实会话 ID");
  const select = descendants(page.card).find(node => node.tagName === "SELECT");
  session.value = "unsaved-existing-session";
  select.value = "nb";
  await select.trigger("change");
  await cardButton(page.card, "保存语言").parentNode.trigger("submit");
  const feedback = cardFeedback(page.card);
  assert.ok(feedback && !feedback.hidden, "action errors must be visible beside the clicked controls");
  assert.ok(text(feedback).includes("当前权限不足"));
  assert.ok(feedback.className.includes("error"));
  assert.equal(session.value, "unsaved-existing-session");
  assert.equal(select.value, "nb");
  assert.ok(text(page.card).includes("尚未保存"));
  assert.equal(cardButton(page.card, "保存语言").disabled, false);
});

test("successful language save survives a failed refresh with a visible verification warning", async () => {
  let reads = 0;
  const page = await dashboardFixture(fixtureAgent(), {
    post: async () => ({ ok: true, json: async () => ({ ok: true, detail: "语言已保存", skill_sync_complete: true }) }),
    manage: async () => { if (++reads > 1) throw new Error("状态连接暂不可用"); return { agents: [fixtureAgent()] }; },
  });
  const select = descendants(page.card).find(node => node.tagName === "SELECT");
  select.value = "nb";
  await select.trigger("change");
  await cardButton(page.card, "保存语言").parentNode.trigger("submit");
  const feedback = cardFeedback(page.card);
  assert.ok(feedback && !feedback.hidden);
  assert.ok(text(feedback).includes("语言已保存"));
  assert.ok(text(feedback).includes("刷新失败"));
  assert.ok(feedback.className.includes("warning"));
  assert.equal(text(page.card).includes("尚未保存的语言设置"), false, "a refresh error does not undo a successful save");
});

test("remote management uses only an exact qualified local proxy path", () => {
  const id = "a".repeat(64);
  assert.equal(ui.remotePrefix("/remote/" + id + "/"), "/remote/" + id);
  for (const path of ["/", "/remote/not-a-hub/", "/remote/"+id+"/remote/other/", "https://other.invalid/", "/remote/"+id+"/?url=other"]) assert.equal(ui.remotePrefix(path), "");
});

test("remote page cannot grant management or recursively open another Hub", async () => {
  const peer = {id:"a".repeat(64),state:"connected",agents:[],management_allowed:false};
  const calls = [];
  const local = ui.createPeerCard(document,peer,body=>calls.push(body),{openRemote:id=>calls.push(id)});
  assert.ok(text(local).includes("允许此主机管理本机"));
  const allow = descendants(local).find(n=>n.tagName==="BUTTON"&&n.textContent==="允许此主机管理本机");
  await allow.trigger("click");
  assert.deepEqual(calls[0],{action:"allow_management",peer_id:peer.id});
  const remote = ui.createPeerCard(document,peer,()=>{}, {remote:true,openRemote:()=>{throw Error("recursive");}});
  assert.equal(descendants(remote).filter(n=>n.tagName==="BUTTON").length,0);
  const relay=ui.createPeerCard(document,{...peer,state:"relayed"},()=>{},{openRemote:()=>{}});
  assert.equal(descendants(relay).find(n=>n.textContent==="打开远端管理").disabled,true);
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
