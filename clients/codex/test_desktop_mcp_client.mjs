/** Local fixtures only: no official MCP process or native assistant is started. */
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import http from 'node:http';
import { EventEmitter } from 'node:events';
import { PassThrough } from 'node:stream';
import {
  ConfigurationError, HubError, McpError, PROTOCOL_VERSION, parseJSON, parseAddress,
  validateConfig, loadConfig, validateBinding, validateTool, notification,
  MCPConnection, DesktopMcpAdapter, HubAPI, drain, consume,
} from './desktop_mcp_client.mjs';

const target = 'bound-original-thread';
const initialize = () => ({ protocolVersion: PROTOCOL_VERSION, capabilities: { tools: {} }, serverInfo: { name: 'codex-app-tools', version: '0.1.0' } });
const tools = () => ({ tools: [{ name: 'send_message_to_thread', inputSchema: { type: 'object', required: ['threadId', 'prompt'], properties: { threadId: { type: 'string' }, prompt: { type: 'string' } } } }] });

test('notifications use current communication policy without translating or duplicating original materials', () => {
  const legacy = envelope();
  const mailbox = { ...legacy, mailbox: true, task_id: '', kind: 'request', root_origin: { endpoint: 'human-entry' } };
  for (const source of [legacy, mailbox]) {
    const message = { ...source, communication_language: 'ru', content: 'Necessary context. 中文原件保留。' };
    const text = notification(message.recipient, message);
    for (const expected of ['communication-policy.md', 'peers.send_language', 'original materials', 'one concise version', 'human in their requested or original input language']) assert.ok(text.includes(expected), expected);
    assert.equal(text.split('中文原件保留。').length - 1, 1);
    assert.ok(text.includes('"communication_language":"ru"'));
    assert.equal(message.content, 'Necessary context. 中文原件保留。');
  }
});
const acknowledgement = (threadId = target) => ({ isError: false, content: [{ type: 'text', text: JSON.stringify({ threadId }) }] });
const envelope = () => ({ message_id: 'message-test', task_id: 'task-test', correlation_id: 'correlation-test', recipient: 'codex-test', sender: 'workbuddy-test', kind: 'result', mode: 'queue', delivery: 'dispatching', content: 'verified result' });
const binding = () => ({ protocol_version: 1, endpoint: { id: 'codex-test', enabled: true, driver: 'codex', session_id: target, modes: ['queue'], idle_wake_verified: false, result_queue_while_busy_verified: true }, session: { state: 'busy' } });

function fixture(t) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'agi-hub-node-test-'));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const script = path.join(directory, 'server.mjs'); fs.writeFileSync(script, '// public fixture, never executed');
  const raw = { base_url: 'http://127.0.0.1:7331', endpoint: 'codex-test', endpoint_token: 'scope-test', native: { transport: 'desktop-mcp', command: process.execPath, args: [script], cwd: directory, thread_id: target, timeout_seconds: 1 } };
  const environment = { CODEX_THREAD_ID: 'official-host-caller', CODEX_APP_TOOLS_PIPE_PATH: 'official-host-path', HOME: directory, PATH: '/usr/bin:/bin', SECRET_TOKEN: 'not-inherited', NODE_OPTIONS: 'not-inherited' };
  return { raw, environment, config: validateConfig(raw, environment), directory };
}

class FakeRPC {
  constructor(replies = [initialize(), tools(), acknowledgement()]) { this.replies = replies; this.calls = []; this.notifications = []; this.closed = false; this.starts = 0; }
  start() { this.starts++; }
  async call(method, params) {
    this.calls.push({ method, params });
    const value = this.replies.shift(); if (value instanceof Error) throw value;
    return value;
  }
  notify(method) { this.notifications.push(method); }
  close() { this.closed = true; }
}

const stateTools = () => ({ tools: [...tools().tools, { name: 'read_thread', inputSchema: {
  type: 'object', required: ['threadId'], properties: { threadId: { type: 'string' },
    turnLimit: { type: 'number' }, includeOutputs: { type: 'boolean' }, maxOutputCharsPerItem: { type: 'number' } },
} }] });
const observedThread = (state = 'idle', turnStatus = 'completed') => ({ isError: false, content: [{ type: 'text', text: JSON.stringify({
  schemaVersion: 1, thread: { id: target, kind: 'codex', hostId: 'local', status: { type: state, ...(state === 'active' ? { activeFlags: [] } : {}) } },
  turns: [{ id: 'native-turn-test', status: turnStatus, messages: ['PRIVATE CHAT MUST NOT LEAVE THE OBSERVER'] }],
}) }] });

