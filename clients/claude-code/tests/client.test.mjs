import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { EndpointClient } from '../agi-hub-endpoint/hooks/lib/client.js';
import { absolutePath, byteLength, clientConfigPath, parseAddress, parseJSON, scopeKey, tokenFingerprint, validateBinding, validateConfig } from '../agi-hub-endpoint/hooks/lib/config.js';
import { nativeMarkerShape, notification, reportToolResult, validateEnvelope } from '../agi-hub-endpoint/hooks/lib/protocol.js';

const clone = value => value === undefined ? undefined : JSON.parse(JSON.stringify(value));
const config = (sid = 'session-a') => ({ base_url: 'http://127.0.0.1:7331', endpoint: 'claude', endpoint_token: 'test-only-scoped-token', native: { transport: 'mod', session_id: sid, long_poll_ms: 25000, max_prompt_bytes: 1048576 } });
const envelope = (id = 'one', kind = 'task') => ({ message_id: kind + ':' + id, task_id: id, recipient: 'claude', sender: 'codex', delivery: 'dispatching', mode: 'queue', kind, content: 'Perform the explicitly authorized test.' });
const deferred = () => { let resolve; const promise = new Promise(done => { resolve = done; }); return { promise, resolve }; };
const nativeWrapped = text => 'The agi-hub-endpoint plugin sent a message:\n' + text + '\n\nThis is how Claude Code surfaces a prompt a plugin submits between turns — it starts this turn in the user\'s place. Address the message above.';

function harness({ sid = 'session-a', saved = new Map(), mailbox = false } = {}) {
  const state = { now: 0, sid, calls: [], timers: [], prompts: [], writes: [], saved, available: true, outstanding: [] };
  let client;
  const port = {
    sessionID: async () => state.sid,
    stat: async () => ({ kind: 'file', size: 256, isLink: false }),
    read: async () => JSON.stringify(config(state.sid)),
    get: async key => clone(saved.get(key)),
    set: async (key, value) => { state.writes.push({ key, value: clone(value) }); saved.set(key, clone(value)); },
    remove: async key => saved.delete(key), keys: async () => [...saved.keys()],
    now: async () => state.now,
    after: (ms, fn) => { const timer = { ms, fn, cancelled: false, cancel() { this.cancelled = true; } }; state.timers.push(timer); return timer; },
    status: async text => { state.status = text; },
    submit: async input => { state.prompts.push(input); client.observeTurn({ text: input.text, turnId: 'native-' + state.prompts.length }); return { text: input.text }; },
    fetch: async (url, init) => {
      const call = { url, init: clone(init), body: init.body ? JSON.parse(init.body) : undefined }; state.calls.push(call);
      if (!state.available) throw new Error('do not expose native raw error or token');
      let value = {};
      if (url.includes('/v1/endpoint?')) value = { protocol_version: 1, capabilities: { mailbox_long_poll_max_ms: 25000, mailbox_client_lease: true }, endpoint: { id: 'claude', enabled: true, driver: 'claude-code', session_id: state.sid, modes: ['queue'], idle_wake_verified: false, mailbox_protocol_version: mailbox ? 1 : 0 }, session: { state: 'idle' }, outstanding: clone(state.outstanding) };
      if (url.includes('/v1/client-lease?')) value = { lease_id: 'lease-a', ttl_ms: 60000, expires_at: '2026-10-16T00:01:00Z' };
      if (url.includes('/v1/inbox?') || url.includes('/v1/mail/inbox?')) value = null;
      return { status: 200, text: JSON.stringify(value) };
    },
  };
  client = new EndpointClient(port, '/private/client.json');
  return { state, port, client, start: async () => { await client.start(); assert.equal(client.state, 'running'); return client.generation; } };
}
const receipts = state => state.calls.filter(call => call.url.includes('/v1/delivery?')).map(call => call.body);
const reports = state => state.calls.filter(call => call.url.includes('/v1/report?')).map(call => call.body);
const mail = (id = 'mail-one', kind = 'request') => ({ message_id: id, mailbox: true, task_id: '', recipient: 'claude', sender: 'codex', delivery: 'dispatching', mode: 'queue', kind, correlation_id: 'original-correlation', root_origin: { endpoint: 'codex', device_id: 'local', session_id: 'original-human-chat', request_ref: 'human-input' }, content: 'One authorized mailbox check.' });
const mailReceipts = state => state.calls.filter(call => call.url.includes('/v1/mail/delivery?')).map(call => call.body);

