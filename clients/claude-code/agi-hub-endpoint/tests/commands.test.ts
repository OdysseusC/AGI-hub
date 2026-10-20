import { expect, mock, test } from 'claude-code/testing';

test('session ID command returns the native session ID without reading configuration', async ($, on) => {
  on('session.id', () => ({ value: 'test-session-uuid' }));
  const answer = await $.command.run({ command: 'agi-hub-id', args: '' });
  expect(answer.text).toBe('test-session-uuid');
});

test('status before session.start is dormant and contains no credentials', async ($) => {
  const answer = await $.command.run({ command: 'agi-hub-status', args: '' });
  expect(JSON.parse(answer.text)).toEqual({ client_version: '0.2.2', state: 'dormant', code: 'not_started' });
});

test('a report before session.start fails without running another tool', async ($) => {
  const answer = await $.tool.call({ tool: 'mcp__agi-hub-endpoint__report', event_id: 'e', task_id: 't', state: 'accepted' });
  expect(typeof answer.result).toBe('string');
  expect(JSON.parse(answer.result)).toEqual({ ok: false, code: 'client_not_started' });
});

test('subagent reports are rejected instead of using the main session native turn', async ($) => {
  const answer = await $.tool.call({ tool: 'mcp__agi-hub-endpoint__report', agentId: 'subagent', event_id: 'e', task_id: 't', state: 'accepted' });
  expect(typeof answer.result).toBe('string');
  expect(JSON.parse(answer.result)).toEqual({ ok: false, code: 'main_session_only' });
});

test('the registered main-session report entry returns JSON text using the native tool output ABI', async ($, on) => {
  const clock = mock.clock(on);
  const registered = [];
  on('command.register', ($, e) => ({ value: { command: e.name } }));
  on('tool.register', ($, e) => {
    registered.push(e.name); expect(e.isDeferred).toBe(false);
    return { value: { tool: 'mcp__agi-hub-endpoint__' + e.name } };
  });
  on('ui.status', () => ({ value: undefined }));
  on('settings.read', () => ({ value: {} }));
  on('session.start', () => ({ cwd: '/test' }));
  await $.session.start({ cwd: '/test', surface: 'terminal', isInteractive: true });
  await clock.settle();
  expect(registered).toEqual(['report', 'mail_send', 'peers', 'mail_state']);
  const status = JSON.parse((await $.command.run({ command: 'agi-hub-status', args: '' })).text);
  expect(status.state).toBe('dormant');
  expect(status.code).toBe('unconfigured');
  const answer = await $.tool.call({ tool: 'mcp__agi-hub-endpoint__report', event_id: 'e', task_id: 't', state: 'accepted' });
  expect(typeof answer.result).toBe('string');
  expect(JSON.parse(answer.result)).toEqual({ ok: false, code: 'client_generation_changed' });
});

test('a missing activation option uses only its configured plugin path from host settings', async ($, on) => {
  const clock = mock.clock(on);
  const paths = [];
  on('command.register', ($, e) => ({ value: { command: e.name } }));
  on('tool.register', ($, e) => ({ value: { tool: 'mcp__agi-hub-endpoint__' + e.name } }));
  on('ui.status', () => ({ value: undefined }));
  on('settings.read', () => ({ value: { pluginConfigs: {
    'agi-hub-endpoint@agi-hub-local': { options: { client_config: '/private/own-client.json' } },
    'other-plugin@agi-hub-local': { options: { client_config: '/private/other-client.json' } },
  } } }));
  on('fs.stat', ($, e) => { paths.push(e.path); return { value: { kind: 'file', size: 2, isLink: false } }; });
  on('fs.read', () => ({ value: '{}' }));
  on('session.start', () => ({ cwd: '/test' }));
  await $.session.start({ cwd: '/test', surface: 'terminal', isInteractive: true });
  await clock.settle();
  expect(paths.map(path => path.replace(/\\/g, '/').replace(/^[A-Za-z]:/, ''))).toEqual(['/private/own-client.json']);
  const answer = JSON.parse((await $.command.run({ command: 'agi-hub-status', args: '' })).text);
  expect(answer.state).toBe('stopped');
  expect(answer.code).toBe('invalid_endpoint');
});

test('explicit activation configuration takes priority without reading host settings', { options: { client_config: '/private/explicit-client.json' } }, async ($, on) => {
  const clock = mock.clock(on);
  const paths = [];
  on('command.register', ($, e) => ({ value: { command: e.name } }));
  on('tool.register', ($, e) => ({ value: { tool: 'mcp__agi-hub-endpoint__' + e.name } }));
  on('ui.status', () => ({ value: undefined }));
  on('fs.stat', ($, e) => { paths.push(e.path); return { value: { kind: 'file', size: 2, isLink: false } }; });
  on('fs.read', () => ({ value: '{}' }));
  on('session.start', () => ({ cwd: '/test' }));
  await $.session.start({ cwd: '/test', surface: 'terminal', isInteractive: true });
  await clock.settle();
  expect(paths.map(path => path.replace(/\\/g, '/').replace(/^[A-Za-z]:/, ''))).toEqual(['/private/explicit-client.json']);
});

test('mailbox native tools return JSON text before binding, without executing HTTP or Bash', async ($) => {
  for (const input of [
    { tool: 'mcp__agi-hub-endpoint__mail_send', id: 'saved', recipient: 'codex', kind: 'reply', content: 'Checked', correlation_id: 'known' },
    { tool: 'mcp__agi-hub-endpoint__peers' },
    { tool: 'mcp__agi-hub-endpoint__mail_state', correlation_id: 'known' },
  ]) {
    const answer = await $.tool.call(input);
    expect(typeof answer.result).toBe('string');
    expect(JSON.parse(answer.result)).toEqual({ ok: false, code: 'client_not_started' });
  }
});

test('mailbox tools reject subagent calls and preserve main-session identity', async ($) => {
  for (const tool of ['mail_send', 'peers', 'mail_state']) {
    const answer = await $.tool.call({ tool: 'mcp__agi-hub-endpoint__' + tool, agentId: 'subagent', id: 'saved', recipient: 'codex', kind: 'reply', content: 'Checked', correlation_id: 'known' });
    expect(typeof answer.result).toBe('string');
    expect(JSON.parse(answer.result)).toEqual({ ok: false, code: 'main_session_only' });
  }
});
