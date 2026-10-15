#!/usr/bin/env node
/** Result-only Codex Desktop MCP consumer. Node standard library, no npm install. */
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import https from 'node:https';
import { isIP } from 'node:net';
import { spawn } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { setTimeout as delay } from 'node:timers/promises';

export const MAX_BYTES = 1 << 20;
export const MAX_FRAME_BYTES = 4 << 20;
export const PROTOCOL_VERSION = '2025-11-25';
const TOOL = 'send_message_to_thread';
const object = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const cleanString = value => typeof value === 'string' && value.length > 0 && value.trim() === value && !/[\x00-\x1f\x7f]/.test(value);
export class ConfigurationError extends Error {}
export class McpError extends Error {}
export class HubError extends Error {
  constructor(code, status = undefined) { super(code); this.code = code; this.status = status; }
}

/** JSON.parse supplies syntax validation; the second walk rejects duplicate keys. */
export function parseJSON(text) {
  const result = JSON.parse(text);
  let offset = 0;
  const whitespace = () => { while (/\s/.test(text[offset] ?? '') && offset < text.length) offset++; };
  const string = () => {
    const start = offset++;
    while (offset < text.length) {
      if (text[offset++] === '\\') offset++;
      else if (text[offset - 1] === '"') return JSON.parse(text.slice(start, offset));
    }
    throw new ConfigurationError('invalid_json');
  };
  const value = depth => {
    if (depth > 128) throw new ConfigurationError('json_nesting_too_deep');
    whitespace();
    const open = text[offset];
    if (open === '{' || open === '[') {
      offset++; whitespace();
      const close = open === '{' ? '}' : ']';
      const keys = new Set();
      while (text[offset] !== close) {
        if (open === '{') {
          const key = string();
          if (keys.has(key)) throw new ConfigurationError('duplicate_json_key');
          keys.add(key); whitespace(); offset++; // Colon, already syntax-checked.
        }
        value(depth + 1); whitespace();
        if (text[offset] === ',') { offset++; whitespace(); } else break;
      }
      offset++;
    } else if (open === '"') string();
    else while (offset < text.length && !/[\s,}\]]/.test(text[offset])) offset++;
  };
  value(0);
  return result;
}

export function parseAddress(value) {
  if (typeof value !== 'string') throw new ConfigurationError('invalid_url');
  // Parse the original numeric authority before URL can normalize 127.1 or octal IPs.
  const match = /^(https?):\/\/(\[[0-9a-fA-F:]+\]|[0-9.]+)(?::([0-9]+))?\/?$/.exec(value);
  if (!match) throw new ConfigurationError('hub_requires_numeric_loopback_base_url');
  const host = match[2].replace(/^\[|\]$/g, '');
  const family = isIP(host);
  const ipv4 = family === 4 && host.split('.')[0] === '127';
  let parsed;
  try { parsed = new URL(value); } catch { throw new ConfigurationError('invalid_url'); }
  const ipv6 = family === 6 && parsed.hostname === '[::1]';
  if (!ipv4 && !ipv6) throw new ConfigurationError('hub_requires_numeric_loopback_base_url');
  const port = match[3] ? Number(match[3]) : (match[1] === 'https' ? 443 : 80);
  if (!Number.isInteger(port) || port < 1 || port > 65535) throw new ConfigurationError('invalid_port');
  const authority = match[2] + (match[3] ? ':' + match[3] : '');
  return { scheme: match[1], host, port, authority, origin: match[1] + '://' + authority };
}

const HOST_ENVIRONMENT = [
  'CODEX_APP_TOOLS_PIPE_PATH', 'CODEX_MCP_NODE_PATH', 'CODEX_BROWSER_USE_NODE_PATH',
  'CODEX_ELECTRON_RESOURCES_PATH', 'CODEX_CLI_PATH', 'CODEX_THREAD_ID', 'XDG_CACHE_HOME',
  'PATH', 'HOME', 'USER', 'LOGNAME', 'SHELL', 'TMPDIR', 'TMP', 'TEMP', 'LANG', 'LC_ALL', 'LC_CTYPE',
  'SYSTEMROOT', 'SystemRoot', 'WINDIR', 'USERPROFILE', 'HOMEDRIVE', 'HOMEPATH', 'APPDATA',
  'LOCALAPPDATA', 'PROGRAMFILES', 'PROGRAMFILES(X86)', 'COMMONPROGRAMFILES', 'SYSTEMDRIVE',
  'PATHEXT', 'NUMBER_OF_PROCESSORS', 'PROCESSOR_ARCHITECTURE',
];