test('configuration reads only the actual plugin identity and respects explicit activation options', () => {
  const settings = { pluginConfigs: {
    'agi-hub-endpoint@agi-hub-local': { options: { client_config: '/private/own.json' } },
    'another-plugin@agi-hub-local': { options: { client_config: '/private/other.json' } },
  } };
  assert.equal(clientConfigPath({ client_config: '/private/explicit.json' }, 'agi-hub-endpoint', settings), '/private/explicit.json');
  assert.equal(clientConfigPath({ client_config: '' }, 'agi-hub-endpoint', settings), '/private/own.json');
  assert.equal(clientConfigPath(undefined, 'agi-hub-endpoint@agi-hub-local', settings), '/private/own.json');
  assert.equal(clientConfigPath({}, 'agi-hub-endpoint@other', settings), undefined);
  assert.equal(clientConfigPath({}, 'unconfigured', settings), undefined);
  assert.equal(clientConfigPath({}, 'agi-hub-endpoint', {}), undefined);
  settings.pluginConfigs['agi-hub-endpoint@other'] = { options: { client_config: '/private/conflicting.json' } };
  assert.throws(() => clientConfigPath({}, 'agi-hub-endpoint', settings), /ambiguous_plugin_configuration/);
  settings.pluginConfigs['agi-hub-endpoint'] = { options: { client_config: '' } };
  assert.throws(() => clientConfigPath({}, 'agi-hub-endpoint', settings), /ambiguous_plugin_configuration/);
  delete settings.pluginConfigs['agi-hub-endpoint@other'];
  settings.pluginConfigs['agi-hub-endpoint'].options.client_config = '/private/conflicting.json';
  assert.throws(() => clientConfigPath({}, 'agi-hub-endpoint', settings), /ambiguous_plugin_configuration/);
  assert.equal(clientConfigPath({}, 'agi-hub-endpoint@agi-hub-local', settings), '/private/own.json');
  settings.pluginConfigs['agi-hub-endpoint'].options.client_config = '/private/own.json';
  assert.equal(clientConfigPath({}, 'agi-hub-endpoint', settings), '/private/own.json');
});

test('configuration rejects duplicate keys, exotic addresses and private transport overrides', () => {
  assert.throws(() => parseJSON('{"endpoint":"a","endpo\\u0069nt":"b"}'), /duplicate_json_key/);
  for (const value of ['http://localhost:7331', 'http://127.1', 'http://0177.0.0.1', 'http://127.0.0.256', 'http://127.0.0.1:0', 'http://127.0.0.1/path', 'http://user@127.0.0.1']) assert.throws(() => parseAddress(value));
  assert.equal(parseAddress('http://[::1]:7331/').origin, 'http://[::1]:7331');
  assert.equal(byteLength('hello世界😀'), 15);
  assert.equal(absolutePath('C:\\Users\\test\\client.json'), true);
  assert.equal(absolutePath('\\\\server\\share\\client.json'), false);
  const bad = config(); bad.native.tools = ['Bash']; assert.throws(() => validateConfig(bad), /invalid_claude_mod_fields/);
  assert.notEqual(scopeKey(validateConfig(config('a'))), scopeKey(validateConfig(config('b'))));
});

test('all report outcomes are encoded as native MCP text output, including a successful HTTP report', async () => {
  const { client, start } = harness(); const generation = await start(); await client.consume(envelope(), generation);
  const accepted = await client.report({ event_id: 'accepted', task_id: 'one', state: 'accepted' });
  assert.deepEqual(accepted, { ok: true });
  for (const outcome of [accepted, { ok: false, code: 'main_session_only' }, { ok: false, code: 'client_not_started' }, { ok: false, status: 403, code: 'hub_http_403' }]) {
    const encoded = reportToolResult(outcome);
    assert.equal(typeof encoded.result, 'string'); assert.deepEqual(JSON.parse(encoded.result), outcome);
  }
});

test('journal namespace uses a verified SHA-256 fingerprint and separates recreated Hubs', () => {
  for (const value of ['', 'abc', 'test-token'.repeat(100), 'a'.repeat(64), 'b'.repeat(55), 'c'.repeat(56)]) assert.equal(tokenFingerprint(value), createHash('sha256').update(value).digest('hex'));
  const first = validateConfig(config()), rotated = validateConfig({ ...config(), endpoint_token: 'another-test-only-token' });
  assert.notEqual(scopeKey(first), scopeKey(rotated)); assert.equal(scopeKey(first).includes(first.endpoint_token), false);
});

test('unbound sessions do not request a lease or consume any inbox', async () => {
  const { client, port, state } = harness(); port.read = async () => JSON.stringify(config('some-other-session'));
  await client.start(); assert.equal(client.state, 'dormant'); assert.equal(state.calls.length, 0);
});

test('binding mismatch stops before claiming; unsupported modes and malformed Envelopes are rejected', async () => {
  const { client, port, state } = harness(); const original = port.fetch;
  port.fetch = async (url, init) => { const response = await original(url, init); if (url.includes('/v1/endpoint?')) { const value = JSON.parse(response.text); value.endpoint.session_id = 'different'; response.text = JSON.stringify(value); } return response; };
  await client.start(); assert.equal(client.state, 'stopped'); assert.equal(state.calls.some(call => call.url.includes('/v1/inbox?')), false);
  assert.throws(() => validateEnvelope({ ...envelope(), mode: 'interrupt' }, 'claude'));
  assert.throws(() => validateBinding({ protocol_version: 1, endpoint: { ...config(), id: 'claude', enabled: true, driver: 'claude-code', session_id: 'session-a', modes: ['steer'] } }, validateConfig(config())));
});

