import { EndpointClient } from './lib/client.js';
import { reportToolResult, REPORT_SPEC } from './lib/protocol.js';

export function register(on, options) {
  let client;
  on('session.start', async ($, e, next) => {
    await $.command.register({ name: 'agi-hub-id', description: 'Show this native Claude Code session UUID.' });
    await $.command.register({ name: 'agi-hub-status', description: 'Show AGI Hub binding and delivery state without credentials.' });
    await $.command.register({ name: 'agi-hub-diagnose', description: 'Schedule one native idle-wake diagnostic in 30 seconds.' });
    await $.tool.register(REPORT_SPEC);
    const port = {
      sessionID: () => $.session.id(),
      stat: path => $.fs.stat(path), read: path => $.fs.read(path),
      get: key => $.store.get(key), set: (key, value) => $.store.set(key, value),
      keys: () => $.store.keys(), remove: key => $.store.delete(key),
      now: () => $.clock.now(), after: (ms, fn) => $.clock.after(ms, fn),
      fetch: (url, init) => $.http.fetch(url, init),
      submit: input => $.prompt.submit(input), status: text => $.ui.status(text),
    };
    client = new EndpointClient(port, options?.client_config);
    const active = client;
    active.schedule(0, () => active.start());
    return next(e);
  });
  on('classic.SessionStart', async ($, e, next) => {
    // These switches do not re-fire session.start on current Claude Code.
    if (client && ['clear', 'resume', 'fork'].includes(e.source)) {
      const active = client;
      active.schedule(0, () => active.start());
    }
    return next(e);
  });
  on('session.end', async ($, e, next) => {
    // Invalidate synchronously; release is best effort within the short end-hook budget.
    if (client) client.end().catch(() => {});
    return next(e);
  });
  on('turn.start', async ($, e, next) => {
    if (client) client.observeTurn(e);
    return next(e);
  });
  on('turn.complete', async ($, e, next) => {
    if (client) client.completeTurn(e);
    return next(e);
  });
  on('command.run', { command: 'agi-hub-id' }, async ($) => ({ text: await $.session.id() }));
  on('command.run', { command: 'agi-hub-status' }, async ($) => ({ text: JSON.stringify(client?.status() ?? { state: 'dormant', code: 'not_started' }) }));
  on('command.run', { command: 'agi-hub-diagnose' }, async ($) => client ? client.diagnose() : { text: 'AGI Hub: session has not started.' });
  on('tool.call', { tool: 'mcp__agi-hub-endpoint__report' }, async ($, e) => {
    const { tool, tool_use_id, agentId, requestMeta, consent, ...input } = e;
    if (agentId !== undefined) return reportToolResult({ ok: false, code: 'main_session_only' });
    return reportToolResult(client ? await client.report(input) : { ok: false, code: 'client_not_started' });
  }).catch(async () => ({ deny: 'AGI Hub report was not confirmed.' }));
}
