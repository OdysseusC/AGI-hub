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
  actions: ["install", "enable", "share"], ...changes,
});

test("installation and runtime states never imply native validation", () => {
  assert.equal(typeof ui.installationStatus, "function");
  assert.equal(ui.installationStatus("installed").label, "文件已安装");
  assert.equal(ui.installationStatus("pending_binding").label, "待绑定会话");
  assert.equal(ui.installationStatus("configured").label, "已配置");
  assert.equal(ui.installationStatus("validated").label, "已验证");
  assert.equal(ui.installationStatus("new-state").label, "状态待核实");
  assert.equal(ui.sessionStatus("unknown").label, "待核实");
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