test('unsupported peer bindings stop Claude before lease acquisition or inbox claim', async () => {
  for (const value of [1, 2, -1, true, '1']) {
    const { client, port, state } = harness(); const original = port.fetch;
    port.fetch = async (url, init) => {
      const response = await original(url, init);
      if (url.includes('/v1/endpoint?')) { const body = JSON.parse(response.text); body.endpoint.peer_protocol_version = value; response.text = JSON.stringify(body); }
      return response;
    };
    await client.start();
    assert.equal(client.state, 'stopped'); assert.equal(client.code, 'claude_peer_protocol_not_supported');
    assert.equal(state.calls.some(call => /\/v1\/(client-lease|inbox)\?/.test(call.url)), false);
    assert.equal(state.prompts.length, 0);
  }
  const { client, port, state } = harness(); const original = port.fetch;
  port.fetch = async (url, init) => {
    const response = await original(url, init);
    if (url.includes('/v1/endpoint?')) { const body = JSON.parse(response.text); body.endpoint.peer_while_busy_verified = true; response.text = JSON.stringify(body); }
    return response;
  };
  await client.start(); assert.equal(client.code, 'claude_peer_protocol_not_supported'); assert.equal(state.prompts.length, 0);
});

test('unsupported addressed mailbox bindings stop Claude before acquiring a lease', async () => {
  for (const [field, value, code] of [['mailbox_protocol_version', 2, 'claude_mailbox_protocol_not_supported'], ['mailbox_protocol_version', '1', 'claude_mailbox_protocol_not_supported'], ['mailbox_while_busy_verified', true, 'claude_mailbox_busy_guidance_not_supported']]) {
    const { client, port, state } = harness(); const original = port.fetch;
    port.fetch = async (url, init) => {
      const response = await original(url, init);
      if (url.includes('/v1/endpoint?')) { const body = JSON.parse(response.text); body.endpoint[field] = value; response.text = JSON.stringify(body); }
      return response;
    };
    await client.start(); assert.equal(client.state, 'stopped'); assert.equal(client.code, code);
    assert.equal(state.calls.some(call => /\/v1\/(client-lease|inbox)\?/.test(call.url)), false);
    assert.equal(state.prompts.length, 0);
  }
});

test('native marker evidence confirms delivery and the full claim is journaled before prompt submission', async () => {
  const { client, state, start, port } = harness(); const generation = await start();
  const submit = port.submit;
  port.submit = async input => { const claims = [...state.saved.values()].filter(value => value.message_id === 'task:one'); assert.ok(claims.some(value => value.state === 'claimed')); return submit(input); };
  await client.consume(envelope(), generation);
  assert.equal(state.prompts.length, 1); assert.equal(state.prompts[0].asUser, undefined);
  assert.equal(receipts(state).at(-1).outcome, 'delivered');
  assert.ok(state.writes.some(write => write.value.receipt?.outcome === 'delivered' && write.value.state === 'receipt_pending'));
  const call = state.calls.find(call => call.url.includes('/v1/delivery?')); assert.equal(call.init.headers['X-AGI-Hub-Lease'], 'lease-a');
});

test('a hook returning text without entering a native turn is uncertain, never a delivery ACK', async () => {
  const { client, port, state, start } = harness(); const generation = await start();
  port.submit = async input => { state.prompts.push(input); return { text: input.text }; };
  await client.consume(envelope(), generation);
  assert.equal(receipts(state).at(-1).outcome, 'uncertain'); assert.equal(client.state, 'awaiting_native_entry');
  await client.consume(envelope(), generation); assert.equal(state.prompts.length, 1);
});

test('a queued prompt is reconciled only when its exact marker starts a later native turn', async () => {
  const { client, port, state, start } = harness(); const generation = await start();
  port.submit = async input => { state.prompts.push(input); return { text: input.text }; };
  await client.consume(envelope(), generation);
  client.observeTurn({ text: 'unrelated user text', turnId: 'not-ours' }); assert.equal(receipts(state).at(-1).outcome, 'uncertain');
  client.observeTurn({ text: state.prompts[0].text, turnId: 'later-native-turn' });
  const callback = state.timers.findLast(timer => timer.ms === 0 && !timer.cancelled); await callback.fn(); await client.serialTail;
  assert.equal(receipts(state).at(-1).outcome, 'delivered'); assert.equal(state.prompts.length, 1); assert.equal(client.state, 'running');
});

test('the exact native plugin wrapper confirms an MQ delivery without weakening marker boundaries', async () => {
  const { client, port, state, start } = harness(); const generation = await start();
  port.submit = async input => { state.prompts.push(input); client.observeTurn({ text: nativeWrapped(input.text), turnId: 'wrapped-native-turn' }); return { text: input.text }; };
  await client.consume(envelope(), generation);
  assert.equal(receipts(state).at(-1).outcome, 'delivered');
  assert.equal(client.messages.get('task:one').native_turn_id, 'wrapped-native-turn');
  assert.equal(client.messages.get('task:one').native_message_shape, 'native-plugin-wrapper');
  assert.equal(state.prompts[0].asUser, undefined);
  const marker = client.messages.get('task:one').marker;
  assert.equal(nativeMarkerShape(marker + '\nbody', marker), 'raw-marker');
  assert.equal(nativeMarkerShape('quoted text\n' + marker + '\nbody', marker), null);
  assert.equal(nativeMarkerShape(nativeWrapped('quoted text\n' + marker + '\nbody'), marker), null);
  assert.equal(nativeMarkerShape('The agi-hub-endpoint plugin sent a message:\n' + marker + '\nbody', marker), null);
  assert.equal(nativeMarkerShape(nativeWrapped(marker + '\nbody').replace('agi-hub-endpoint', 'another-plugin'), marker), null);
});

