import { expect, mock, test } from 'claude-code/testing';

test('session ID command returns the native session ID without reading configuration', async ($, on) => {
  on('session.id', () => ({ value: 'test-session-uuid' }));
  const answer = await $.command.run({ command: 'agi-hub-id', args: '' });
  expect(answer.text).toBe('test-session-uuid');
});

test('status before session.start is dormant and contains no credentials', async ($) => {
  const answer = await $.command.run({ command: 'agi-hub-status', args: '' });
  expect(answer.text).toBe('{"state":"dormant","code":"not_started"}');
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
  on('command.register', ($, e) => ({ value: { command: e.name } }));
  on('tool.register', ($, e) => {
    expect(e.name).toBe('report'); expect(e.isDeferred).toBe(false);
    return { value: { tool: 'mcp__agi-hub-endpoint__report' } };
  });
  on('ui.status', () => ({ value: undefined }));
  on('session.start', () => ({ cwd: '/test' }));
  await $.session.start({ cwd: '/test', surface: 'terminal', isInteractive: true });
  await clock.settle();
  const answer = await $.tool.call({ tool: 'mcp__agi-hub-endpoint__report', event_id: 'e', task_id: 't', state: 'accepted' });
  expect(typeof answer.result).toBe('string');
  expect(JSON.parse(answer.result)).toEqual({ ok: false, code: 'client_generation_changed' });
});
