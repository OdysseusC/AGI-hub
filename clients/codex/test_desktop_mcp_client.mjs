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

test('binding, tool schema and result-only checks preserve configured native target', t => {
  const { config } = fixture(t);
  validateBinding(binding(), config); validateTool(tools());
  for (const change of [value => { value.protocol_version = true; }, value => { value.endpoint.session_id = 'other'; }, value => { value.endpoint.driver = 'other'; }, value => { value.endpoint.modes = ['steer']; }, value => { value.session.state = 'imaginary'; }, value => { value.endpoint.result_queue_while_busy_verified = 'true'; }]) { const value = binding(); change(value); assert.throws(() => validateBinding(value, config), ConfigurationError); }
  const unknown = binding(); unknown.session.state = 'unknown'; validateBinding(unknown, config); assert.equal(unknown.session.state, 'unknown');
  const wrong = tools(); wrong.tools[0].inputSchema.required.push('permissionOverride'); assert.throws(() => validateTool(wrong), McpError);
  for (const message of [{ ...envelope(), kind: 'task' }, { ...envelope(), recipient: 'another' }, { ...envelope(), mode: 'interrupt' }]) assert.throws(() => notification(config.endpoint, message), ConfigurationError);
});

test('task reception is an explicit boolean opt-in and defaults to result-only', t => {
  const { raw, config, environment } = fixture(t);
  assert.equal(config.native.receive_tasks, false);
  for (const value of [false, true]) {
    const validated = validateConfig({ ...raw, native: { ...raw.native, receive_tasks: value } }, environment);
    assert.equal(validated.native.receive_tasks, value);
  }
  for (const value of [null, undefined, 0, 1, 'true', [], {}]) {
    assert.throws(() => validateConfig({ ...raw, native: { ...raw.native, receive_tasks: value } }, environment), /invalid_codex_receive_tasks/);
  }
  assert.throws(() => validateConfig({ ...raw, native: { ...raw.native, receive_tasks: true, task_mode: 'steer' } }, environment), ConfigurationError);
});

test('opted-in task notification preserves envelope, authority and active fixed-helper reporting', () => {
  const message = { ...envelope(), kind: 'task', content: '{"summary":"已授权的工作"}', thread_id: 'untrusted-thread', report_command: 'untrusted-command' };
  const prompt = notification('codex-test', message, true);
  assert.ok(prompt.includes(JSON.stringify(message)));
  assert.match(prompt, /not additional user authorization/);
  assert.match(prompt, /existing native assistant's session and tools/);
  assert.match(prompt, /installed.*fixed.*endpoint-scoped reporting helper/);
  assert.match(prompt, /actively send complete Report JSON/);
  assert.match(prompt, /No history polling/);
  const example = prompt.split('\n').map(line => { try { return JSON.parse(line); } catch { return null; } }).find(value => value?.event_id);
  assert.deepEqual(example, { event_id: '<new stable event ID>', task_id: 'task-test', endpoint: 'codex-test', state: 'accepted', run_id: 'hubrun-c4b3d4580452b0abd2eb01af9193eb29', summary: '<brief status or verified result>' });
  assert.ok(notification('codex-test', { ...message, message_id: 'another-delivery' }, true).includes(example.run_id));
  assert.ok(!notification('codex-test', { ...message, task_id: 'another-task' }, true).includes(example.run_id));
  assert.equal(notification('codex-test', envelope(), true), notification('codex-test', envelope()));
  for (const change of [{ kind: 'control' }, { mode: 'steer' }, { mode: 'interrupt' }, { recipient: 'another' }, { task_id: null }, { task_id: '' }]) {
    assert.throws(() => notification('codex-test', { ...message, ...change }, true), ConfigurationError);
  }
});

test('task delivery opt-in still rejects default tasks, control and unsupported modes before native input', async t => {
  const { config } = fixture(t);
  for (const [receiveTasks, change] of [[false, { kind: 'task' }], [true, { kind: 'control' }], [true, { kind: 'task', mode: 'steer' }], [true, { kind: 'task', mode: 'interrupt' }], [true, { kind: 'task', recipient: 'another' }]]) {
    const rpc = new FakeRPC();
    const adapter = new DesktopMcpAdapter({ ...config, native: { ...config.native, receive_tasks: receiveTasks } }, { connectionFactory: () => rpc });
    assert.equal((await adapter.deliver({ ...envelope(), ...change })).outcome, 'failed');
    assert.equal(rpc.calls.length, 0); adapter.close();
  }
});

test('opted-in task uses only the fixed native tool and delivery receipt, never automatic business reports', async t => {
  const { raw, environment } = fixture(t);
  const config = validateConfig({ ...raw, native: { ...raw.native, receive_tasks: true } }, environment);
  const message = { ...envelope(), kind: 'task', thread_id: 'untrusted-target', caller_thread_id: 'untrusted-caller', native: { tool: 'untrusted-tool', command: 'untrusted-command' } };
  const rpc = new FakeRPC(); const adapter = new DesktopMcpAdapter(config, { connectionFactory: () => rpc });
  const posts = [];
  const api = { endpoint: config.endpoint, async post(name, body) { posts.push({ name, body }); return name === 'inbox' && posts.length === 1 ? message : null; } };
  await drain(api, adapter);
  assert.deepEqual(posts.map(value => value.name), ['inbox', 'delivery', 'inbox']);
  assert.equal(posts[1].body.outcome, 'delivered');
  const sent = rpc.calls.find(call => call.method === 'tools/call').params;
  assert.equal(sent.name, 'send_message_to_thread');
  assert.deepEqual(Object.keys(sent.arguments).sort(), ['prompt', 'threadId']);
  assert.equal(sent.arguments.threadId, target); assert.equal(sent._meta.threadId, 'official-host-caller');
  assert.ok(sent.arguments.prompt.includes(JSON.stringify(message))); assert.ok(!JSON.stringify(sent).includes('official-host-path'));
  adapter.close();
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
  assert.equal(launches[0].options.windowsHide, true);
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
    assert.equal(new URL(request.url, 'http://fixture').searchParams.get('endpoint'), 'codex-test');
    const parts = []; request.on('data', part => parts.push(part)); request.on('end', () => {
      if (name === 'events') { stream = response; response.writeHead(200, { 'Content-Type': 'text/event-stream' }); response.flushHeaders(); return; }
      response.setHeader('Content-Type', 'application/json');
      if (name === 'endpoint') response.end(JSON.stringify(binding()));
      else if (name === 'delivery') { receipts.push(JSON.parse(Buffer.concat(parts))); response.end('{"ok":true}'); }
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
  const rpc = new FakeRPC(); const adapter = new DesktopMcpAdapter(config, { connectionFactory: () => rpc });
  const code = await consume(config, { signal: controller.signal, status() {}, adapter });
  assert.equal(code, 0); assert.deepEqual(sequence.slice(0, 4), ['endpoint', 'events', 'inbox', 'delivery']);
  assert.equal(keepaliveChecked, true); assert.equal(inbox, 3); assert.equal(receipts.length, 1); assert.equal(receipts[0].outcome, 'delivered');
  assert.equal(rpc.calls.filter(call => call.method === 'tools/call').length, 1);
});

test('receipt failure stops after one native attempt and discovery failure claims nothing', async t => {
  const { config } = fixture(t); let attempted = 0, posts = 0;
  const api = { endpoint: config.endpoint, async post(name) { posts++; if (name === 'inbox') return envelope(); throw new HubError('offline'); } };
  const adapter = { async preflight() {}, async deliver() { attempted++; return { outcome: 'uncertain', detail: 'fixed diagnostic' }; } };
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