test('drop is failed and a thrown submit is uncertain; neither prompt is retried', async () => {
  for (const behavior of ['drop', 'throw']) {
    const { client, port, state, start } = harness(); const generation = await start(); let count = 0;
    port.submit = async () => { count++; if (behavior === 'throw') throw new Error('secret raw error'); return { drop: 'raw secret reason' }; };
    await client.consume(envelope(), generation); await client.consume(envelope(), generation);
    assert.equal(count, 1); assert.equal(receipts(state).at(-1).outcome, behavior === 'drop' ? 'failed' : 'uncertain');
    assert.equal(JSON.stringify([...state.saved.values()]).includes('secret'), false);
  }
});

test('invalid envelope and oversized prompt fail without touching the native session', async () => {
  const { client, state, start } = harness(); const generation = await start();
  await client.consume({ ...envelope(), recipient: 'other' }, generation);
  await client.consume({ ...envelope('big'), content: '世'.repeat(400000) }, generation);
  assert.equal(state.prompts.length, 0); assert.deepEqual(receipts(state).map(value => value.outcome), ['failed', 'failed']);
});

test('reports require a locally received task and native turn, keep a stable run and freeze event contents', async () => {
  const { client, state, start } = harness(); const generation = await start();
  assert.equal((await client.report({ event_id: 'x', task_id: 'unknown', state: 'accepted' })).code, 'report_task_not_received');
  await client.consume(envelope(), generation);
  const accepted = { event_id: '__proto__', task_id: 'one', state: 'accepted', summary: 'Taken.' };
  assert.deepEqual(await client.report(accepted), { ok: true });
  client.currentTurn = 'later-manual-turn';
  assert.deepEqual(await client.report({ event_id: 'r', task_id: 'one', state: 'running' }), { ok: true });
  assert.deepEqual(await client.report(accepted), { ok: true });
  assert.equal((await client.report({ ...accepted, summary: 'changed' })).code, 'report_event_changed');
  assert.deepEqual(new Set(reports(state).map(report => report.run_id)), new Set(['claude-turn:native-1']));
  assert.equal(reports(state).every(report => report.endpoint === 'claude'), true);
  assert.equal((await client.report({ ...accepted, endpoint: 'codex' })).code, 'invalid_report');
});

test('result handoffs can be displayed but cannot become reportable tasks', async () => {
  const { client, state, start } = harness(); const generation = await start();
  await client.consume(envelope('result-only', 'result'), generation);
  assert.match(state.prompts[0].text, /not a new task/);
  assert.equal((await client.report({ event_id: 'x', task_id: 'result-only', state: 'accepted' })).code, 'report_task_not_received');
});

test('restart replays saved delivered, failed and uncertain receipts byte-for-byte, never submit', async () => {
  for (const outcome of ['delivered', 'failed', 'uncertain']) {
    const saved = new Map(), first = harness({ saved });
    const prefix = scopeKey(validateConfig(config()));
    saved.set(prefix + 'msg:task%3Aone', { scope: prefix, message_id: 'task:one', task_id: 'one', kind: 'task', marker: '[marker]', state: 'receipt_pending', at: 0, receipt: { outcome, detail: 'exact previous receipt' } });
    await first.client.start();
    assert.deepEqual(receipts(first.state), [{ message_id: 'task:one', outcome, detail: 'exact previous receipt' }]);
    assert.equal(first.state.prompts.length, 0);
    assert.equal(first.client.state, outcome === 'uncertain' ? 'reconcile_required' : 'running');
  }
});

test('server outstanding without local journal stops for reconciliation instead of resubmitting', async () => {
  const { client, state } = harness(); state.outstanding = [envelope()]; await client.start();
  assert.equal(client.state, 'reconcile_required'); assert.equal(state.prompts.length, 0);
  assert.equal(state.calls.some(call => call.url.includes('/v1/inbox?')), false);
});

test('a lease expiring while inbox returns must not reach prompt.submit', async () => {
  const { client, state, port, start } = harness(); await start(); const original = port.fetch;
  port.fetch = async (url, init) => { if (url.includes('/v1/inbox?')) { state.now = 61000; return { status: 200, text: JSON.stringify(envelope()) }; } return original(url, init); };
  await client.poll(client.generation); assert.equal(state.prompts.length, 0); assert.equal(client.code, 'client_lease_expired');
});