test('native session observation reads only the bound thread and retains a real busy turn ID', async t => {
  const { config } = fixture(t); config.caller = target;
  const rpc = new FakeRPC([initialize(), stateTools(), observedThread('active', 'inProgress')]);
  const adapter = new DesktopMcpAdapter(config, { connectionFactory: () => rpc });
  assert.deepEqual(await adapter.observeSession(), { state: 'busy', run_id: 'native-turn-test' });
  const read = rpc.calls.at(-1).params;
  assert.equal(read.name, 'read_thread');
  assert.deepEqual(read.arguments, { threadId: target, turnLimit: 1, includeOutputs: false, maxOutputCharsPerItem: 1 });
  assert.equal(read._meta.threadId, target);
  assert.ok(!rpc.calls.some(call => call.params?.name === 'wait_threads'));
  adapter.close();
});

test('drain restores an unknown Hub session before claim and ends after one native input', async t => {
  const { config } = fixture(t), sequence = [], snapshots = [];
  const rpc = new FakeRPC([initialize(), stateTools(), observedThread(), acknowledgement()]);
  const adapter = new DesktopMcpAdapter(config, { connectionFactory: () => rpc });
  const api = { endpoint: config.endpoint,
    async setSession(snapshot) { sequence.push('session'); snapshots.push(snapshot); },
    async post(name) { sequence.push(name); return name === 'inbox' ? envelope() : { ok: true }; },
  };
  await drain(api, adapter);
  assert.deepEqual(sequence, ['session', 'inbox', 'delivery']);
  assert.deepEqual(snapshots, [{ state: 'idle', run_id: '' }]);
  assert.deepEqual(rpc.calls.filter(call => call.method === 'tools/call').map(call => call.params.name), ['read_thread', 'send_message_to_thread']);
  assert.ok(!JSON.stringify(snapshots).includes('PRIVATE'));
  adapter.close();
});

test('the next receipt event re-observes a newly active turn before another claim', async t => {
  const { config } = fixture(t), snapshots = []; let claims = 0;
  const rpc = new FakeRPC([initialize(), stateTools(), observedThread(), acknowledgement(), observedThread('active', 'inProgress')]);
  const adapter = new DesktopMcpAdapter(config, { connectionFactory: () => rpc });
  const api = { endpoint: config.endpoint, async setSession(snapshot) { snapshots.push(snapshot); },
    async post(name) { return name === 'inbox' && ++claims === 1 ? envelope() : name === 'inbox' ? null : { ok: true }; } };
  await drain(api, adapter); // Initial connection.
  await drain(api, adapter); // Actual receipt event from the subscribed stream.
  assert.deepEqual(snapshots, [{ state: 'idle', run_id: '' }, { state: 'busy', run_id: 'native-turn-test' }]);
  assert.deepEqual(rpc.calls.filter(call => call.method === 'tools/call').map(call => call.params.name), ['read_thread', 'send_message_to_thread', 'read_thread']);
  assert.equal(claims, 2); adapter.close();
});

test('send-only old hosts explicitly fail closed and clear stale idle without claiming', async t => {
  const { config } = fixture(t), snapshots = [];
  const rpc = new FakeRPC([initialize(), tools()]);
  const adapter = new DesktopMcpAdapter(config, { connectionFactory: () => rpc });
  const api = { endpoint: config.endpoint, async setSession(snapshot) { snapshots.push(snapshot); }, async post() { assert.fail('unsupported host claimed inbox'); } };
  await assert.rejects(drain(api, adapter), /desktop_mcp_session_observation_unavailable/);
  assert.deepEqual(snapshots, [{ state: 'unknown', run_id: '' }]);
  assert.ok(!rpc.calls.some(call => call.method === 'tools/call'));
  adapter.close();
});