function absoluteFile(value, code) {
  if (typeof value !== 'string' || !path.isAbsolute(value)) throw new ConfigurationError(code);
  try {
    const real = fs.realpathSync(value);
    if (!fs.statSync(real).isFile()) throw new Error();
    return real;
  } catch { throw new ConfigurationError(code); }
}

export function validateConfig(config, environment = process.env) {
  if (!object(config) || Object.keys(config).some(key => !['base_url', 'endpoint', 'endpoint_token', 'native'].includes(key))) throw new ConfigurationError('invalid_client_config_fields');
  if (!cleanString(config.endpoint) || config.endpoint.length > 128) throw new ConfigurationError('invalid_endpoint');
  if (typeof config.endpoint_token !== 'string' || !config.endpoint_token.trim() || /[^\x20-\x7e\t]/.test(config.endpoint_token)) throw new ConfigurationError('invalid_endpoint_token');
  const address = parseAddress(config.base_url);
  const native = config.native;
  if (!object(native) || native.transport !== 'desktop-mcp' || Object.keys(native).some(key => !['transport', 'command', 'args', 'cwd', 'thread_id', 'timeout_seconds'].includes(key))) throw new ConfigurationError('invalid_codex_desktop_mcp_fields');
  if (!cleanString(native.thread_id)) throw new ConfigurationError('unconfigured_codex_thread_id');
  const command = absoluteFile(native.command, 'invalid_codex_desktop_mcp_command');
  if (process.platform === 'win32') {
    if (path.extname(command).toLowerCase() !== '.exe') throw new ConfigurationError('windows_codex_desktop_mcp_requires_actual_exe');
  } else {
    if (!['launch_codex_app_tools_mcp', 'node', 'nodejs'].includes(path.basename(command))) throw new ConfigurationError('codex_desktop_mcp_requires_official_launcher_or_node');
    try { fs.accessSync(command, fs.constants.X_OK); } catch { throw new ConfigurationError('invalid_codex_desktop_mcp_command'); }
  }
  if (!Array.isArray(native.args) || native.args.length !== 1) throw new ConfigurationError('invalid_codex_desktop_mcp_server_argument');
  const script = absoluteFile(native.args[0], 'invalid_codex_desktop_mcp_server_argument');
  if (path.basename(script) !== 'server.mjs') throw new ConfigurationError('invalid_codex_desktop_mcp_server_argument');
  let cwd;
  try {
    if (typeof native.cwd !== 'string' || !path.isAbsolute(native.cwd)) throw new Error();
    cwd = fs.realpathSync(native.cwd);
    if (!fs.statSync(cwd).isDirectory() || path.dirname(script) !== cwd) throw new Error();
  } catch { throw new ConfigurationError('invalid_codex_desktop_mcp_cwd'); }
  const timeout = native.timeout_seconds ?? 30;
  if (typeof timeout !== 'number' || !Number.isFinite(timeout) || timeout < 1 || timeout > 300) throw new ConfigurationError('invalid_codex_timeout');
  if (!cleanString(environment.CODEX_THREAD_ID)) throw new ConfigurationError('codex_desktop_mcp_requires_host_thread');
  if (!cleanString(environment.CODEX_APP_TOOLS_PIPE_PATH)) throw new ConfigurationError('codex_desktop_mcp_requires_host_environment');
  const env = {};
  for (const key of HOST_ENVIRONMENT) if (typeof environment[key] === 'string') env[key] = environment[key];
  return {
    ...config, address,
    native: { ...native, command, args: [script], cwd, timeout_seconds: timeout },
    caller: environment.CODEX_THREAD_ID, environment: env,
  };
}

export function loadConfig(filename, environment = process.env) {
  try {
    if (fs.statSync(filename).size > MAX_BYTES) throw new ConfigurationError('config_too_large');
    return validateConfig(parseJSON(fs.readFileSync(filename, 'utf8')), environment);
  } catch (error) {
    if (error instanceof ConfigurationError) throw error;
    throw new ConfigurationError('cannot_read_private_config');
  }
}