test('old claim/store continuations cannot write into a new session scope', async () => {
  const { client, state, port, start } = harness(); const generation = await start();
  const blocked = deferred(), written = deferred(), original = port.set;
  port.set = async (key, value) => { if (key.includes('msg:')) { await original(key, value); written.resolve(); await blocked.promise; } else await original(key, value); };
  const pending = client.consume(envelope(), generation); await written.promise;
  await client.end(); state.sid = 'session-b'; client.config = validateConfig(config('session-b')); client.prefix = scopeKey(client.config);
  blocked.resolve(); await assert.rejects(pending, /client_generation_changed/);
  assert.equal(state.writes.some(write => write.key.startsWith(scopeKey(validateConfig(config('session-b'))))), false);
  assert.equal(state.prompts.length, 0);
});

test('old failing callbacks cannot halt a new generation', async () => {
  const { client, state, start } = harness(); await start(); const blocked = deferred();
  client.schedule(1, async () => { await blocked.promise; throw new Error('old callback'); });
  const callback = state.timers.at(-1); callback.fn();
  await client.end(); await client.start(); blocked.resolve(); await new Promise(resolve => setImmediate(resolve));
  assert.equal(client.state, 'running');
});

test('Hub unavailable at start reconnects with backoff; a competing lease waits without claiming', async () => {
  const { client, state, port } = harness(); state.available = false; await client.start();
  assert.equal(client.state, 'reconnecting'); assert.equal(state.timers.at(-1).ms, 1000);
  state.available = true; const retry = state.timers.at(-1); await retry.fn(); await new Promise(resolve => setImmediate(resolve));
  assert.equal(client.state, 'running');
  await client.end(); const original = port.fetch;
  port.fetch = async (url, init) => url.includes('/v1/client-lease?') ? { status: 409, text: '{}' } : original(url, init);
  await client.start(); assert.equal(client.state, 'locked'); assert.equal(client.code, 'client_lease_held');
  assert.equal(state.calls.some(call => call.url.includes('/v1/inbox?')), false);
});

test('inbox network ambiguity reconnects to an outstanding record and never retries the prompt', async () => {
  const { client, state, port, start } = harness(); await start(); const original = port.fetch;
  port.fetch = async (url, init) => { if (url.includes('/v1/inbox?')) { state.outstanding = [envelope()]; throw new Error('response lost after claim'); } return original(url, init); };
  await client.poll(client.generation); assert.equal(client.state, 'reconnecting');
  const retry = state.timers.findLast(timer => !timer.cancelled && timer.ms === 1000); await retry.fn(); await new Promise(resolve => setImmediate(resolve));
  assert.equal(client.state, 'reconcile_required'); assert.equal(state.prompts.length, 0);
});

test('diagnostic schedules one prompt, preserves plugin attribution and records the observed native turn', async () => {
  const { client, state, start } = harness(); await start();
  const first = await client.diagnose(); assert.match(first.text, /30 seconds/);
  assert.match((await client.diagnose()).text, /already scheduled/);
  await state.timers.findLast(timer => timer.ms === 30000).fn(); await new Promise(resolve => setImmediate(resolve));
  assert.equal(state.prompts.length, 1); assert.equal(state.prompts[0].asUser, undefined);
  assert.equal(client.status().diagnostic.state, 'native_turn_observed');
  assert.equal(state.calls.some(call => call.url.includes('/v1/inbox?')), false);
});

test('diagnostic observes a real native turn with the host plugin wrapper and records only its shape', async () => {
  const { client, port, state, start } = harness(); await start();
  port.submit = async input => { state.prompts.push(input); client.observeTurn({ text: nativeWrapped(input.text), turnId: 'wrapped-diagnostic-turn' }); return { text: input.text }; };
  await client.diagnose(); await state.timers.findLast(timer => timer.ms === 30000).fn();
  const diagnostic = client.status().diagnostic;
  assert.equal(diagnostic.state, 'native_turn_observed'); assert.equal(diagnostic.native_turn_id, 'wrapped-diagnostic-turn');
  assert.equal(diagnostic.native_message_shape, 'native-plugin-wrapper');
  assert.equal(JSON.stringify(diagnostic).includes('Address the message'), false);
});

test('subagent turns cannot replace the main session turn or confirm a delivery marker', async () => {
  const { client, port, state, start } = harness(); const generation = await start();
  port.submit = async input => { state.prompts.push(input); return { text: input.text }; };
  await client.consume(envelope(), generation);
  client.observeTurn({ text: state.prompts[0].text, turnId: 'subagent-turn', agentId: 'subagent' });
  assert.equal(client.currentTurn, null); assert.equal(receipts(state).at(-1).outcome, 'uncertain');
});

test('run ID captures the first native turn before asynchronous journal reads', async () => {
  const { client, port, state, start } = harness(); const generation = await start(); await client.consume(envelope(), generation);
  const original = port.get;
  port.get = async key => { const value = await original(key); if (key.includes('turn:')) client.completeTurn({ turnId: 'native-1' }); return value; };
  assert.deepEqual(await client.report({ event_id: 'accepted', task_id: 'one', state: 'accepted' }), { ok: true });
  assert.equal(reports(state).at(-1).run_id, 'claude-turn:native-1');
});