test('unchanged native state still checks the current binding before every claim', async () => {
  let bound = true, claims = 0, writes = 0;
  const api = { endpoint: 'codex-test', async setSession() { writes++; if (!bound) throw new HubError('native_session_binding_mismatch', 409); },
    async post() { assert.ok(bound, 'rebound endpoint was claimed using cached old native state'); claims++; return null; } };
  const adapter = { async observeSession() { return { state: 'idle', run_id: '' }; }, async preflight() {} };
  await drain(api, adapter);
  bound = false;
  await assert.rejects(drain(api, adapter), error => error instanceof HubError && error.status === 409);
  assert.equal(claims, 1); assert.equal(writes, 2);
});

test('wrong, unsupported or incomplete native metadata clears stale idle and never claims', async t => {
  const { config } = fixture(t);
  const changes = [
    value => { value.thread.id = 'another-thread'; }, value => { value.thread.kind = 'chatgpt'; },
    value => { value.thread.hostId = 'remote'; }, value => { value.schemaVersion = 2; },
    value => { value.thread.status.type = 'notLoaded'; }, value => { value.thread.status.type = 'systemError'; },
    value => { value.thread.status.type = 'active'; value.thread.status.activeFlags = []; },
    value => { value.turns[0].status = 'inProgress'; }, value => { value.turns[0].id = ''; },
    value => { value.turns[0].id = value.thread.id; },
    value => { value.turns = []; }, value => { value.turns.push(value.turns[0]); },
    value => { value.thread.status.activeFlags = ['waitingOnApproval']; },
  ];
  const replies = changes.map(change => { const reply = observedThread(); const value = JSON.parse(reply.content[0].text); change(value); reply.content[0].text = JSON.stringify(value); return reply; });
  replies.push(new McpError('PRIVATE native failure'), { isError: true, content: [{ type: 'text', text: 'PRIVATE ERROR' }] },
    { isError: false, content: [{ type: 'text', text: 'not JSON' }] });
  for (const reply of replies) {
    const rpc = new FakeRPC([initialize(), stateTools(), reply]);
    const adapter = new DesktopMcpAdapter(config, { connectionFactory: () => rpc }), snapshots = [];
    const api = { endpoint: config.endpoint, async setSession(snapshot) { snapshots.push(snapshot); }, async post() { assert.fail('unverified observation claimed inbox'); } };
    await assert.rejects(drain(api, adapter), error => error instanceof McpError && error.message === 'desktop_mcp_session_observation_unconfirmed');
    assert.deepEqual(snapshots, [{ state: 'unknown', run_id: '' }]);
    adapter.close();
  }
});

test('native disconnect invalidates a previous idle observation before any further claim', async t => {
  const { config } = fixture(t), snapshots = []; let claims = 0;
  const rpc = new FakeRPC([initialize(), stateTools(), observedThread(), new McpError('desktop_mcp_closed')]);
  const adapter = new DesktopMcpAdapter(config, { connectionFactory: () => rpc });
  const api = { endpoint: config.endpoint, async setSession(snapshot) { snapshots.push(snapshot); }, async post() { claims++; return null; } };
  await drain(api, adapter);
  await assert.rejects(drain(api, adapter), /session_observation_unconfirmed/);
  assert.deepEqual(snapshots, [{ state: 'idle', run_id: '' }, { state: 'unknown', run_id: '' }]);
  assert.equal(claims, 1); adapter.close();
});

test('unconfirmed session publication never permits inbox claim', async () => {
  for (const error of [new HubError('hub_request_unconfirmed'), new HubError('binding_changed', 409), new HubError('old_hub', 404)]) {
    const api = { async setSession() { throw error; }, async post() { assert.fail('unconfirmed state claimed inbox'); } };
    const adapter = { async observeSession() { return { state: 'idle', run_id: '' }; } };
    await assert.rejects(drain(api, adapter), candidate => candidate === error);
  }
});

test('Hub reconnection republishes the same native snapshot after restart resets unknown', async t => {
  const { config } = fixture(t), controller = new AbortController(), snapshots = [];
  let connections = 0, claims = 0, reads = 0, hubSession = { state: 'unknown' };
  const api = { endpoint: config.endpoint,
    async get() { if (connections === 1) hubSession = { state: 'unknown' }; return { ...binding(), session: hubSession }; },
    async setSession(snapshot) { snapshots.push(snapshot); hubSession = snapshot; },
    async post() { assert.equal(hubSession.state, 'idle'); claims++; return null; },
    async subscribe() { const generation = ++connections; return { close() {}, async *changes() {
      if (generation === 1) throw new HubError('event_stream_disconnected');
      controller.abort();
    } }; },
  };
  const adapter = { async preflight() {}, async observeSession() { reads++; return { state: 'idle', run_id: '' }; }, close() {} };
  assert.equal(await consume(config, { api, adapter, signal: controller.signal, status() {}, sleep: async () => {} }), 0);
  assert.deepEqual(snapshots, [{ state: 'idle', run_id: '' }, { state: 'idle', run_id: '' }]);
  assert.equal(reads, 2); assert.equal(claims, 2);
});

