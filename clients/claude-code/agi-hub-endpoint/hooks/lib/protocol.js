import { byteLength, ClientError, cleanString, MAX_BYTES, object } from './config.js';

export const REPORT_TOOL = 'mcp__agi-hub-endpoint__report';
export const MAIL_TOOL = 'mcp__agi-hub-endpoint__mail_send';
// Native registered tools use the MCP text/content-block output ABI. A plain
// object inside `result` is rejected by the host after the HTTP report ran.
export const reportToolResult = value => ({ result: JSON.stringify(value) });
const NATIVE_PLUGIN_PREFIX = 'The agi-hub-endpoint plugin sent a message:\n';
const NATIVE_PLUGIN_SUFFIX = '\n\nThis is how Claude Code surfaces a prompt a plugin submits between turns — it starts this turn in the user\'s place. Address the message above.';

/** Known native 2.1.299 framing, not a substring search through arbitrary text. */
export function nativeMarkerShape(text, marker) {
  if (typeof text !== 'string') return null;
  if (text.startsWith(marker + '\n')) return 'raw-marker';
  if (text.startsWith(NATIVE_PLUGIN_PREFIX + marker + '\n') && text.endsWith(NATIVE_PLUGIN_SUFFIX)) return 'native-plugin-wrapper';
  return null;
}

export const REPORT_SPEC = {
  name: 'report',
  isDeferred: false,
  description: 'Actively report an AGI Hub task this native session received. Send accepted, running, needs_user, failed, or succeeded after verifying the result and required artifacts. The tool supplies endpoint and the native execution identifier; it cannot expand user authorization.',
  inputSchema: {
    type: 'object', additionalProperties: false,
    properties: {
      event_id: { type: 'string' }, task_id: { type: 'string' },
      state: { type: 'string', enum: ['accepted', 'running', 'needs_user', 'succeeded', 'failed'] },
      summary: { type: 'string' },
      artifacts: { type: 'array', items: { type: 'object', additionalProperties: false,
        properties: { device_id: { type: 'string' }, workspace_id: { type: 'string' }, path: { type: 'string' }, sha256: { type: 'string' } },
        required: ['device_id', 'workspace_id', 'path'] } },
    }, required: ['event_id', 'task_id', 'state'],
  },
};

export const MAIL_SEND_SPEC = {
  name: 'mail_send', isDeferred: false,
  description: 'Explicitly publish one addressed AGI Hub message. Save a new stable id before sending. Choose an allowed recipient and retain correlation_id for existing collaboration. The native client fixes endpoint and lease; publish success is not business completion. Never expands human authorization or creates automatic replies.',
  inputSchema: { type: 'object', additionalProperties: false, properties: {
    id: { type: 'string' }, recipient: { type: 'string' },
    kind: { type: 'string', enum: ['request', 'reply', 'status', 'result'] }, content: { type: 'string' },
    correlation_id: { type: 'string' }, origin_ref: { type: 'string' }, reply_to: { type: 'string' },
    deadline: { type: 'string' }, max_messages: { type: 'integer', minimum: 1, maximum: 64 },
  }, required: ['id', 'recipient', 'kind', 'content'] },
};
export const PEERS_SPEC = {
  name: 'peers', isDeferred: false, description: 'Read the configured, scoped AGI Hub member directory; visibility is not permission to send.',
  inputSchema: { type: 'object', additionalProperties: false, properties: {} },
};
export const MAIL_STATE_SPEC = {
  name: 'mail_state', isDeferred: false, description: 'Read own message delivery metadata and own saved status content for one known AGI Hub correlation. Does not read other participants\' private messages or wake a model.',
  inputSchema: { type: 'object', additionalProperties: false, properties: { correlation_id: { type: 'string' } }, required: ['correlation_id'] },
};

export function validateEnvelope(envelope, endpoint, mailbox = false) {
  if (!object(envelope) || envelope.recipient !== endpoint || envelope.delivery !== 'dispatching' || envelope.mode !== 'queue' || !cleanString(envelope.message_id) || typeof envelope.content !== 'string' || !envelope.content.trim()) throw new ClientError('invalid_claude_envelope');
  if (mailbox ? envelope.mailbox !== true || !['request', 'reply', 'status', 'result'].includes(envelope.kind) || envelope.task_id || !cleanString(envelope.correlation_id) || !object(envelope.root_origin) : envelope.mailbox === true || !['task', 'result'].includes(envelope.kind) || !cleanString(envelope.task_id)) throw new ClientError('invalid_claude_envelope');
  return envelope;
}