test('a late response from an earlier acquisition cannot release a newer owner lease', async () => {
  const { client, port, state } = harness(); const original = port.fetch, gate = deferred(), acquired = deferred();
  let held, firstOwner;
  port.fetch = async (url, init) => {
    if (!url.includes('/v1/client-lease?')) return original(url, init);
    const body = JSON.parse(init.body); state.calls.push({ url, init: clone(init), body });
    if (body.release) { assert.equal(body.owner_id, held); held = undefined; return { status: 200, text: '{}' }; }
    if (held && held !== body.owner_id) return { status: 409, text: '{}' };
    held = body.owner_id;
    if (!firstOwner) { firstOwner = held; acquired.resolve(); await gate.promise; }
    return { status: 200, text: JSON.stringify({ lease_id: 'lease-' + body.owner_id, ttl_ms: 60000, expires_at: '2026-10-16T00:01:00Z' }) };
  };
  const oldStart = client.start(); await acquired.promise;
  await client.start(); assert.equal(client.state, 'locked'); assert.equal(client.code, 'client_lease_held');
  const secondOwner = client.owner; assert.notEqual(secondOwner, firstOwner);
  gate.resolve(); await oldStart;
  assert.equal(held, undefined); await client.start(); assert.equal(client.state, 'running');
  assert.notEqual(client.owner, firstOwner);
});

test('a stale HTTP 409 cannot stop a newer successfully bound generation', async () => {
  const { client, port, start } = harness(); const generation = await start(), original = port.fetch;
  const gate = deferred(), requested = deferred();
  port.fetch = async (url, init) => { if (url.includes('/v1/delivery?')) { requested.resolve(); await gate.promise; return { status: 409, text: '{}' }; } return original(url, init); };
  const old = client.request('/v1/delivery', 'POST', { message_id: 'old', outcome: 'uncertain', detail: 'old' }, generation).catch(error => error);
  await requested.promise; await client.end(); await client.start(); gate.resolve();
  assert.match((await old).code, /client_generation_changed/); assert.equal(client.state, 'running');
});

test('Hub restart invalidating an old lease reconnects without replaying a prompt', async () => {
  const { client, port, state, start } = harness(); await start(); const original = port.fetch; let firstRenew = true;
  port.fetch = async (url, init) => {
    if (url.includes('/v1/client-lease?') && JSON.parse(init.body).lease_id && !JSON.parse(init.body).release && firstRenew) { firstRenew = false; return { status: 409, text: '{}' }; }
    return original(url, init);
  };
  await client.poll(client.generation); assert.equal(client.state, 'reconnecting');
  await state.timers.findLast(timer => !timer.cancelled && timer.ms === 1000).fn();
  assert.equal(client.state, 'running'); assert.equal(state.prompts.length, 0);
});

test('hot reload waits for an unreleased lease to expire and automatically starts without stealing', async () => {
  const { client, port, state } = harness(); const original = port.fetch;
  let oldLeaseExpires = 60000, acquisitions = 0;
  port.fetch = async (url, init) => {
    if (url.includes('/v1/client-lease?') && !JSON.parse(init.body).lease_id) {
      acquisitions++;
      if (state.now < oldLeaseExpires) return { status: 409, text: '{}' };
    }
    return original(url, init);
  };
  await client.start(); assert.equal(client.state, 'locked'); const owner = client.owner;
  let elapsed = 0;
  for (const delay of [1000, 2000, 4000, 8000, 16000, 30000]) {
    const retry = state.timers.findLast(timer => !timer.cancelled && timer.ms === delay);
    assert.ok(retry); elapsed += delay; state.now = elapsed; await retry.fn();
    assert.equal(client.owner, owner);
    if (elapsed < oldLeaseExpires) assert.equal(client.state, 'locked');
  }
  assert.equal(client.state, 'running'); assert.equal(acquisitions, 7);
  assert.equal(state.prompts.length, 0);
  assert.equal(state.calls.some(call => call.url.includes('/v1/inbox?')), false);
  assert.equal(state.calls.some(call => call.url.includes('/v1/client-lease?') && call.body?.release), false);
});

test('a live competing consumer that keeps renewing remains locked and receives no data-plane calls', async () => {
  const { client, port, state } = harness(); const original = port.fetch;
  port.fetch = async (url, init) => url.includes('/v1/client-lease?') ? { status: 409, text: '{}' } : original(url, init);
  await client.start();
  for (const delay of [1000, 2000, 4000, 8000, 16000, 30000, 30000]) {
    state.now += delay;
    const retry = state.timers.findLast(timer => !timer.cancelled && timer.ms === delay);
    assert.ok(retry); await retry.fn(); assert.equal(client.state, 'locked');
  }
  assert.equal(state.calls.every(call => call.url.includes('/v1/endpoint?')), true);
  assert.equal(state.prompts.length, 0);
});

test('only old terminal journals in the current binding are compacted', async () => {
  const { client, state } = harness(); const prefix = scopeKey(validateConfig(config())); state.now = 31 * 86400000;
  state.saved.set(prefix + 'msg:task%3Aone', { scope: prefix, message_id: 'task:one', task_id: 'one', kind: 'task', marker: '[marker]', at: 0, state: 'receipted', receipt: { outcome: 'delivered', detail: 'old' } });
  state.saved.set(prefix + 'task:one', { task_id: 'one', message_id: 'task:one', terminal: true, run_id: 'claude-turn:old-turn' });
  state.saved.set(prefix + 'turn:old-turn', 'one'); state.saved.set('another-scope:must-remain', { receipt: { outcome: 'delivered' } });
  await client.start(); assert.equal(client.state, 'running'); assert.equal(state.saved.size, 1); assert.equal(state.saved.has('another-scope:must-remain'), true);
});