test('read tool schema is verified before any metadata call or claim', async t => {
  const { config } = fixture(t);
  for (const change of [value => { value.tools[1].inputSchema.required.push('hostId'); },
    value => { value.tools[1].inputSchema.properties.includeOutputs.type = 'string'; },
    value => { value.tools.push(value.tools[1]); }]) {
    const listed = stateTools(); change(listed);
    const rpc = new FakeRPC([initialize(), listed]), snapshots = [];
    const adapter = new DesktopMcpAdapter(config, { connectionFactory: () => rpc });
    const api = { async setSession(snapshot) { snapshots.push(snapshot); }, async post() { assert.fail('unverified read tool claimed inbox'); } };
    await assert.rejects(drain(api, adapter), /read_thread_schema_unverified/);
    assert.deepEqual(snapshots, [{ state: 'unknown', run_id: '' }]);
    assert.ok(!rpc.calls.some(call => call.method === 'tools/call'));
    adapter.close();
  }
});

test('initial stream and session change produce three observations, then quiet Hub emits no periodic reads', { timeout: 2000 }, async t => {
  const { config } = fixture(t), controller = new AbortController();
  let stream, hubSession = { state: 'unknown', run_id: '' }, commits = 0, publications = 0, claims = 0, quiet = false;
  const rpc = new FakeRPC([initialize(), stateTools(), observedThread(), observedThread(), observedThread()]);
  const adapter = new DesktopMcpAdapter(config, { connectionFactory: () => rpc });
  const server = http.createServer((request, response) => {
    const route = new URL(request.url, 'http://fixture').pathname;
    assert.equal(request.headers['x-agi-hub-native-session'], target);
    const parts = []; request.on('data', part => parts.push(part)); request.on('end', () => {
      if (route === '/v1/events') {
        stream = response; response.writeHead(200, { 'Content-Type': 'text/event-stream' });
        response.write('event: change\ndata: {}\n\n'); response.flushHeaders(); return;
      }
      response.setHeader('Content-Type', 'application/json');
      if (route === '/v1/endpoint') response.end(JSON.stringify({ ...binding(), session: hubSession }));
      else if (route === '/v1/endpoint/session') {
        const snapshot = JSON.parse(Buffer.concat(parts)); publications++;
        assert.deepEqual(Object.keys(snapshot).sort(), ['run_id', 'state']);
        const changed = JSON.stringify(snapshot) !== JSON.stringify(hubSession);
        hubSession = snapshot; response.end('{"ok":true}');
        if (changed) { commits++; stream.write('event: change\ndata: {}\n\n'); }
      } else if (route === '/v1/inbox') {
        claims++; response.end('null');
        if (claims === 3) {
          stream.write(': keepalive\n\n');
          setTimeout(() => { quiet = rpc.calls.filter(call => call.params?.name === 'read_thread').length === 3 && claims === 3; controller.abort(); }, 40);
        }
      } else assert.fail('unexpected route ' + route);
    });
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(() => { controller.abort(); stream?.destroy(); server.closeAllConnections(); server.close(); });
  config.base_url = 'http://127.0.0.1:' + server.address().port; config.address = parseAddress(config.base_url);
  assert.equal(await consume(config, { adapter, signal: controller.signal, status() {} }), 0);
  assert.equal(commits, 1); assert.equal(publications, 3); assert.equal(claims, 3); assert.equal(quiet, true);
  assert.ok(!rpc.calls.some(call => call.params?.name === 'wait_threads' || call.params?.name === 'send_message_to_thread'));
});

test('private config rejects duplicate keys, unsafe URL, caller overrides and missing host context', t => {
  const { raw, environment, directory } = fixture(t);
  for (const url of ['http://localhost:7331', 'http://127.1:7331', 'http://0177.0.0.1:7331', 'http://192.168.1.1:7331', 'http://secret@127.0.0.1:7331', 'http://127.0.0.1:7331/v1', 'http://127.0.0.1:7331?secret=x']) assert.throws(() => parseAddress(url), ConfigurationError);
  assert.equal(parseAddress('http://[::1]:7331').origin, 'http://[::1]:7331');
  assert.equal(parseAddress('http://127.0.0.2:80').authority, '127.0.0.2:80');
  assert.throws(() => parseJSON('{"nested":{"x":1,"x":2}}'), /duplicate_json_key/);
  assert.deepEqual(parseJSON('{"escaped\\\"key":[{"ok":"\\\\"},2,true,null]}'), { 'escaped"key': [{ ok: '\\' }, 2, true, null] });
  const filename = path.join(directory, 'client.json');
  fs.writeFileSync(filename, JSON.stringify(raw)); assert.equal(loadConfig(filename, environment).caller, 'official-host-caller');
  fs.writeFileSync(filename, '{"endpoint":"one","endpoint":"two"}'); assert.throws(() => loadConfig(filename, environment), /duplicate_json_key/);
  for (const key of ['CODEX_THREAD_ID', 'CODEX_APP_TOOLS_PIPE_PATH']) { const missing = { ...environment }; delete missing[key]; assert.throws(() => validateConfig(raw, missing), ConfigurationError); }
  assert.throws(() => validateConfig({ ...raw, native: { ...raw.native, environment: { CODEX_THREAD_ID: 'forged' } } }, environment), ConfigurationError);
  assert.throws(() => validateConfig({ ...raw, native: { ...raw.native, args: [raw.native.args[0], '--interaction-client-id'] } }, environment), ConfigurationError);
});

test('legacy receive_tasks requires removing only that key while strict native validation stays enabled', t => {
  const { raw, environment, directory } = fixture(t);
  const filename = path.join(directory, 'legacy-client.json');
  for (const receiveTasks of [true, false]) {
    const legacy = { ...raw, native: { ...raw.native, receive_tasks: receiveTasks } };
    fs.writeFileSync(filename, JSON.stringify(legacy));
    assert.throws(() => loadConfig(filename, environment), error => error instanceof ConfigurationError && error.message === 'invalid_codex_desktop_mcp_fields');
    const migrated = structuredClone(legacy);
    delete migrated.native.receive_tasks;
    assert.deepEqual(migrated, raw);
    fs.writeFileSync(filename, JSON.stringify(migrated));
    const accepted = loadConfig(filename, environment);
    assert.equal(accepted.native.thread_id, target);
    assert.equal(accepted.caller, environment.CODEX_THREAD_ID);
    assert.notEqual(accepted.caller, accepted.native.thread_id);
    assert.throws(() => validateConfig({ ...migrated, native: { ...migrated.native, another_legacy_flag: true } }, environment), error => error instanceof ConfigurationError && error.message === 'invalid_codex_desktop_mcp_fields');
  }
});

test('binding, tool schema and result-only checks preserve configured native target', t => {
  const { config } = fixture(t);
  validateBinding(binding(), config); validateTool(tools());
  for (const change of [value => { value.protocol_version = true; }, value => { value.endpoint.session_id = 'other'; }, value => { value.endpoint.driver = 'other'; }, value => { value.endpoint.modes = ['steer']; }, value => { value.session.state = 'imaginary'; }, value => { value.endpoint.result_queue_while_busy_verified = 'true'; }]) { const value = binding(); change(value); assert.throws(() => validateBinding(value, config), ConfigurationError); }
  const unknown = binding(); unknown.session.state = 'unknown'; validateBinding(unknown, config); assert.equal(unknown.session.state, 'unknown');
  const wrong = tools(); wrong.tools[0].inputSchema.required.push('permissionOverride'); assert.throws(() => validateTool(wrong), McpError);
  for (const message of [{ ...envelope(), kind: 'task' }, { ...envelope(), recipient: 'another' }, { ...envelope(), mode: 'interrupt' }]) assert.throws(() => notification(config.endpoint, message), ConfigurationError);
});

test('MCP result calls only fixed official tool using host caller and verifies target ACK', async t => {
  const { config } = fixture(t); const rpc = new FakeRPC();
  const adapter = new DesktopMcpAdapter(config, { connectionFactory: () => rpc });
  const message = { ...envelope(), caller_thread_id: 'untrusted-envelope-caller' };
  const result = await adapter.deliver(message);
  assert.equal(result.outcome, 'delivered'); assert.equal(rpc.starts, 1);
  assert.deepEqual(rpc.calls.map(call => call.method), ['initialize', 'tools/list', 'tools/call']);
  assert.deepEqual(rpc.notifications, ['notifications/initialized']);
  const sent = rpc.calls[2].params;
  assert.equal(sent.name, 'send_message_to_thread'); assert.deepEqual(Object.keys(sent.arguments).sort(), ['prompt', 'threadId']);
  assert.equal(sent.arguments.threadId, target); assert.equal(sent._meta.threadId, 'official-host-caller');
  assert.match(sent.arguments.prompt, /kind=result, not a new task/); assert.match(sent.arguments.prompt, /"caller_thread_id":"untrusted-envelope-caller"/);
  assert.ok(!JSON.stringify(sent).includes('official-host-path'));
  adapter.close();
});

test('addressed requests and replies guide only the configured original thread', async t => {
  const { config } = fixture(t);
  const root_origin = { endpoint: 'human-entry', device_id: 'local', session_id: 'human-original-chat', request_ref: 'human-turn' };
  const mailBinding = binding(); mailBinding.endpoint.mailbox_protocol_version = 1; mailBinding.endpoint.mailbox_while_busy_verified = true;
  validateBinding(mailBinding, config);
  for (const kind of ['request', 'reply', 'result']) {
    const message = { ...envelope(), kind, mailbox: true, task_id: '', root_origin, target_thread_id: 'cannot-retarget' };
    const rpc = new FakeRPC(); const adapter = new DesktopMcpAdapter(config, { connectionFactory: () => rpc });
    const result = await adapter.deliver(message);
    assert.equal(result.outcome, 'delivered');
    const sent = rpc.calls.find(call => call.method === 'tools/call').params;
    assert.equal(sent.arguments.threadId, target);
    assert.match(sent.arguments.prompt, /mail-send/);
    assert.match(sent.arguments.prompt, /REQUIRED new stable id/);
    assert.match(sent.arguments.prompt, /"id":"<new-saved-stable-id>"/);
    assert.match(sent.arguments.prompt, /"reply_to":"message-test"/);
    assert.match(sent.arguments.prompt, /recipient you choose/);
    assert.ok(!sent.arguments.prompt.includes('parent_task_id'));
    assert.match(sent.arguments.prompt, /root_origin/);
    assert.match(sent.arguments.prompt, /tokens/);
    assert.equal(rpc.calls.filter(call => call.method === 'tools/call').length, 1);
    adapter.close();
  }
  for (const change of [value => { value.endpoint.mailbox_protocol_version = true; }, value => { value.endpoint.mailbox_protocol_version = 2; }, value => { value.endpoint.mailbox_while_busy_verified = 'true'; }]) {
    const value = binding(); change(value); assert.throws(() => validateBinding(value, config), ConfigurationError);
  }
  assert.throws(() => notification(config.endpoint, { ...envelope(), kind: 'reply', mailbox: true, task_id: '', root_origin: null }), /invalid_mailbox_native/);
});

test('mail inbox drain records status silently, acknowledges native input and never generates replies', async t => {
  const { config } = fixture(t); const calls = [];
  const message = { ...envelope(), kind: 'request', mailbox: true, task_id: '', root_origin: { endpoint: 'human-entry' } };
  const pending = [{ ...message, kind: 'status' }, message, null]; let nativeInputs = 0;
  const api = { endpoint: config.endpoint, mailbox: true, async setSession() {}, async post(name) { calls.push(name); return name === 'mail/inbox' ? pending.shift() : { ok: true }; } };
  const adapter = { async observeSession() { return { state: 'idle', run_id: '' }; }, async preflight() {}, async deliver() { nativeInputs++; return { outcome: 'delivered', detail: 'input acknowledged' }; } };
  await drain(api, adapter, { status() {} });
  assert.deepEqual(calls, ['mail/inbox', 'mail/delivery']);
  assert.equal(nativeInputs, 0);
  await drain(api, adapter, { status() {} });
  assert.deepEqual(calls, ['mail/inbox', 'mail/delivery', 'mail/inbox', 'mail/delivery']);
  assert.equal(nativeInputs, 1);
  assert.ok(!calls.some(name => ['report', 'mail/send', 'peer/settled'].includes(name)));
});

test('MCP discovery fails before native call; ambiguous attempted delivery never retries or falls back', async t => {
  const { config } = fixture(t);
  for (const replies of [[new McpError('closed')], [initialize(), { tools: [] }]]) {
    const rpc = new FakeRPC(replies); const adapter = new DesktopMcpAdapter(config, { connectionFactory: () => rpc });
    assert.equal((await adapter.deliver(envelope())).outcome, 'failed'); assert.ok(!rpc.calls.some(call => call.method === 'tools/call')); adapter.close();
  }
  for (const reply of [new McpError('timeout'), acknowledgement('wrong'), { isError: true, content: [{ type: 'text', text: 'PRIVATE ERROR' }] }, { isError: false, content: [{ type: 'text', text: 'not JSON' }] }, { isError: false, content: [{ type: 'text', text: '{"threadId":"wrong","threadId":"bound-original-thread"}' }] }]) {
    const rpc = new FakeRPC([initialize(), tools(), reply]); const adapter = new DesktopMcpAdapter(config, { connectionFactory: () => rpc });
    const result = await adapter.deliver(envelope()); assert.equal(result.outcome, 'uncertain'); assert.ok(!result.detail.includes('PRIVATE'));
    assert.equal(rpc.calls.filter(call => call.method === 'tools/call').length, 1); assert.equal(rpc.closed, true); adapter.close();
  }
});

function fakeProcess(handler, captured) {
  const child = new EventEmitter(); child.stdin = new PassThrough(); child.stdout = new PassThrough(); child.exitCode = null; child.signalCode = null; child.kills = [];
  child.kill = signal => { child.kills.push(signal ?? 'SIGTERM'); child.exitCode = 0; queueMicrotask(() => child.emit('close', 0)); return true; };
  let buffer = '';
  child.stdin.on('data', chunk => {
    buffer += chunk.toString();
    while (buffer.includes('\n')) {
      const index = buffer.indexOf('\n'); const frame = JSON.parse(buffer.slice(0, index)); buffer = buffer.slice(index + 1); captured.push(frame);
      const response = handler(frame);
      if (response !== undefined) queueMicrotask(() => child.stdout.write(Buffer.from(JSON.stringify({ jsonrpc: '2.0', id: frame.id, result: response }) + '\n')));
    }
  });
  return child;
}

test('stdio client uses JSON-RPC framing, single safe spawn and filtered environment', async t => {
  const { config } = fixture(t); const captured = [], launches = [];
  let child;
  const adapter = new DesktopMcpAdapter(config, { connectionFactory: (native, environment) => new MCPConnection(native, environment, { spawnProcess(command, args, options) {
    launches.push({ command, args, options });
    return child = fakeProcess(frame => ({ initialize: initialize, 'tools/list': tools, 'tools/call': acknowledgement }[frame.method]?.()), captured);
  } }) });
  assert.equal((await adapter.deliver(envelope())).outcome, 'delivered'); adapter.close();
  assert.equal(launches.length, 1); assert.equal(launches[0].options.shell, false); assert.deepEqual(launches[0].options.stdio, ['pipe', 'pipe', 'ignore']);
  assert.equal(launches[0].options.env.CODEX_APP_TOOLS_PIPE_PATH, 'official-host-path');
  assert.equal(launches[0].options.env.SECRET_TOKEN, undefined); assert.equal(launches[0].options.env.NODE_OPTIONS, undefined);
  assert.ok(captured.every(frame => frame.jsonrpc === '2.0')); assert.equal(captured.filter(frame => frame.method === 'tools/call').length, 1);
  assert.equal(child.kills.length, 1);
});

test('stdio timeout closes only helper and an attempted input remains uncertain once', async t => {
  const { config } = fixture(t); config.native.timeout_seconds = 0.01;
  const captured = []; let child;
  const adapter = new DesktopMcpAdapter(config, { connectionFactory: (native, environment) => new MCPConnection(native, environment, { spawnProcess: () => child = fakeProcess(frame => frame.method === 'initialize' ? initialize() : frame.method === 'tools/list' ? tools() : undefined, captured) }) });
  assert.equal((await adapter.deliver(envelope())).outcome, 'uncertain');
  assert.equal(captured.filter(frame => frame.method === 'tools/call').length, 1); assert.equal(child.kills.length, 1);
});

test('HTTP/SSE subscribes before inbox, passes scope headers and keepalive does not poll', async t => {
  const { config } = fixture(t); const controller = new AbortController(); const sequence = [], receipts = []; let inbox = 0, stream, keepaliveChecked = false;
  const server = http.createServer((request, response) => {
    const name = new URL(request.url, 'http://fixture').pathname.split('/').pop(); sequence.push(name);
    assert.equal(request.headers.authorization, 'Bearer scope-test'); assert.equal(request.headers.host, config.address.authority); assert.equal(request.headers.origin, config.address.origin);
    assert.equal(request.headers['x-agi-hub-native-session'], target);
    assert.equal(new URL(request.url, 'http://fixture').searchParams.get('endpoint'), 'codex-test');
    const parts = []; request.on('data', part => parts.push(part)); request.on('end', () => {
      if (name === 'events') { stream = response; response.writeHead(200, { 'Content-Type': 'text/event-stream' }); response.flushHeaders(); return; }
      response.setHeader('Content-Type', 'application/json');
      if (name === 'endpoint') response.end(JSON.stringify(binding()));
      else if (name === 'session') { response.end('{"ok":true}'); }
      else if (name === 'delivery') { receipts.push(JSON.parse(Buffer.concat(parts))); response.end('{"ok":true}'); stream.write('event: change\ndata: {}\n\n'); }
      else if (name === 'inbox') {
        inbox++; response.end(JSON.stringify(inbox === 1 ? envelope() : null));
        if (inbox === 2) {
          stream.write(': keepalive\n\n');
          setTimeout(() => { keepaliveChecked = inbox === 2; stream.write('event: change\ndata: {}\n\n'); }, 20);
        } else if (inbox === 3) setImmediate(() => controller.abort());
      }
    });
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(() => { stream?.destroy(); server.closeAllConnections(); server.close(); });
  config.base_url = 'http://127.0.0.1:' + server.address().port; config.address = parseAddress(config.base_url);
  const rpc = new FakeRPC([initialize(), stateTools(), observedThread(), acknowledgement(), observedThread(), observedThread()]); const adapter = new DesktopMcpAdapter(config, { connectionFactory: () => rpc });
  const code = await consume(config, { signal: controller.signal, status() {}, adapter });
  assert.equal(code, 0); assert.deepEqual(sequence.slice(0, 5), ['endpoint', 'events', 'session', 'inbox', 'delivery']);
  assert.equal(keepaliveChecked, true); assert.equal(inbox, 3); assert.equal(receipts.length, 1); assert.equal(receipts[0].outcome, 'delivered');
  assert.equal(rpc.calls.filter(call => call.params?.name === 'send_message_to_thread').length, 1);
  assert.equal(rpc.calls.filter(call => call.params?.name === 'read_thread').length, 3);
});

test('receipt failure stops after one native attempt and discovery failure claims nothing', async t => {
  const { config } = fixture(t); let attempted = 0, posts = 0;
  const api = { endpoint: config.endpoint, async setSession() {}, async post(name) { posts++; if (name === 'inbox') return envelope(); throw new HubError('offline'); } };
  const adapter = { async observeSession() { return { state: 'idle', run_id: '' }; }, async preflight() {}, async deliver() { attempted++; return { outcome: 'uncertain', detail: 'fixed diagnostic' }; } };
  await assert.rejects(drain(api, adapter), /delivery_receipt_unconfirmed/); assert.equal(attempted, 1); assert.equal(posts, 2);
  let claimed = false;
  const offline = { async preflight() { throw new McpError('desktop_mcp_closed'); }, close() {} };
  const beforeClaim = { async get() { return binding(); }, async subscribe() { claimed = true; }, async post() { claimed = true; } };
  assert.equal(await consume(config, { api: beforeClaim, adapter: offline, status() {} }), 2); assert.equal(claimed, false);
});

test('Hub does not follow HTTP redirects', async t => {
  const { config } = fixture(t); const calls = [];
  const server = http.createServer((request, response) => { calls.push(request.url); response.writeHead(307, { Location: '/must-not-follow' }); response.end(); });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(() => server.close()); config.base_url = 'http://127.0.0.1:' + server.address().port; config.address = parseAddress(config.base_url);
  await assert.rejects(new HubAPI(config).get('endpoint'), error => error instanceof HubError && error.status === 307);
  assert.equal(calls.length, 1);
});
