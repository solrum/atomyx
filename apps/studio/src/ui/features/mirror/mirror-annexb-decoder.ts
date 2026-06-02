import type { MirrorApi } from "../../../state/features/mirror/index.js";

/**
 * scrcpy path: raw H.264 NAL with in-band SPS/PPS. Decoder config
 * is derived from the first config packet's SPS; subsequent
 * keyframes carry their own SPS/PPS so in-band bootstrap keeps
 * working through GOP changes.
 */
export function attachAnnexBPath(
  api: MirrorApi,
  sessionId: string,
  decoder: VideoDecoder,
  configure: (c: VideoDecoderConfig) => void,
  isConfigured: () => boolean,
): () => void {
  let sps: Uint8Array | null = null;
  let pps: Uint8Array | null = null;

  const unsubFrame = api.onFrame(sessionId, (frame) => {
    const summary = classifyNalPayload(frame.nal);
    const isPureConfig =
      summary.hasConfig && !summary.hasSlice && !summary.hasIdr;
    const isIdr = summary.hasIdr;

    if (isPureConfig) {
      const parsed = extractSpsPpsFull(frame.nal);
      if (parsed) {
        sps = parsed.sps;
        pps = parsed.pps;
        if (!isConfigured()) {
          const profile = parsed.spsBody[1] ?? 0x42;
          const constraints = parsed.spsBody[2] ?? 0;
          const level = parsed.spsBody[3] ?? 0x1e;
          const hex = (n: number) => n.toString(16).padStart(2, "0").toUpperCase();
          configure({
            codec: `avc1.${hex(profile)}${hex(constraints)}${hex(level)}`,
            optimizeForLatency: true,
          } as VideoDecoderConfig);
        }
      }
      return;
    }
    if (!isConfigured()) return;

    const payload =
      isIdr && sps && pps ? concatAll(sps, pps, frame.nal) : frame.nal;
    try {
      decoder.decode(
        new EncodedVideoChunk({
          type: isIdr ? "key" : "delta",
          timestamp: frame.timestampUs,
          data: payload,
        }),
      );
    } catch (e) {
      console.error("[mirror] decode threw", e);
    }
  });
  return () => unsubFrame();
}

function classifyNalPayload(bytes: Uint8Array): {
  readonly hasIdr: boolean;
  readonly hasSlice: boolean;
  readonly hasConfig: boolean;
} {
  let hasIdr = false;
  let hasSlice = false;
  let hasConfig = false;
  let i = 0;
  while (i + 2 < bytes.length) {
    let start = -1;
    if (
      bytes[i] === 0 &&
      bytes[i + 1] === 0 &&
      bytes[i + 2] === 0 &&
      bytes[i + 3] === 1
    ) {
      start = i + 4;
      i += 4;
    } else if (bytes[i] === 0 && bytes[i + 1] === 0 && bytes[i + 2] === 1) {
      start = i + 3;
      i += 3;
    } else {
      i += 1;
      continue;
    }
    const t = (bytes[start] ?? 0) & 0x1f;
    if (t === 5) hasIdr = true;
    else if (t === 1) hasSlice = true;
    else if (t === 7 || t === 8) hasConfig = true;
  }
  return { hasIdr, hasSlice, hasConfig };
}

interface AnnexbNal {
  readonly start: number;
  readonly end: number;
  readonly type: number;
}

function iterateAnnexBNals(bytes: Uint8Array): AnnexbNal[] {
  const nals: AnnexbNal[] = [];
  const starts: { offset: number; scLen: number }[] = [];
  let i = 0;
  while (i + 2 < bytes.length) {
    if (
      bytes[i] === 0 &&
      bytes[i + 1] === 0 &&
      bytes[i + 2] === 0 &&
      bytes[i + 3] === 1
    ) {
      starts.push({ offset: i, scLen: 4 });
      i += 4;
      continue;
    }
    if (bytes[i] === 0 && bytes[i + 1] === 0 && bytes[i + 2] === 1) {
      starts.push({ offset: i, scLen: 3 });
      i += 3;
      continue;
    }
    i += 1;
  }
  for (let j = 0; j < starts.length; j += 1) {
    const s = starts[j]!;
    const end = j + 1 < starts.length ? starts[j + 1]!.offset : bytes.length;
    const bodyStart = s.offset + s.scLen;
    if (bodyStart >= end) continue;
    const type = (bytes[bodyStart] ?? 0) & 0x1f;
    nals.push({ start: bodyStart, end, type });
  }
  return nals;
}

function extractSpsPpsFull(payload: Uint8Array): {
  readonly sps: Uint8Array;
  readonly pps: Uint8Array;
  readonly spsBody: Uint8Array;
} | null {
  const nals = iterateAnnexBNals(payload);
  const spsNal = nals.find((n) => n.type === 7);
  const ppsNal = nals.find((n) => n.type === 8);
  if (!spsNal || !ppsNal) return null;
  const spsBody = payload.slice(spsNal.start, spsNal.end);
  const ppsBody = payload.slice(ppsNal.start, ppsNal.end);
  const startCode = new Uint8Array([0, 0, 0, 1]);
  const sps = new Uint8Array(4 + spsBody.byteLength);
  sps.set(startCode, 0);
  sps.set(spsBody, 4);
  const pps = new Uint8Array(4 + ppsBody.byteLength);
  pps.set(startCode, 0);
  pps.set(ppsBody, 4);
  return { sps, pps, spsBody };
}

function concatAll(...arrays: Uint8Array[]): Uint8Array {
  const total = arrays.reduce((sum, a) => sum + a.byteLength, 0);
  const out = new Uint8Array(total);
  let o = 0;
  for (const a of arrays) {
    out.set(a, o);
    o += a.byteLength;
  }
  return out;
}