export function notification(endpoint, envelope) {
  if (envelope.kind !== 'result' || envelope.recipient !== endpoint || envelope.mode !== 'queue') throw new ConfigurationError('codex_desktop_mcp_result_only');
  return 'AGI Hub result returned to the original entry. This is kind=result, not a new task. '
    + 'Present its summary and artifact references to the user. Do not repeat the task or '
    + 'send accepted/running reports for this completed task. Further work requires user direction. '
    + 'Preserve the existing authorization boundary. Complete result JSON:\n' + JSON.stringify(envelope);
}

export function validateBinding(registered, config) {
  if (!object(registered) || registered.protocol_version !== 1) throw new ConfigurationError('incompatible_hub_protocol');
  const binding = registered.endpoint;
  if (!object(binding) || binding.id !== config.endpoint) throw new ConfigurationError('hub_endpoint_mismatch');
  if (binding.enabled !== true) throw new ConfigurationError('hub_endpoint_disabled_or_invalid');
  if (!Array.isArray(binding.modes) || binding.modes.length !== 1 || binding.modes[0] !== 'queue') throw new ConfigurationError('codex_client_requires_queue_only_binding');
  if (binding.driver !== 'codex' || binding.session_id !== config.native.thread_id) throw new ConfigurationError('codex_native_binding_mismatch');
  if (typeof binding.idle_wake_verified !== 'boolean') throw new ConfigurationError('invalid_hub_wake_verification');
  if (typeof (binding.result_queue_while_busy_verified ?? false) !== 'boolean') throw new ConfigurationError('invalid_hub_busy_result_verification');
  if (!object(registered.session) || !['unknown', 'idle', 'busy', 'needs_user'].includes(registered.session.state)) throw new ConfigurationError('invalid_hub_session_state');
}

export function validateTool(result) {
  const matches = Array.isArray(result?.tools) ? result.tools.filter(tool => object(tool) && tool.name === TOOL) : [];
  if (matches.length !== 1) throw new McpError('desktop_mcp_tool_unavailable');
  const schema = matches[0].inputSchema;
  if (!object(schema) || schema.type !== 'object' || !Array.isArray(schema.required) || schema.required.length !== 2 || !schema.required.includes('threadId') || !schema.required.includes('prompt') || !object(schema.properties) || schema.properties.threadId?.type !== 'string' || schema.properties.prompt?.type !== 'string') throw new McpError('desktop_mcp_tool_schema_unverified');
}

