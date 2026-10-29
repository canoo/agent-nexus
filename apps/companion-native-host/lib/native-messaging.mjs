/**
 * Chrome and Edge native messaging use a four-byte little-endian length,
 * followed by one UTF-8 JSON message. The protocol permits much larger
 * payloads, but Companion activity envelopes intentionally do not.
 */
export const MAX_NATIVE_MESSAGE_BYTES = 4 * 1024;

const FRAME_HEADER_BYTES = 4;

export function encodeNativeMessage(message, { maxMessageBytes = MAX_NATIVE_MESSAGE_BYTES } = {}) {
  const payload = Buffer.from(JSON.stringify(message), "utf8");
  if (payload.length === 0 || payload.length > maxMessageBytes) {
    throw new RangeError("native message exceeds the fixed Companion limit");
  }
  const frame = Buffer.allocUnsafe(FRAME_HEADER_BYTES + payload.length);
  frame.writeUInt32LE(payload.length, 0);
  payload.copy(frame, FRAME_HEADER_BYTES);
  return frame;
}

/**
 * Incremental, bounded native-message decoder. A declared oversize frame is
 * terminal: consuming it would require retaining untrusted content.
 */
export class NativeMessageDecoder {
  constructor({ maxMessageBytes = MAX_NATIVE_MESSAGE_BYTES } = {}) {
    if (!Number.isSafeInteger(maxMessageBytes) || maxMessageBytes < 1) {
      throw new TypeError("maxMessageBytes must be a positive safe integer");
    }
    this.maxMessageBytes = maxMessageBytes;
    this.buffer = Buffer.alloc(0);
    this.closed = false;
  }

  push(chunk) {
    if (this.closed) return Object.freeze([{ ok: false, code: "native_input_closed" }]);
    if (!Buffer.isBuffer(chunk) && !(chunk instanceof Uint8Array)) {
      this.closed = true;
      return Object.freeze([{ ok: false, code: "invalid_native_input" }]);
    }

    const incoming = Buffer.from(chunk);
    const frames = [];
    let offset = 0;

    // A stdin chunk may contain several valid frames. Copy only enough bytes
    // for the one frame currently being assembled, so retained input stays
    // bounded even when a browser batches messages in one stream chunk.
    while (offset < incoming.length) {
      if (this.buffer.length < FRAME_HEADER_BYTES) {
        const needed = FRAME_HEADER_BYTES - this.buffer.length;
        const available = Math.min(needed, incoming.length - offset);
        this.buffer = Buffer.concat([this.buffer, incoming.subarray(offset, offset + available)]);
        offset += available;
        if (this.buffer.length < FRAME_HEADER_BYTES) break;
      }
      const length = this.buffer.readUInt32LE(0);
      if (length === 0 || length > this.maxMessageBytes) {
        this.buffer = Buffer.alloc(0);
        this.closed = true;
        frames.push(Object.freeze({ ok: false, code: "native_message_too_large" }));
        break;
      }
      const frameLength = FRAME_HEADER_BYTES + length;
      if (this.buffer.length < frameLength) {
        const needed = frameLength - this.buffer.length;
        const available = Math.min(needed, incoming.length - offset);
        this.buffer = Buffer.concat([this.buffer, incoming.subarray(offset, offset + available)]);
        offset += available;
        if (this.buffer.length < frameLength) break;
      }

      const payload = this.buffer.subarray(FRAME_HEADER_BYTES, frameLength);
      this.buffer = this.buffer.subarray(frameLength);
      try {
        frames.push(Object.freeze({ ok: true, message: JSON.parse(payload.toString("utf8")) }));
      } catch {
        // Do not surface any input bytes in diagnostics.
        frames.push(Object.freeze({ ok: false, code: "malformed_native_json" }));
      }
    }
    return Object.freeze(frames);
  }
}
