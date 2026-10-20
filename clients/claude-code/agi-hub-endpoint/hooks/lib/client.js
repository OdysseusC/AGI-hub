import { absolutePath, byteLength, ClientError, cleanString, object, parseJSON, scopeKey, validateBinding, validateConfig, MAX_BYTES } from './config.js';
import { canonicalJSON, nativeMarkerShape, notification, validateEnvelope, validateMail, validateReport } from './protocol.js';

const freshID = () => 'mod-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2) + '-' + Math.random().toString(36).slice(2);
const DELIVERED = 'A matching AGI Hub marker entered the bound Claude session in a native turn.start event.';
const UNCERTAIN = 'Native entry is unconfirmed; reconcile the message marker in the bound session before retrying. No prompt will be resubmitted.';
const TERMINAL = ['succeeded', 'failed'];
export const CLIENT_VERSION = '0.2.3';

/** Host-independent state machine. Only register.js knows the mod's $ API. */
export class EndpointClient {
  constructor(port, configPath) {
    this.port = port; this.configPath = configPath; this.owner = freshID();
    this.generation = 0; this.state = 'dormant'; this.code = 'unconfigured';
    this.timers = new Set(); this.messages = new Map(); this.currentTurn = null;
    this.serialTail = Promise.resolve(); this.polling = false; this.backoff = 1000; this.mailbox = false;
  }
  serial(fn) {
    const result = this.serialTail.then(fn);
    this.serialTail = result.catch(() => {});
    return result;
  }
  alive(generation) { return this.generation === generation && this.lease !== undefined; }
  guard(generation) { if (!this.alive(generation)) throw new ClientError('client_generation_changed'); }
  schedule(ms, fn, generation = this.generation) {
    const timer = this.port.after(ms, () => {
      this.timers.delete(timer);
      if (this.generation === generation) return Promise.resolve().then(fn).catch(error => { if (this.generation === generation) this.failure(error); });
    });
    this.timers.add(timer); return timer;
  }
  halt(code) {
    this.generation++; this.state = 'stopped'; this.code = code; this.lease = undefined;
    for (const timer of this.timers) timer.cancel();
    this.timers.clear(); this.polling = false;
    Promise.resolve(this.port.status('AGI Hub: stopped · ' + code)).catch(() => {});
  }
  failure(error) {
    const retryOwner = this.owner;
    const locked = error instanceof ClientError && error.status === 409 && error.acquireLease;
    const retryable = locked || error instanceof ClientError && (error.code === 'hub_request_unconfirmed' || error.code === 'invalid_hub_response' || error.status >= 500 || error.status === 409 && error.leaseRequest);
    this.halt(error instanceof ClientError ? error.code : 'callback_failed');
    if (retryable) {
      this.state = locked ? 'locked' : 'reconnecting';
      if (locked) { this.code = 'client_lease_held'; Promise.resolve(this.port.status('AGI Hub: locked · waiting for the existing consumer lease')).catch(() => {}); }
      const delay = this.backoff; this.backoff = Math.min(30000, this.backoff * 2);
      this.schedule(delay, () => this.start(retryOwner));
    }
  }
  async pause(state, code) {
    this.state = state; this.code = code;
    await this.port.status('AGI Hub: ' + state + ' · ' + code);
  }
  status() {
    return { client_version: CLIENT_VERSION, state: this.state, code: this.code, last_message_id: this.lastMessage ?? null, native_turn_id: this.currentTurn, diagnostic: this.diagnostic ? { state: this.diagnostic.state, nonce: this.diagnostic.nonce, native_turn_id: this.diagnostic.turnId ?? null, native_message_shape: this.diagnostic.shape ?? null } : null };
  }
  async raw(config, path, method, body, lease, query = {}) {
    const headers = { Authorization: 'Bearer ' + config.endpoint_token, Origin: config.address.origin, 'Content-Type': 'application/json' };
    if (lease) headers['X-AGI-Hub-Lease'] = lease;
    let response;
    const suffix = Object.entries(query).map(([key, value]) => '&' + encodeURIComponent(key) + '=' + encodeURIComponent(value)).join('');
    try { response = await this.port.fetch(config.address.origin + path + '?endpoint=' + encodeURIComponent(config.endpoint) + (['/v1/inbox', '/v1/mail/inbox'].includes(path) ? '&wait_ms=' + config.native.long_poll_ms : '') + suffix, { method, headers, ...(body === undefined ? {} : { body: JSON.stringify(body) }) }); }
    catch { throw new ClientError('hub_request_unconfirmed'); }
    if (!object(response) || !Number.isInteger(response.status)) throw new ClientError('invalid_hub_response');
    if (response.status < 200 || response.status >= 300) throw new ClientError('hub_http_' + response.status, response.status);
    try { return parseJSON(response.text); } catch { throw new ClientError('invalid_hub_response'); }
  }
  async request(path, method, body, generation, query) {
    if (!this.alive(generation)) throw new ClientError('client_generation_changed');
    const config = this.config, lease = this.lease;
    try {
      const result = await this.raw(config, path, method, body, lease, query);
      this.guard(generation); return result;
    } catch (error) { this.guard(generation); if (error.status === 409) error.leaseRequest = true; throw error; }
  }
  async start(retryOwner) {
    const generation = this.generation + 1;
    // Explicit rebinding gets a new owner; a transport retry keeps its owner so
    // an acquire whose response was lost can be recovered idempotently.
    const owner = retryOwner || freshID();
    await this.end();
    if (this.generation !== generation) return;
    this.owner = owner;
    this.state = 'dormant'; this.code = 'unbound'; this.messages = new Map(); this.currentTurn = null;
    this.serialTail = Promise.resolve();
    try {
      if (this.configPath === undefined || this.configPath === '') { this.code = 'unconfigured'; await this.port.status('AGI Hub: dormant (unconfigured)'); return; }
      if (!absolutePath(this.configPath)) throw new ClientError('private_config_absolute_path_required');
      const stat = await this.port.stat(this.configPath);
      if (!object(stat) || stat.kind !== 'file' || stat.isLink || !Number.isInteger(stat.size) || stat.size > MAX_BYTES) throw new ClientError('invalid_private_config_file');
      // The mod API has no permission bits. The exporter must create this file privately.
      const text = await this.port.read(this.configPath);
      if (byteLength(text) > MAX_BYTES) throw new ClientError('config_too_large');
      const config = validateConfig(parseJSON(text));
      if (this.generation !== generation) return;
      const sessionID = await this.port.sessionID();
      if (this.generation !== generation) return;
      if (sessionID !== config.native.session_id) { this.code = 'unbound'; await this.port.status('AGI Hub: dormant (unbound)'); return; }
      this.config = config; this.prefix = scopeKey(config);
      const registered = validateBinding(await this.raw(config, '/v1/endpoint', 'GET'), config);
      if (this.generation !== generation) return;
      this.mailbox = registered.endpoint.mailbox_protocol_version === 1;
      const startedAt = await this.port.now();
      let lease;
      try { lease = await this.raw(config, '/v1/client-lease', 'POST', { owner_id: owner }); }
      catch (error) { if (error.status === 409) error.acquireLease = true; throw error; }
      if (this.generation !== generation) {
        if (cleanString(lease?.lease_id)) await this.raw(config, '/v1/client-lease', 'POST', { owner_id: owner, lease_id: lease.lease_id, release: true }).catch(() => {});
        return;
      }
      this.takeLease(lease, startedAt);
      if (await this.port.now() >= this.leaseDeadline) throw new ClientError('client_lease_expired');
      this.scheduleRenew(generation);
      const current = validateBinding(await this.request('/v1/endpoint', 'GET', undefined, generation), config);
      if ((current.endpoint.mailbox_protocol_version === 1) !== this.mailbox) throw new ClientError('mailbox_binding_changed_rebind_required');
      const ready = await this.recover(current.outstanding, generation);
      if (!this.alive(generation)) return;
      if (!ready) { await this.pause('reconcile_required', 'outstanding_native_entry_unconfirmed'); return; }
      await this.pruneJournal(generation);
      this.state = 'running'; this.backoff = 1000; this.code = current.session.state === 'unknown' ? 'confirm_hub_session_idle' : 'listening';
      await this.port.status('AGI Hub: bound · ' + this.code);
      this.schedulePoll(0, generation);
    } catch (error) {
      if (this.generation !== generation) return;
      this.failure(error instanceof ClientError ? error : new ClientError('client_start_failed'));
    }
  }
  takeLease(value, startedAt) {
    if (!object(value) || !cleanString(value.lease_id) || value.ttl_ms !== 60000 || typeof value.expires_at !== 'string') throw new ClientError('invalid_hub_lease');
    if (this.lease && this.lease !== value.lease_id) throw new ClientError('client_lease_changed');
    this.lease = value.lease_id; this.leaseDeadline = startedAt + value.ttl_ms;
  }
  async renew(generation) {
    if (this.renewing?.generation === generation) { await this.renewing.promise; this.guard(generation); return; }
    const operation = async () => {
      this.guard(generation);
      const sessionID = this.config.native.session_id;
      if (await this.port.sessionID() !== sessionID) throw new ClientError('native_session_changed');
      this.guard(generation);
      const startedAt = await this.port.now();
      this.guard(generation);
      if (startedAt >= this.leaseDeadline) throw new ClientError('client_lease_expired');
      const value = await this.request('/v1/client-lease', 'POST', { owner_id: this.owner, lease_id: this.lease }, generation);
      this.takeLease(value, startedAt);
      if (await this.port.now() >= this.leaseDeadline) throw new ClientError('client_lease_expired');
    };
    const promise = operation(); this.renewing = { generation, promise };
    try { await promise; } finally { if (this.renewing?.promise === promise) this.renewing = undefined; }
  }
  scheduleRenew(generation) {
    this.schedule(15000, async () => {
      if (!this.alive(generation)) return;
      try { await this.renew(generation); if (this.alive(generation)) this.scheduleRenew(generation); }
      catch (error) { if (this.generation === generation) this.failure(error instanceof ClientError ? error : new ClientError('lease_renewal_unconfirmed')); }
    }, generation);
  }
  async end() {
    const config = this.config, lease = this.lease, owner = this.owner;
    this.generation++; this.lease = undefined; this.state = 'dormant'; this.code = 'session_ended';
    for (const timer of this.timers) timer.cancel();
    this.timers.clear(); this.polling = false; this.currentTurn = null;
    if (config && lease) await this.raw(config, '/v1/client-lease', 'POST', { owner_id: owner, lease_id: lease, release: true }).catch(() => {});
  }
  async saveMessage(record, generation) { this.guard(generation); await this.port.set(record.scope + 'msg:' + encodeURIComponent(record.message_id), record); }
  taskKey(taskID, prefix = this.prefix) { return prefix + 'task:' + encodeURIComponent(taskID); }
  async recover(outstanding, generation) {
    let ready = true; const prefix = this.prefix, messages = this.messages;
    const outstandingIDs = new Set(outstanding.map(envelope => envelope?.message_id));
    const keys = await this.port.keys();
    this.guard(generation);
    for (const key of keys.filter(key => key.startsWith(prefix + 'msg:'))) {
      const record = await this.port.get(key);
      this.guard(generation);
      if (!object(record) || !cleanString(record.message_id)) throw new ClientError('invalid_client_journal');
      record.scope = prefix;
      messages.set(record.message_id, record);
      if (!record.receipt) {
        record.receipt = { outcome: 'uncertain', detail: UNCERTAIN }; record.state = 'receipt_pending';
        await this.saveMessage(record, generation);
      }
      if (record.state !== 'receipted' || outstandingIDs.has(record.message_id)) await this.postReceipt(record, generation);
    }
    for (const envelope of outstanding) {
      if (!object(envelope) || envelope.recipient !== this.config.endpoint || !['dispatching', 'uncertain'].includes(envelope.delivery) || !cleanString(envelope.message_id)) throw new ClientError('invalid_outstanding_envelope');
      const record = messages.get(envelope.message_id);
      if (!record || record.receipt?.outcome !== 'delivered') ready = false;
    }
    if ([...messages.values()].some(record => record.receipt?.outcome === 'uncertain')) ready = false;
    return ready;
  }
  async pruneJournal(generation) {
    const cutoff = (await this.port.now()) - 30 * 86400000, prefix = this.prefix;
    this.guard(generation);
    for (const record of [...this.messages.values()]) {
      if (record.state !== 'receipted' || !['delivered', 'failed'].includes(record.receipt?.outcome) || record.at >= cutoff) continue;
      const taskKey = this.taskKey(record.task_id, prefix), task = record.kind === 'task' ? await this.port.get(taskKey) : undefined;
      this.guard(generation);
      if (task && !task.terminal && record.receipt.outcome !== 'failed') continue;
      if (task?.run_id?.startsWith('claude-turn:')) { this.guard(generation); await this.port.remove(prefix + 'turn:' + encodeURIComponent(task.run_id.slice('claude-turn:'.length))); }
      if (task) { this.guard(generation); await this.port.remove(taskKey); }
      this.guard(generation); await this.port.remove(prefix + 'msg:' + encodeURIComponent(record.message_id));
      this.guard(generation); this.messages.delete(record.message_id);
    }
    for (const key of (await this.port.keys()).filter(key => key.startsWith(prefix + 'send:'))) {
      this.guard(generation);
      const record = await this.port.get(key);
      this.guard(generation);
      if (record?.state === 'sent' && Number.isFinite(record.at) && record.at < cutoff) await this.port.remove(key);
    }
  }
  schedulePoll(ms, generation) {
    if (this.state !== 'running' || !this.alive(generation)) return;
    this.schedule(ms, () => this.poll(generation), generation);
  }
  async poll(generation) {
    if (this.polling || this.state !== 'running' || !this.alive(generation)) return;
    this.polling = true;
    try {
      await this.renew(generation);
      const registered = validateBinding(await this.request('/v1/endpoint', 'GET', undefined, generation), this.config);
      if ((registered.endpoint.mailbox_protocol_version === 1) !== this.mailbox) throw new ClientError('mailbox_binding_changed_rebind_required');
      const envelope = await this.request(this.mailbox ? '/v1/mail/inbox' : '/v1/inbox', 'POST', {}, generation);
      if (envelope === null) { this.backoff = 1000; return; }
      this.lastMessage = cleanString(envelope?.message_id) ? envelope.message_id : null;
      await this.consume(envelope, generation);
    } catch (error) {
      if (this.generation !== generation) return;
      if (error instanceof ClientError && error.status === 422) {
        this.backoff = 5000; this.code = 'hub_not_dispatchable';
      } else {
        this.failure(error instanceof ClientError ? error : new ClientError('inbox_claim_unconfirmed'));
      }
    } finally {
      if (this.generation === generation) {
        this.polling = false;
        this.schedulePoll(this.code === 'hub_not_dispatchable' ? this.backoff : 0, generation);
      }
    }
  }
  async consume(envelope, generation) {
    this.guard(generation);
    const prefix = this.prefix, config = this.config;
    if (!object(envelope) || !cleanString(envelope.message_id)) throw new ClientError('invalid_claim_response_reconcile_in_hub');
    const existing = this.messages.get(envelope.message_id);
    if (existing) {
      // A duplicate claim never calls submit a second time.
      if (!existing.receipt) { existing.receipt = { outcome: 'uncertain', detail: UNCERTAIN }; existing.state = 'receipt_pending'; await this.saveMessage(existing, generation); }
      await this.postReceipt(existing, generation);
      if (existing.receipt.outcome === 'uncertain') await this.pause('reconcile_required', 'duplicate_claim');
      return;
    }
    const at = await this.port.now(); this.guard(generation);
    const record = { scope: prefix, message_id: envelope.message_id, task_id: envelope.task_id ?? '', kind: envelope.kind ?? '', mailbox: envelope.mailbox === true, marker: `[AGI Hub message_id=${encodeURIComponent(envelope.message_id)} nonce=${freshID()}]`, state: 'claimed', at };
    this.messages.set(record.message_id, record);
    await this.serial(() => this.saveMessage(record, generation));
    this.guard(generation);
    try { validateEnvelope(envelope, config.endpoint, this.mailbox); }
    catch { await this.receipt(record, 'failed', 'Invalid Envelope for the bound Claude endpoint; no prompt submitted.', generation); return; }
    if (record.mailbox && envelope.kind === 'status') {
      await this.receipt(record, 'delivered', 'Addressed status recorded without native input or a model wake.', generation);
      return;
    }
    const text = notification(envelope, record.marker);
    if (byteLength(text) > config.native.max_prompt_bytes) { await this.receipt(record, 'failed', 'Claude prompt exceeds the configured byte limit; no prompt submitted.', generation); return; }
    if (envelope.kind === 'task') {
      await this.serial(async () => {
        this.guard(generation);
        const key = this.taskKey(envelope.task_id, prefix), task = await this.port.get(key);
        this.guard(generation);
        if (task && task.message_id !== envelope.message_id) throw new ClientError('duplicate_task_message');
        if (!task) await this.port.set(key, { task_id: envelope.task_id, message_id: envelope.message_id, kind: 'task', events: {}, terminal: false, at: record.at });
      });
    }
    // A long poll may outlive the lease or a session switch: renew again at the side effect boundary.
    await this.renew(generation);
    if (await this.port.sessionID() !== config.native.session_id) throw new ClientError('native_session_or_lease_changed_before_submit');
    this.guard(generation);
    const submitAt = await this.port.now(); this.guard(generation);
    if (submitAt >= this.leaseDeadline) throw new ClientError('client_lease_expired');
    let result;
    try { result = await this.port.submit({ text }); }
    catch {
      if (!this.alive(generation)) return;
      if (record.native_turn_id) { await this.receipt(record, 'delivered', DELIVERED, generation); return; }
      await this.receipt(record, 'uncertain', UNCERTAIN, generation);
      if (this.alive(generation)) await this.pause('awaiting_native_entry', 'submit_unconfirmed'); return;
    }
    if (!this.alive(generation)) return;
    if (record.native_turn_id) { await this.receipt(record, 'delivered', DELIVERED, generation); return; }
    if (typeof result?.drop === 'string') {
      // Keep raw hook errors out of the journal, UI and transcript-facing returns.
      await this.receipt(record, 'failed', 'Claude prompt was dropped by a hook; no native entry observed.', generation); return;
    }
    await this.receipt(record, 'uncertain', UNCERTAIN, generation);
    if (this.alive(generation)) await this.pause('awaiting_native_entry', 'waiting_for_native_turn_marker');
  }
  async postReceipt(record, generation) {
    await this.request(record.mailbox ? '/v1/mail/delivery' : '/v1/delivery', 'POST', { message_id: record.message_id, ...record.receipt }, generation);
    if (!this.alive(generation)) return;
    record.state = 'receipted'; await this.saveMessage(record, generation);
  }
  async receipt(record, outcome, detail, generation) {
    return this.serial(async () => {
      if (!this.alive(generation)) return;
      if (record.native_turn_id && outcome === 'uncertain') outcome = 'delivered', detail = DELIVERED;
      record.receipt = { outcome, detail }; record.state = 'receipt_pending';
      await this.saveMessage(record, generation);
      try { await this.postReceipt(record, generation); }
      catch (error) {
        if (!this.alive(generation)) return;
        if (error.status === 409 || error.code === 'hub_request_unconfirmed' || error.status >= 500) this.failure(error);
        else await this.pause('receipt_unconfirmed', 'replay_saved_receipt_after_rebind');
      }
    });
  }
  /** Called synchronously by turn.start, before allowing the native turn to proceed. */
  observeTurn(event) {
    if (event?.agentId !== undefined) return;
    if (!cleanString(event?.turnId)) return;
    this.currentTurn = event.turnId;
    if (typeof event.text !== 'string') return;
    if (this.diagnostic && nativeMarkerShape(event.text, this.diagnostic.marker)) {
      this.diagnostic.state = 'native_turn_observed'; this.diagnostic.turnId = event.turnId;
      this.diagnostic.shape = nativeMarkerShape(event.text, this.diagnostic.marker);
    }
    if (!this.lease) return;
    const record = [...this.messages.values()].find(item => nativeMarkerShape(event.text, item.marker));
    if (!record) return;
    if (record.native_turn_id && record.native_turn_id !== event.turnId) { this.halt('duplicate_native_turn_marker'); return; }
    record.native_turn_id = event.turnId;
    record.native_message_shape = nativeMarkerShape(event.text, record.marker);
    const generation = this.generation;
    this.schedule(0, async () => {
      await this.receipt(record, 'delivered', DELIVERED, generation);
      if (this.alive(generation) && this.state === 'awaiting_native_entry') {
        this.state = 'running'; this.code = 'listening'; this.schedulePoll(0, generation);
      }
    }, generation);
  }
  completeTurn(event) { if (event?.agentId === undefined && event?.turnId === this.currentTurn) this.currentTurn = null; }
  toolError(error) {
    return { ok: false, ...(Number.isInteger(error?.status) ? { status: error.status } : {}), code: error instanceof ClientError ? error.code : 'mail_tool_unconfirmed' };
  }
  async requireMailbox(generation) {
    if (!this.mailbox) throw new ClientError('mailbox_not_enabled');
    await this.renew(generation);
    const current = validateBinding(await this.request('/v1/endpoint', 'GET', undefined, generation), this.config);
    if (current.endpoint.mailbox_protocol_version !== 1) throw new ClientError('mailbox_binding_changed_rebind_required');
  }
  async peers(input) {
    const generation = this.generation;
    try {
      if (!object(input) || Object.keys(input).length) throw new ClientError('invalid_peers_input');
      await this.requireMailbox(generation);
      return { ok: true, catalog: await this.request('/v1/catalog', 'GET', undefined, generation) };
    } catch (error) { return this.toolError(error); }
  }
  async mailState(input) {
    const generation = this.generation;
    try {
      if (!object(input) || Object.keys(input).some(key => key !== 'correlation_id') || !cleanString(input.correlation_id) || input.correlation_id.length > 128) throw new ClientError('invalid_mail_state_input');
      await this.requireMailbox(generation);
      return { ok: true, conversation: await this.request('/v1/mail/state', 'GET', undefined, generation, { correlation_id: input.correlation_id }) };
    } catch (error) { return this.toolError(error); }
  }
  async mailSend(input) {
    const generation = this.generation, originTurn = this.currentTurn;
    try {
      validateMail(input);
      const payload = parseJSON(JSON.stringify(input));
      await this.requireMailbox(generation);
      return await this.serial(async () => {
        this.guard(generation);
        const key = this.prefix + 'send:' + encodeURIComponent(payload.id), saved = await this.port.get(key);
        this.guard(generation);
        if (!payload.correlation_id && !payload.origin_ref) {
          payload.origin_ref = saved?.payload?.origin_ref;
          if (!payload.origin_ref) {
            if (!cleanString(originTurn)) throw new ClientError('native_origin_turn_unconfirmed');
            payload.origin_ref = 'claude-turn:' + originTurn;
          }
        }
        if (saved) {
          if (!object(saved.payload) || canonicalJSON(saved.payload) !== canonicalJSON(payload)) throw new ClientError('mail_id_content_changed');
          if (saved.state === 'sent' && !object(saved.message)) throw new ClientError('invalid_mail_send_journal');
          return saved.state === 'sent' ? { ok: true, message: saved.message } : { ok: false, code: 'mail_send_unconfirmed_reconcile', message_id: payload.id, correlation_id: payload.correlation_id || payload.id };
        }
        const pending = { payload, state: 'sending', at: await this.port.now() };
        this.guard(generation);
        await this.port.set(key, pending);
        this.guard(generation);
        let result;
        try {
          result = await this.request('/v1/mail/send', 'POST', payload, generation);
          if (!object(result) || result.mailbox !== true || result.sender !== this.config.endpoint || result.recipient !== payload.recipient || result.kind !== payload.kind || result.content !== payload.content || !object(result.root_origin)) throw new ClientError('mail_send_ack_unconfirmed');
          const remote = /^[a-f0-9]{64}\/[a-zA-Z0-9_.-]{1,100}$/.test(payload.recipient);
          // Only qualified LAN sends may replace the caller's stable ID. The
          // original ID remains the journal/retry key, never the canonical ID.
          const expectedCorrelation = remote && (!payload.correlation_id || payload.correlation_id === payload.id)
            ? result.message_id : (payload.correlation_id || payload.id);
          const matchingID = remote
            ? result.source_message_id === payload.id && typeof result.message_id === 'string' && /^lan-[a-f0-9]{64}$/.test(result.message_id)
            : result.message_id === payload.id;
          if (!matchingID || result.correlation_id !== expectedCorrelation) throw new ClientError('mail_send_ack_unconfirmed');
        } catch (error) {
          this.guard(generation);
          pending.state = 'uncertain'; await this.port.set(key, pending);
          return { ...this.toolError(error), message_id: payload.id, correlation_id: payload.correlation_id || payload.id };
        }
        const { message_id, correlation_id, sender, recipient, kind, root_origin, delivery } = result;
        const message = { message_id, correlation_id, sender, recipient, kind, root_origin, delivery };
        this.guard(generation);
        await this.port.set(key, { ...pending, state: 'sent', message });
        return { ok: true, message };
      });
    } catch (error) { return this.toolError(error); }
  }
  async report(input) {
    const generation = this.generation;
    try {
      validateReport(input);
      await this.renew(generation);
      return await this.serial(async () => {
        if (!this.alive(generation)) throw new ClientError('client_generation_changed');
        const prefix = this.prefix, key = this.taskKey(input.task_id, prefix), task = await this.port.get(key);
        this.guard(generation);
        const record = task && this.messages.get(task.message_id);
        if (!object(task) || task.kind !== 'task' || !record || record.kind !== 'task') throw new ClientError('report_task_not_received');
        if (!task.run_id) {
          const turnId = this.currentTurn;
          if (!cleanString(turnId) || record.native_turn_id !== turnId) throw new ClientError('native_task_turn_unconfirmed');
          const turnKey = prefix + 'turn:' + encodeURIComponent(turnId);
          const prior = await this.port.get(turnKey);
          this.guard(generation);
          if (prior && prior !== input.task_id) throw new ClientError('native_turn_already_bound_to_task');
          await this.port.set(turnKey, input.task_id);
          this.guard(generation);
          task.run_id = 'claude-turn:' + turnId;
        }
        const report = { ...input, endpoint: this.config.endpoint, run_id: task.run_id };
        if (!object(task.events)) throw new ClientError('invalid_report_journal');
        const eventKey = 'event:' + input.event_id, saved = task.events[eventKey];
        if (saved && canonicalJSON(saved) !== canonicalJSON(report)) throw new ClientError('report_event_changed');
        task.events[eventKey] = report;
        // Persist the complete stable Report before its first transmission.
        this.guard(generation);
        await this.port.set(key, task);
        await this.request('/v1/report', 'POST', saved || report, generation);
        if (TERMINAL.includes(input.state)) { task.terminal = true; task.terminal_at = await this.port.now(); this.guard(generation); await this.port.set(key, task); }
        return { ok: true };
      });
    } catch (error) {
      if (!this.alive(generation)) return { ok: false, code: 'client_generation_changed' };
      if (error.status === 409 || error.code === 'hub_request_unconfirmed' || error.status >= 500) this.failure(error);
      return { ok: false, ...(Number.isInteger(error.status) ? { status: error.status } : {}), code: error instanceof ClientError ? error.code : 'report_unconfirmed' };
    }
  }
  async diagnose() {
    if (!this.lease || !['running', 'awaiting_native_entry'].includes(this.state)) return { text: 'AGI Hub: bind this session and acquire its lease first.' };
    if (this.diagnostic?.state === 'scheduled') return { text: 'AGI Hub: one idle-wake diagnostic is already scheduled.' };
    const nonce = freshID(), generation = this.generation;
    this.diagnostic = { nonce, marker: `[AGI Hub idle-wake nonce=${nonce}]`, state: 'scheduled' };
    this.schedule(30000, async () => {
      await this.renew(generation);
      if (!this.alive(generation)) return;
      const diagnostic = this.diagnostic;
      diagnostic.state = 'submitted_unconfirmed';
      const result = await this.port.submit({ text: `${diagnostic.marker}\nAGI Hub idle-wake diagnostic. Reply with this nonce only: ${nonce}. Do not call tools.` });
      if (this.alive(generation) && result?.drop) diagnostic.state = 'dropped';
    }, generation);
    return { text: 'AGI Hub: one diagnostic is scheduled in 30 seconds. Leave this session idle. Nonce: ' + nonce };
  }
}
