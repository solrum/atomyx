// @ts-expect-error — mp4box ships no types; typed minimally below
import MP4BoxDefault from "mp4box";

import type { MirrorApi } from "../../../state/features/mirror/index.js";

interface MP4BoxFile {
  onReady?: (info: {
    readonly tracks: readonly {
      readonly id: number;
      readonly codec: string;
      readonly video?: { readonly width?: number; readonly height?: number };
    }[];
  }) => void;
  onError?: (err: string) => void;
  onSamples?: (
    id: number,
    user: unknown,
    samples: readonly {
      readonly data: ArrayBuffer | Uint8Array;
      readonly cts: number;
      readonly is_sync: boolean;
      readonly description?: { readonly avcC?: unknown };
    }[],
  ) => void;
  appendBuffer: (chunk: ArrayBuffer & { fileStart: number }) => number;
  setExtractionOptions: (
    id: number,
    user: unknown,
    opts: { readonly nbSamples?: number },
  ) => void;
  start: () => void;
  flush: () => void;
  stop: () => void;
  getTrackById: (id: number) => {
    readonly avcC?: {
      readonly SPS?: readonly { readonly nalu: Uint8Array }[];
      readonly PPS?: readonly { readonly nalu: Uint8Array }[];
      readonly AVCProfileIndication?: number;
      readonly profile_compatibility?: number;
      readonly AVCLevelIndication?: number;
      readonly nalu_length_size?: number;
    };
  };
}

interface MP4BoxModule {
  createFile: () => MP4BoxFile;
}

const MP4Box = MP4BoxDefault as unknown as MP4BoxModule;

/**
 * simctl / SCK path: Swift's AVAssetWriterDelegate hands us
 * fragmented MP4 blobs. mp4box.js demuxes them into individual
 * H.264 samples and gives us the SPS/PPS via the avcC box; each
 * sample is already length-prefixed so we feed WebCodecs in AVCC
 * mode with the avcC as `description`.
 */
export function attachFmp4Path(
  api: MirrorApi,
  sessionId: string,
  decoder: VideoDecoder,
  configure: (c: VideoDecoderConfig) => void,
): () => void {
  const mp4 = MP4Box.createFile();
  let fileStart = 0;
  let avcCBody: Uint8Array | null = null;
  let initScanBuffer = new Uint8Array(0);
  let initScanDone = false;
  let sampleCount = 0;

  mp4.onError = (err) => {
    console.error("[mirror.ui] mp4box error", err);
  };

  mp4.onReady = (info) => {
    const videoTrack = info.tracks.find((t) => t.codec.startsWith("avc1"));
    if (!videoTrack) return;
    if (!avcCBody) {
      console.error(
        "[mirror.ui] fmp4 ready but avcC body not extracted — decoder cannot configure",
      );
      return;
    }
    console.log(
      "[mirror.ui] fmp4 ready codec",
      videoTrack.codec,
      "size",
      videoTrack.video?.width,
      "x",
      videoTrack.video?.height,
      "avcCBody",
      avcCBody.byteLength,
      "head",
      hexDump(avcCBody.slice(0, 16)),
    );
    // AVCC mode: VideoDecoder is given the raw
    // AVCDecoderConfigurationRecord as `description`. Sample data
    // is then expected length-prefixed (which is exactly what
    // mp4box hands us), so no Annex-B conversion or in-band
    // SPS/PPS prepending is needed.
    configure({
      codec: videoTrack.codec,
      codedWidth: videoTrack.video?.width,
      codedHeight: videoTrack.video?.height,
      description: avcCBody,
      optimizeForLatency: true,
    });
    mp4.setExtractionOptions(videoTrack.id, null, { nbSamples: 1 });
    mp4.start();
  };

  mp4.onSamples = (_id, _user, samples) => {
    for (const s of samples) {
      const data = s.data instanceof Uint8Array
        ? s.data
        : new Uint8Array(s.data);
      sampleCount += 1;
      if (sampleCount <= 3) {
        console.log(
          "[mirror.ui] sample #",
          sampleCount,
          s.is_sync ? "key" : "delta",
          "bytes",
          data.byteLength,
          "head",
          hexDump(data.slice(0, 32)),
        );
      }
      try {
        decoder.decode(
          new EncodedVideoChunk({
            type: s.is_sync ? "key" : "delta",
            timestamp: s.cts,
            data: data.slice().buffer as ArrayBuffer,
          }),
        );
      } catch (e) {
        console.error("[mirror.ui] fmp4 decode threw", e);
      }
    }
  };

  const unsubFrame = api.onFrame(sessionId, (frame) => {
    if (!initScanDone) {
      const merged = new Uint8Array(
        initScanBuffer.byteLength + frame.nal.byteLength,
      );
      merged.set(initScanBuffer, 0);
      merged.set(frame.nal, initScanBuffer.byteLength);
      initScanBuffer = merged;
      const extracted = extractAvcCBody(initScanBuffer);
      if (extracted) {
        avcCBody = extracted;
        initScanDone = true;
        initScanBuffer = new Uint8Array(0);
        console.log(
          "[mirror.ui] avcC body extracted",
          extracted.byteLength,
          "bytes — ConfigVer/Profile/Compat/Level:",
          [...extracted.slice(0, 4)].map((b) =>
            b.toString(16).padStart(2, "0"),
          ).join(" "),
        );
      }
    }
    const buffer = frame.nal.slice().buffer as ArrayBuffer & {
      fileStart: number;
    };
    buffer.fileStart = fileStart;
    fileStart += frame.nal.byteLength;
    mp4.appendBuffer(buffer);
  });
  return () => {
    unsubFrame();
    try {
      mp4.flush();
      mp4.stop();
    } catch {
      // already stopped
    }
  };
}

/**
 * Scan an fmp4 byte buffer for the `avcC` box and return its body
 * (everything inside the box, excluding the 4-byte size and
 * 4-byte fourcc header). Returns `null` if the box hasn't fully
 * arrived yet — caller should accumulate more bytes and retry.
 *
 * The body is an `AVCDecoderConfigurationRecord` per ISO/IEC
 * 14496-15 and is exactly the byte sequence `VideoDecoder`
 * expects as `description` for AVCC-mode H.264.
 */
function extractAvcCBody(buf: Uint8Array): Uint8Array | null {
  for (let i = 4; i + 4 <= buf.byteLength; i += 1) {
    if (
      buf[i] === 0x61 &&
      buf[i + 1] === 0x76 &&
      buf[i + 2] === 0x63 &&
      buf[i + 3] === 0x43
    ) {
      const sizePos = i - 4;
      const boxSize =
        ((buf[sizePos] ?? 0) << 24) |
        ((buf[sizePos + 1] ?? 0) << 16) |
        ((buf[sizePos + 2] ?? 0) << 8) |
        (buf[sizePos + 3] ?? 0);
      const bodyStart = i + 4;
      const bodyEnd = sizePos + boxSize;
      if (boxSize < 8 || bodyEnd > buf.byteLength) return null;
      return buf.slice(bodyStart, bodyEnd);
    }
  }
  return null;
}

function hexDump(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join(" ");
}