export class MCPConnection {
  constructor(native, environment, { spawnProcess = spawn } = {}) {
    this.native = native; this.environment = environment; this.spawnProcess = spawnProcess;
    this.closed = true; this.nextId = 0; this.pending = new Map(); this.buffer = Buffer.alloc(0);
  }
  start() {
    if (!this.closed) return;
    this.closed = false;
    try {
      this.child = this.spawnProcess(this.native.command, this.native.args, {
        cwd: this.native.cwd, env: this.environment, shell: false, stdio: ['pipe', 'pipe', 'ignore'],
      });
      this.child.on('error', () => this.fail('desktop_mcp_process_unavailable'));
      this.child.on('close', () => this.fail('desktop_mcp_closed'));
      this.child.stdin.on('error', () => this.fail('desktop_mcp_write_unconfirmed'));
      this.child.stdout.on('error', () => this.fail('desktop_mcp_closed'));
      this.child.stdout.on('data', chunk => this.onData(chunk));
    } catch { this.closed = true; throw new McpError('desktop_mcp_process_unavailable'); }
  }
  onData(chunk) {
    if (this.closed) return;
    this.buffer = Buffer.concat([this.buffer, chunk]);
    while (true) {
      const end = this.buffer.indexOf(10);
      if (end < 0) {
        if (this.buffer.length > MAX_FRAME_BYTES) this.fail('invalid_desktop_mcp_frame');
        return;
      }
      if (end + 1 > MAX_FRAME_BYTES) { this.fail('invalid_desktop_mcp_frame'); return; }
      const line = this.buffer.subarray(0, end); this.buffer = this.buffer.subarray(end + 1);
      let frame;
      try { frame = parseJSON(new TextDecoder('utf-8', { fatal: true }).decode(line)); }
      catch { this.fail('invalid_desktop_mcp_frame'); return; }
      if (!object(frame) || frame.jsonrpc !== '2.0') { this.fail('invalid_desktop_mcp_frame'); return; }
      // Do not answer native sampling, approval, permission or other server requests.
      if ('method' in frame) continue;
      if (!Number.isInteger(frame.id) || !this.pending.has(frame.id)) { this.fail('unexpected_desktop_mcp_response'); return; }
      const pending = this.pending.get(frame.id); this.pending.delete(frame.id); clearTimeout(pending.timer);
      if ('error' in frame || !object(frame.result)) pending.reject(new McpError('desktop_mcp_rpc_unconfirmed'));
      else pending.resolve(frame.result);
    }
  }
  write(frame) {
    if (this.closed) throw new McpError('desktop_mcp_closed');
    const encoded = Buffer.from(JSON.stringify({ jsonrpc: '2.0', ...frame }) + '\n');
    if (encoded.length > MAX_FRAME_BYTES) throw new McpError('desktop_mcp_input_too_large');
    this.child.stdin.write(encoded, error => { if (error) this.fail('desktop_mcp_write_unconfirmed'); });
  }
  notify(method) { this.write({ method }); }
  call(method, params) {
    if (this.closed) return Promise.reject(new McpError('desktop_mcp_closed'));
    const id = ++this.nextId;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => this.fail('desktop_mcp_timeout'), this.native.timeout_seconds * 1000);
      this.pending.set(id, { resolve, reject, timer });
      try { this.write({ id, method, params }); }
      catch (error) { this.fail(error instanceof McpError ? error.message : 'desktop_mcp_write_unconfirmed'); }
    });
  }
  fail(code) {
    for (const pending of this.pending.values()) { clearTimeout(pending.timer); pending.reject(new McpError(code)); }
    this.pending.clear(); this.close();
  }
  close() {
    if (this.closed) return;
    this.closed = true;
    for (const pending of this.pending.values()) { clearTimeout(pending.timer); pending.reject(new McpError('desktop_mcp_closed')); }
    this.pending.clear(); this.buffer = Buffer.alloc(0);
    const child = this.child;
    try { child.stdin.destroy(); child.stdout.destroy(); } catch {}
    if (child && child.exitCode === null && child.signalCode === null) {
      try { child.kill(); } catch {}
      const timer = setTimeout(() => { if (child.exitCode === null && child.signalCode === null) { try { child.kill('SIGKILL'); } catch {} } }, 1000);
      timer.unref();
    }
  }
}

export class DesktopMcpAdapter {
  constructor(config, { connectionFactory = (native, environment) => new MCPConnection(native, environment) } = {}) {
    this.config = config; this.connectionFactory = connectionFactory; this.ready = false; this.rpc = null;
  }
  async preflight() {
    if (this.ready && this.rpc && !this.rpc.closed) return;
    this.close();
    this.rpc = this.connectionFactory(this.config.native, this.config.environment); this.rpc.start();
    try {
      const initialized = await this.rpc.call('initialize', {
        protocolVersion: PROTOCOL_VERSION, capabilities: {},
        clientInfo: { name: 'agi-hub', title: 'AGI Hub', version: '0.2.0' },
      });
      if (initialized.protocolVersion !== PROTOCOL_VERSION || !object(initialized.capabilities?.tools) || initialized.serverInfo?.name !== 'codex-app-tools') throw new McpError('desktop_mcp_handshake_unverified');
      this.rpc.notify('notifications/initialized');
      validateTool(await this.rpc.call('tools/list', {})); this.ready = true;
    } catch (error) { this.close(); throw error; }
  }
  async deliver(envelope) {
    let attempted = false;
    try {
      const prompt = notification(this.config.endpoint, envelope);
      const params = {
        name: TOOL, arguments: { threadId: this.config.native.thread_id, prompt },
        _meta: { threadId: this.config.caller, callId: 'agi-hub-' + randomUUID() },
      };
      if (Buffer.byteLength(JSON.stringify(params)) + 256 > MAX_FRAME_BYTES) throw new McpError('desktop_mcp_input_too_large');
      await this.preflight();
      attempted = true;
      const result = await this.rpc.call('tools/call', params);
      if (result.isError !== false || !Array.isArray(result.content) || result.content.length !== 1 || result.content[0]?.type !== 'text' || typeof result.content[0].text !== 'string') throw new McpError('desktop_mcp_delivery_unconfirmed');
      const ack = parseJSON(result.content[0].text);
      if (!object(ack) || ack.threadId !== this.config.native.thread_id) throw new McpError('desktop_mcp_target_ack_mismatch');
      return { outcome: 'delivered', detail: 'Official Codex Desktop MCP acknowledged send-now input to the bound original thread; business completion remains separate' };
    } catch {
      if (attempted) { this.close(); return { outcome: 'uncertain', detail: 'Codex Desktop MCP native input was attempted without a verified target acknowledgement; reconcile before retrying' }; }
      return { outcome: 'failed', detail: 'Codex Desktop MCP preflight or result validation failed; no native input was attempted' };
    }
  }
  close() { this.ready = false; this.rpc?.close(); this.rpc = null; }
}

