export const MAX_BYTES = 1048576;
export const object = value => value !== null && typeof value === 'object' && !Array.isArray(value);
export const cleanString = value => typeof value === 'string' && value.length > 0 && value.trim() === value && !/[\x00-\x1f\x7f]/.test(value);
export class ClientError extends Error {
  constructor(code, status) { super(code); this.code = code; this.status = status; }
}
export function byteLength(text) {
  let size = 0;
  for (const char of text) { const code = char.codePointAt(0); size += code < 128 ? 1 : code < 2048 ? 2 : code < 65536 ? 3 : 4; }
  return size;
}

/** Syntax-check first, then reject duplicate keys without losing escaped-key equality. */
export function parseJSON(text) {
  if (typeof text !== 'string' || byteLength(text) > 4 * MAX_BYTES) throw new ClientError('invalid_json_size');
  let result;
  try { result = JSON.parse(text); } catch { throw new ClientError('invalid_json'); }
  let offset = 0;
  const whitespace = () => { while (offset < text.length && /\s/.test(text[offset])) offset++; };
  const string = () => {
    const start = offset++;
    while (offset < text.length) {
      if (text[offset++] === '\\') offset++;
      else if (text[offset - 1] === '"') return JSON.parse(text.slice(start, offset));
    }
    throw new ClientError('invalid_json');
  };
  const value = depth => {
    if (depth > 128) throw new ClientError('json_nesting_too_deep');
    whitespace();
    const open = text[offset];
    if (open === '{' || open === '[') {
      offset++; whitespace();
      const close = open === '{' ? '}' : ']';
      const keys = new Set();
      while (text[offset] !== close) {
        if (open === '{') {
          const key = string();
          if (keys.has(key)) throw new ClientError('duplicate_json_key');
          keys.add(key); whitespace(); offset++;
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
  if (typeof value !== 'string') throw new ClientError('invalid_url');
  const match = /^(https?):\/\/(\[::1\]|127(?:\.(?:0|[1-9][0-9]{0,2})){3})(?::([1-9][0-9]*))?\/?$/.exec(value);
  if (!match || (match[2] !== '[::1]' && match[2].split('.').some(part => Number(part) > 255))) throw new ClientError('hub_requires_numeric_loopback_base_url');
  const port = match[3] ? Number(match[3]) : (match[1] === 'https' ? 443 : 80);
  if (!Number.isInteger(port) || port < 1 || port > 65535) throw new ClientError('invalid_port');
  return { origin: `${match[1]}://${match[2]}${match[3] ? ':' + match[3] : ''}` };
}

export function absolutePath(value) {
  return cleanString(value) && (value.startsWith('/') && !value.startsWith('//') || /^[A-Za-z]:[\\/]/.test(value));
}
export function validateConfig(config) {
  if (!object(config) || Object.keys(config).some(key => !['base_url', 'endpoint', 'endpoint_token', 'native'].includes(key))) throw new ClientError('invalid_client_config_fields');
  if (!cleanString(config.endpoint) || config.endpoint.length > 128) throw new ClientError('invalid_endpoint');
  if (typeof config.endpoint_token !== 'string' || !config.endpoint_token.trim() || /[^\x20-\x7e\t]/.test(config.endpoint_token)) throw new ClientError('invalid_endpoint_token');
  const address = parseAddress(config.base_url);
  const native = config.native;
  if (!object(native) || native.transport !== 'mod' || Object.keys(native).some(key => !['transport', 'session_id', 'long_poll_ms', 'max_prompt_bytes'].includes(key))) throw new ClientError('invalid_claude_mod_fields');
  if (typeof native.session_id !== 'string' || !/^[A-Za-z0-9._:-]{1,256}$/.test(native.session_id)) throw new ClientError('invalid_claude_session_id');
  const wait = native.long_poll_ms ?? 25000;
  const bytes = native.max_prompt_bytes ?? MAX_BYTES;
  if (!Number.isInteger(wait) || wait < 1000 || wait > 25000) throw new ClientError('invalid_long_poll_ms');
  if (!Number.isInteger(bytes) || bytes < 1024 || bytes > MAX_BYTES) throw new ClientError('invalid_max_prompt_bytes');
  return { ...config, address, native: { ...native, long_poll_ms: wait, max_prompt_bytes: bytes } };
}
export function validateBinding(registered, config) {
  if (!object(registered) || registered.protocol_version !== 1) throw new ClientError('incompatible_hub_protocol');
  const endpoint = registered.endpoint;
  if (!object(endpoint) || endpoint.id !== config.endpoint || endpoint.enabled !== true || endpoint.driver !== 'claude-code' || endpoint.session_id !== config.native.session_id) throw new ClientError('claude_native_binding_mismatch');
  // This native mod implements only the original single-handoff protocol.
  // Ignore neither an opted-in peer binding nor malformed capability values:
  // waiting/reply handling requires a separately implemented native client.
  if ((endpoint.peer_protocol_version ?? 0) !== 0 || (endpoint.peer_while_busy_verified ?? false) !== false) throw new ClientError('claude_peer_protocol_not_supported');
  if ((endpoint.mailbox_protocol_version ?? 0) !== 0 || (endpoint.mailbox_while_busy_verified ?? false) !== false) throw new ClientError('claude_mailbox_protocol_not_supported');
  if (!Array.isArray(endpoint.modes) || endpoint.modes.length !== 1 || endpoint.modes[0] !== 'queue') throw new ClientError('claude_client_requires_queue_only_binding');
  if (typeof endpoint.idle_wake_verified !== 'boolean' || typeof (endpoint.result_queue_while_busy_verified ?? false) !== 'boolean') throw new ClientError('invalid_hub_verification_fields');
  if (!object(registered.session) || !['unknown', 'idle', 'busy', 'needs_user'].includes(registered.session.state)) throw new ClientError('invalid_hub_session_state');
  if (!Array.isArray(registered.outstanding)) throw new ClientError('incompatible_hub_client_lease');
  return registered;
}
export function scopeKey(config) {
  return 'agi-hub:v1:' + [config.address.origin, config.endpoint, config.native.session_id, tokenFingerprint(config.endpoint_token)].map(encodeURIComponent).join(':') + ':';
}

/** SHA-256 of the validated ASCII token; no Node or optional sandbox globals.
 * This partitions journals after Hub recreation/token rotation; it is not a credential.
 */
export function tokenFingerprint(token) {
  const constants = [0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2];
  const hash = [0x6a09e667,0xbb67ae85,0x3c6ef372,0xa54ff53a,0x510e527f,0x9b05688c,0x1f83d9ab,0x5be0cd19];
  const bytes = Array.from(token, char => char.charCodeAt(0)), bitLength = bytes.length * 8;
  bytes.push(128); while (bytes.length % 64 !== 56) bytes.push(0);
  for (let shift = 56; shift >= 0; shift -= 8) bytes.push(shift >= 32 ? Math.floor(bitLength / 2 ** shift) & 255 : bitLength >>> shift & 255);
  const rotate = (value, amount) => value >>> amount | value << 32 - amount;
  for (let offset = 0; offset < bytes.length; offset += 64) {
    const words = new Array(64);
    for (let index = 0; index < 16; index++) words[index] = bytes[offset + index * 4] << 24 | bytes[offset + index * 4 + 1] << 16 | bytes[offset + index * 4 + 2] << 8 | bytes[offset + index * 4 + 3];
    for (let index = 16; index < 64; index++) { const x = words[index - 15], y = words[index - 2]; words[index] = (words[index - 16] + (rotate(x, 7) ^ rotate(x, 18) ^ x >>> 3) + words[index - 7] + (rotate(y, 17) ^ rotate(y, 19) ^ y >>> 10)) | 0; }
    let [a,b,c,d,e,f,g,h] = hash;
    for (let index = 0; index < 64; index++) {
      const first = (h + (rotate(e, 6) ^ rotate(e, 11) ^ rotate(e, 25)) + (e & f ^ ~e & g) + constants[index] + words[index]) | 0;
      const second = ((rotate(a, 2) ^ rotate(a, 13) ^ rotate(a, 22)) + (a & b ^ a & c ^ b & c)) | 0;
      h=g; g=f; f=e; e=(d+first)|0; d=c; c=b; b=a; a=(first+second)|0;
    }
    for (const [index, value] of [a,b,c,d,e,f,g,h].entries()) hash[index] = hash[index] + value | 0;
  }
  return hash.map(value => (value >>> 0).toString(16).padStart(8, '0')).join('');
}