test('mailbox opt-in requires actual long-poll and lease capabilities before any claim', async () => {
  for (const capabilities of [undefined, { mailbox_long_poll_max_ms: 25000 }, { mailbox_long_poll_max_ms: 0, mailbox_client_lease: true }]) {
    const { client, port, state } = harness({ mailbox: true }); const fetch = port.fetch;
    port.fetch = async (url, init) => { const response = await fetch(url, init); if (url.includes('/v1/endpoint?')) { const body = JSON.parse(response.text); body.capabilities = capabilities; response.text = JSON.stringify(body); } return response; };
    await client.start(); assert.equal(client.code, 'hub_mailbox_mod_capabilities_required');
    assert.equal(state.calls.some(call => /client-lease|inbox/.test(call.url)), false);
  }
});

test('mailbox uses only long-poll mail inbox and native turn-confirmed delivery with lease', async () => {
  const { client, state, port, start } = harness({ mailbox: true }); const generation = await start();
  await client.poll(generation);
  assert.ok(state.calls.some(call => call.url.includes('/v1/mail/inbox?endpoint=claude&wait_ms=25000')));
  assert.equal(state.calls.some(call => call.url.includes('/v1/inbox?')), false);
  await client.consume(mail(), generation);
  assert.equal(mailReceipts(state).at(-1).outcome, 'delivered');
  const sent = state.prompts[0].text;
  assert.match(sent, /mcp__agi-hub-endpoint__mail_send/); assert.match(sent, /"id":"<new-saved-stable-id>"/);
  assert.match(sent, /root_origin/); assert.match(sent, /No Task\/Report/);
  assert.equal([...state.saved.keys()].some(key => key.includes('task:')), false);
  assert.equal(state.calls.find(call => call.url.includes('/v1/mail/delivery?')).init.headers['X-AGI-Hub-Lease'], 'lease-a');
  await client.consume(mail(), generation); assert.equal(state.prompts.length, 1);
  assert.equal((await client.report({ event_id: 'wrong', task_id: 'mail-one', state: 'accepted' })).code, 'report_task_not_received');
  assert.equal(reports(state).length, 0);
  const prior = state.prompts.length;
  await client.consume(envelope('legacy'), generation);
  assert.equal(state.prompts.length, prior); assert.equal(receipts(state).at(-1).outcome, 'failed');
});

test('silent mailbox status is durably recorded without native prompt, turn or business reply', async () => {
  const { client, state, start } = harness({ mailbox: true }); const generation = await start();
  await client.consume(mail('progress', 'status'), generation);
  assert.equal(state.prompts.length, 0); assert.equal(client.currentTurn, null);
  assert.equal(mailReceipts(state).at(-1).outcome, 'delivered');
  assert.equal(state.calls.some(call => call.url.includes('/v1/mail/send?') || call.url.includes('/v1/report?')), false);
  assert.ok(state.writes.some(write => write.value.message_id === 'progress' && write.value.state === 'receipted'));
});

test('mailbox returned prompt text is uncertain until exact native turn starts, without re-submit', async () => {
  const { client, port, state, start } = harness({ mailbox: true }); const generation = await start();
  port.submit = async input => { state.prompts.push(input); return { text: input.text }; };
  await client.consume(mail(), generation);
  assert.equal(mailReceipts(state).at(-1).outcome, 'uncertain');
  client.observeTurn({ text: nativeWrapped(state.prompts[0].text), turnId: 'queued-mail-turn' });
  await state.timers.findLast(timer => timer.ms === 0 && !timer.cancelled).fn(); await client.serialTail;
  assert.equal(mailReceipts(state).at(-1).outcome, 'delivered'); assert.equal(state.prompts.length, 1);
});

test('restart replays mailbox receipt and pauses missing native evidence; never republishes prompts', async () => {
  for (const outcome of ['delivered', 'uncertain']) {
    const saved = new Map(), { client, state } = harness({ mailbox: true, saved });
    const prefix = scopeKey(validateConfig(config()));
    saved.set(prefix + 'msg:mail-one', { scope: prefix, mailbox: true, message_id: 'mail-one', kind: 'request', marker: '[mail marker]', state: 'receipt_pending', at: 0, receipt: { outcome, detail: 'saved exact evidence' } });
    await client.start(); assert.equal(state.prompts.length, 0);
    assert.deepEqual(mailReceipts(state), [{ message_id: 'mail-one', outcome, detail: 'saved exact evidence' }]);
    assert.equal(client.state, outcome === 'delivered' ? 'running' : 'reconcile_required');
  }
  const { client, state } = harness({ mailbox: true }); state.outstanding = [mail()]; await client.start();
  assert.equal(client.state, 'reconcile_required'); assert.equal(state.prompts.length, 0);
});