export class HubAPI {
  constructor(config, { signal } = {}) {
    this.endpoint = config.endpoint; this.address = config.address ?? parseAddress(config.base_url); this.signal = signal;
    this.headers = { Authorization: 'Bearer ' + config.endpoint_token, Host: this.address.authority, Origin: this.address.origin, Connection: 'close' };
  }
  path(name) { return '/v1/' + name + '?' + new URLSearchParams({ endpoint: this.endpoint }); }
  open(method, name, body, extra = {}, timeout = 30000) {
    return new Promise((resolve, reject) => {
      const transport = this.address.scheme === 'https' ? https : http;
      const headers = { ...this.headers, ...extra };
      if (method === 'POST') headers['Content-Length'] = body?.length ?? 0;
      const request = transport.request({
        hostname: this.address.host, port: this.address.port, method, path: this.path(name), headers,
        signal: this.signal, agent: false,
      });
      request.setTimeout(timeout, () => request.destroy());
      request.on('error', () => reject(new HubError('hub_request_unconfirmed')));
      request.on('response', response => resolve({ request, response }));
      request.end(body);
    });
  }
  async jsonRequest(method, name, payload) {
    let body;
    try { body = payload === undefined ? Buffer.alloc(0) : Buffer.from(JSON.stringify(payload)); }
    catch { throw new HubError('invalid_hub_payload'); }
    if (body.length > MAX_BYTES) throw new HubError('hub_payload_too_large');
    const { request, response } = await this.open(method, name, method === 'POST' ? body : undefined, { 'Content-Type': 'application/json' });
    try {
      if (response.statusCode !== 200) throw new HubError('hub_request_rejected', response.statusCode);
      const parts = []; let size = 0;
      for await (const part of response) {
        size += part.length;
        if (size > MAX_BYTES) throw new HubError('hub_response_too_large');
        parts.push(part);
      }
      return parseJSON(new TextDecoder('utf-8', { fatal: true }).decode(Buffer.concat(parts)));
    } catch (error) {
      if (error instanceof HubError) throw error;
      throw new HubError('hub_response_unconfirmed');
    } finally { response.destroy(); request.destroy(); }
  }
  get(name) { return this.jsonRequest('GET', name); }
  post(name, payload) { return this.jsonRequest('POST', name, payload); }
  async subscribe() {
    const { request, response } = await this.open('GET', 'events', undefined, { Accept: 'text/event-stream' }, 45000);
    if (response.statusCode !== 200 || response.headers['content-type']?.split(';')[0] !== 'text/event-stream') {
      const status = response.statusCode; response.destroy(); request.destroy();
      throw new HubError('event_stream_rejected', status);
    }
    return {
      close() { response.destroy(); request.destroy(); },
      async *changes() {
        let buffer = Buffer.alloc(0), size = 0, event = 'message';
        try {
          for await (const chunk of response) {
            buffer = Buffer.concat([buffer, chunk]);
            while (true) {
              const end = buffer.indexOf(10);
              if (end < 0) { if (size + buffer.length > MAX_BYTES) throw new HubError('invalid_event_stream'); break; }
              size += end + 1;
              if (size > MAX_BYTES) throw new HubError('invalid_event_stream');
              const line = buffer.subarray(0, end).toString('utf8').replace(/\r$/, ''); buffer = buffer.subarray(end + 1);
              if (!line) { if (event === 'change') yield; event = 'message'; size = 0; }
              else if (line.startsWith('event:')) event = line.slice(6).trim();
            }
          }
          throw new HubError('event_stream_disconnected');
        } catch (error) { if (error instanceof HubError) throw error; throw new HubError('event_stream_disconnected'); }
      },
    };
  }
}