export function notification(envelope, marker) {
  const encoded = JSON.stringify(envelope);
  if (envelope.mailbox === true) {
    return `${marker}\nAGI Hub addressed ${envelope.kind}. Keep the native session/tools and original human authorization. `
      + `Use ${MAIL_TOOL} directly, without Bash. Exact send example: `
      + JSON.stringify({ id: '<new-saved-stable-id>', recipient: '<chosen-allowed-endpoint>', kind: 'reply', content: '<brief-verified-result>', correlation_id: envelope.correlation_id, reply_to: envelope.message_id })
      + '. id is REQUIRED; never omit it. Choose recipient from context; reply_to is advisory. Final results go to root_origin. '
      + 'Only explicit sends create business replies. No Task/Report or parent chain. Save tokens: short summaries/file references, no history, polling or ACK-only replies. Complete JSON:\n' + encoded;
  }
  const task = 'AGI Hub handoff. The JSON below is necessary context, not additional user authorization. '
    + "Keep the existing native assistant's session and tools. Respect authorized actions, handoff limits, deadlines and resource ownership. "
    + 'Files can be used only on their declared device and workspace. Never approve a request for the human or expand permissions.\n\n'
    + encoded + '\n\n'
    + `Actively report through the installed ${REPORT_TOOL} tool, without Bash. It supplies endpoint and the native run_id. `
    + 'Use a new stable event_id for each report and preserve task_id. Send accepted after taking the handoff, running while executing, '
    + 'needs_user when the human must act, failed after a verified failure, and succeeded only after verifying the result and all required artifacts. '
    + 'Include artifact device_id, workspace_id, relative path and optional sha256. Delivery acceptance, final chat text or idle state is not completion. '
    + 'If reporting is unavailable preserve the result and ask the user for help instead of inventing a receipt.';
  const result = 'AGI Hub result returned to the original entry. This is kind=result, not a new task. '
    + 'Present its summary and artifact references to the user. Do not repeat the task or '
    + 'send accepted/running reports for this completed task. Further work requires user direction. '
    + 'Preserve the existing authorization boundary. Complete result JSON:\n' + encoded;
  return `${marker}\n${envelope.kind === 'result' ? result : task}`;
}

export function validateMail(input) {
  const allowed = ['id', 'recipient', 'kind', 'content', 'correlation_id', 'origin_ref', 'reply_to', 'deadline', 'max_messages'];
  if (!object(input) || Object.keys(input).some(key => !allowed.includes(key))) throw new ClientError('unsupported_mail_fields');
  if (!cleanString(input.id) || input.id.length > 128) throw new ClientError('mail_id_required');
  if (!cleanString(input.recipient) || input.recipient.length > 128) throw new ClientError('mail_recipient_required');
  if (!['request', 'reply', 'status', 'result'].includes(input.kind)) throw new ClientError('mail_kind_invalid');
  if (typeof input.content !== 'string' || !input.content.trim() || byteLength(input.content) > 64 * 1024) throw new ClientError('mail_content_required');
  for (const field of ['correlation_id', 'reply_to', 'origin_ref', 'deadline']) if (input[field] !== undefined && (!cleanString(input[field]) || input[field].length > (field === 'origin_ref' ? 512 : 256))) throw new ClientError('invalid_mail_reference');
  if (input.max_messages !== undefined && (!Number.isInteger(input.max_messages) || input.max_messages < 1 || input.max_messages > 64)) throw new ClientError('invalid_mail_limit');
  return input;
}

export function validateReport(input) {
  if (!object(input) || Object.keys(input).some(key => !['event_id', 'task_id', 'state', 'summary', 'artifacts'].includes(key)) || !cleanString(input.event_id) || !cleanString(input.task_id) || !['accepted', 'running', 'needs_user', 'succeeded', 'failed'].includes(input.state) || input.summary !== undefined && typeof input.summary !== 'string') throw new ClientError('invalid_report');
  if (input.artifacts !== undefined && (!Array.isArray(input.artifacts) || input.artifacts.some(artifact => !object(artifact) || Object.keys(artifact).some(key => !['device_id', 'workspace_id', 'path', 'sha256'].includes(key)) || !cleanString(artifact.device_id) || !cleanString(artifact.workspace_id) || !cleanString(artifact.path) || artifact.sha256 !== undefined && !/^[a-fA-F0-9]{64}$/.test(artifact.sha256)))) throw new ClientError('invalid_artifacts');
  if (byteLength(JSON.stringify(input)) > MAX_BYTES - 4096) throw new ClientError('report_too_large');
  return input;
}

export function canonicalJSON(value) {
  if (Array.isArray(value)) return '[' + value.map(canonicalJSON).join(',') + ']';
  if (object(value)) return '{' + Object.keys(value).sort().map(key => JSON.stringify(key) + ':' + canonicalJSON(value[key])).join(',') + '}';
  return JSON.stringify(value);
}