test('native mailbox send fixes identity, freezes caller ID and saves payload before HTTP', async () => {
  const { client, port, state, start } = harness({ mailbox: true }); await start(); const fetch = port.fetch;
  client.observeTurn({ turnId: 'actual-human-turn', text: 'A real user request' });
  port.fetch = async (url, init) => {
    if (!url.includes('/v1/mail/send?')) return fetch(url, init);
    const body = JSON.parse(init.body); state.calls.push({ url, init: clone(init), body });
    assert.ok([...state.saved.values()].some(value => value.state === 'sending' && value.payload?.id === body.id));
    assert.equal(init.headers['X-AGI-Hub-Lease'], 'lease-a');
    return { status: 200, text: JSON.stringify({ mailbox: true, message_id: body.id, sender: 'claude', recipient: body.recipient, kind: body.kind, content: body.content, correlation_id: body.correlation_id || body.id, root_origin: { endpoint: 'claude', session_id: 'session-a' }, delivery: 'pending' }) };
  };
  const input = { id: 'stable-root', recipient: 'workbuddy', kind: 'request', content: 'A bounded check' };
  const first = await client.mailSend(input); assert.equal(first.ok, true);
  assert.equal(state.calls.find(call => call.url.includes('/v1/mail/send?')).body.origin_ref, 'claude-turn:actual-human-turn');
  client.currentTurn = 'later-turn'; assert.deepEqual(await client.mailSend(input), first);
  assert.equal((await client.mailSend({ ...input, content: 'changed' })).code, 'mail_id_content_changed');
  assert.equal((await client.mailSend({ ...input, id: undefined })).code, 'mail_id_required');
  assert.equal((await client.mailSend({ ...input, endpoint: 'codex' })).code, 'unsupported_mail_fields');
  assert.equal(state.calls.filter(call => call.url.includes('/v1/mail/send?')).length, 1);
  assert.ok(!JSON.stringify(first).includes('test-only-scoped-token'));
});

test('outbound uncertainty survives restart and does not automatically resend the saved ID', async () => {
  const saved = new Map(), first = harness({ mailbox: true, saved }); await first.start(); const fetch = first.port.fetch;
  first.port.fetch = async (url, init) => { if (url.includes('/v1/mail/send?')) { first.state.calls.push({ url, init }); throw new Error('private network detail'); } return fetch(url, init); };
  const input = { id: 'saved-reply', recipient: 'codex', kind: 'reply', content: 'Checked', correlation_id: 'original-correlation', reply_to: 'mail-one' };
  const failed = await first.client.mailSend(input); assert.equal(failed.ok, false); assert.equal(failed.message_id, input.id);
  const second = harness({ mailbox: true, saved }); await second.start();
  assert.equal((await second.client.mailSend(input)).code, 'mail_send_unconfirmed_reconcile');
  assert.equal(second.state.calls.some(call => call.url.includes('/v1/mail/send?')), false);
});

test('new-root reference captures the calling native turn before asynchronous reads', async () => {
  const { client, port, start } = harness({ mailbox: true }); await start(); const get = port.get, fetch = port.fetch;
  client.observeTurn({ turnId: 'original-tool-turn', text: 'Original human request' });
  port.get = async key => { const value = await get(key); if (key.includes('send:')) client.currentTurn = 'a-later-turn'; return value; };
  port.fetch = async (url, init) => {
    if (!url.includes('/v1/mail/send?')) return fetch(url, init);
    const body = JSON.parse(init.body); assert.equal(body.origin_ref, 'claude-turn:original-tool-turn');
    return { status: 200, text: JSON.stringify({ ...body, mailbox: true, message_id: body.id, sender: 'claude', correlation_id: body.id, root_origin: { endpoint: 'claude' }, delivery: 'pending' }) };
  };
  assert.equal((await client.mailSend({ id: 'new-root', recipient: 'codex', kind: 'request', content: 'Check' })).ok, true);
});

test('native peers and mailbox state read only scoped endpoints and encode correlation data', async () => {
  const { client, port, state, start } = harness({ mailbox: true }); await start(); const fetch = port.fetch;
  port.fetch = async (url, init) => { const response = await fetch(url, init); if (url.includes('/v1/catalog?')) response.text = '{"peers":[]}'; if (url.includes('/v1/mail/state?')) response.text = '{"messages":[]}'; return response; };
  assert.deepEqual(await client.peers({}), { ok: true, catalog: { peers: [] } });
  assert.deepEqual(await client.mailState({ correlation_id: 'reference / ? &' }), { ok: true, conversation: { messages: [] } });
  assert.equal((await client.peers({ endpoint: 'other' })).code, 'invalid_peers_input');
  assert.equal((await client.mailState({ correlation_id: 'known', endpoint: 'other' })).code, 'invalid_mail_state_input');
  const read = state.calls.find(call => call.url.includes('/v1/mail/state?'));
  assert.match(read.url, /endpoint=claude&correlation_id=reference%20%2F%20%3F%20%26/);
  assert.equal(read.init.headers['X-AGI-Hub-Lease'], 'lease-a');
});