const defaultStatus = (state, code) => console.log(JSON.stringify({ state, code }));
export async function drain(api, adapter, { status = defaultStatus } = {}) {
  while (true) {
    // A closed native helper must be repaired and verified before another claim.
    await adapter.preflight();
    let envelope;
    try { envelope = await api.post('inbox'); }
    catch (error) {
      if (error instanceof HubError && error.status === 422) { status('error', 'inbox_rejected_waiting_for_event'); return; }
      throw error;
    }
    if (envelope === null) return;
    if (!object(envelope) || !cleanString(envelope.message_id) || envelope.recipient !== api.endpoint || envelope.delivery !== 'dispatching' || !['task', 'control', 'result'].includes(envelope.kind)) throw new HubError('invalid_claimed_envelope');
    let delivery;
    try { delivery = await adapter.deliver(envelope); }
    catch { delivery = { outcome: 'uncertain', detail: 'native client interrupted after claim; reconcile before retrying' }; }
    if (!['delivered', 'failed', 'uncertain'].includes(delivery?.outcome)) delivery = { outcome: 'uncertain', detail: 'native adapter returned invalid evidence; reconcile before retrying' };
    try { await api.post('delivery', { message_id: envelope.message_id, outcome: delivery.outcome, detail: delivery.detail }); }
    catch { throw new HubError('delivery_receipt_unconfirmed_reconcile_in_hub'); }
  }
}

export async function consume(config, { signal, status = defaultStatus, api = new HubAPI(config, { signal }), adapter = new DesktopMcpAdapter(config), sleep = milliseconds => delay(milliseconds, undefined, { signal }) } = {}) {
  let backoff = 1000, announced = false;
  while (!signal?.aborted) {
    const connectedAt = Date.now(); let events;
    try {
      validateBinding(await api.get('endpoint'), config);
      await adapter.preflight(); // tools/list must succeed before subscribing/claiming.
      events = await api.subscribe();
      if (!announced) { status('ready', 'subscribed_native_completion_unverified'); announced = true; }
      await drain(api, adapter, { status });
      for await (const _ of events.changes()) await drain(api, adapter, { status });
    } catch (error) {
      if (signal?.aborted) return 0;
      if (error instanceof ConfigurationError || error instanceof McpError) { status('error', error.message); return 2; }
      if (!(error instanceof HubError)) { status('error', 'client_stopped_preserve_claim_and_reconcile'); return 1; }
      status('error', error.code);
      if ([401, 403, 404].includes(error.status) || ['delivery_receipt_unconfirmed_reconcile_in_hub', 'invalid_claimed_envelope'].includes(error.code)) return 1;
      if (Date.now() - connectedAt >= 20000) backoff = 1000;
    } finally { events?.close(); adapter.close(); }
    try { await sleep(backoff); } catch { return signal?.aborted ? 0 : 1; }
    backoff = Math.min(30000, backoff * 2);
  }
  return 0;
}

export async function main(argv = process.argv.slice(2)) {
  if (argv.length !== 2 || argv[0] !== '--config' || !path.isAbsolute(argv[1])) { defaultStatus('error', 'usage_requires_absolute_config'); return 2; }
  const controller = new AbortController();
  const stop = () => controller.abort();
  process.once('SIGINT', stop); process.once('SIGTERM', stop);
  try { return await consume(loadConfig(argv[1]), { signal: controller.signal }); }
  catch (error) { defaultStatus('error', error instanceof ConfigurationError ? error.message : 'client_stopped_preserve_claim_and_reconcile'); return 2; }
  finally { process.off('SIGINT', stop); process.off('SIGTERM', stop); }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) process.exitCode = await main();
