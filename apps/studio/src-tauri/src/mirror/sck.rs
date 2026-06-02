//! iOS Simulator adapter. Spawns the `atomyx-sim-capture` Swift
//! helper, which drives ScreenCaptureKit + VideoToolbox and broadcasts
//! Annex-B NAL units over a localhost WebSocket server. The helper
//! emits a single JSON handshake line to stdout:
//!   {"event":"listen","port":<port>,"transport":"ws"}
//! Rust reads that line, stores the port in the session, and lets the
//! webview connect directly. Stdout is drained in the background to
//! prevent pipe buffer stall.
//!
//! The fMP4 fallback path (ATOMYX_MIRROR_BACKEND=fmp4 in the helper's
//! environment) is preserved for developer rollback. In that mode the
//! helper emits {"event":"start","transport":"stdout-fmp4"} and then
//! streams fMP4 segments; Rust forwards them via Channel<WireFrame> as
//! before.

use std::path::{Path, PathBuf};
use std::process::Stdio;
use std::sync::Arc;

use serde::Deserialize;
use tauri::ipc::Channel;
use tokio::io::AsyncReadExt;
use tokio::process::Command;
use tokio::sync::Mutex;

use super::scrcpy::{MirrorOptions, MirrorTarget, WireFrame, WireSession};
use super::session::{Session, SessionRegistry};

const BACKEND: &str = "sck";

/// JSON handshake emitted as the first stdout line by the helper.
#[derive(Debug, Deserialize)]
struct HelperHandshake {
    #[allow(dead_code)]
    event: String,
    /// Set when transport == "ws"; absent for fmp4 path.
    port: Option<u16>,
    transport: String,
}

pub async fn start_sck_session(
    target: MirrorTarget,
    opts: MirrorOptions,
    channel: Channel<WireFrame>,
    registry: Arc<SessionRegistry>,
) -> Result<WireSession, String> {
    if target.kind != "ios-simulator" {
        return Err(format!(
            "SCK backend does not support target kind '{}'",
            target.kind
        ));
    }

    let helper = resolve_helper_binary()?;
    eprintln!("[mirror] sck helper: {:?} target={}", helper, target.id);

    let max_size = opts.max_size.unwrap_or(1080);
    let bitrate = opts.bit_rate.unwrap_or(8_000_000);

    let mut child = Command::new(&helper)
        .args([
            "--udid",
            &target.id,
            "--max-size",
            &max_size.to_string(),
            "--bitrate",
            &bitrate.to_string(),
        ])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .kill_on_drop(true)
        .spawn()
        .map_err(|e| format!("failed to spawn atomyx-sim-capture: {e}"))?;

    let stdout = child
        .stdout
        .take()
        .ok_or("atomyx-sim-capture stdout not captured")?;
    let stderr = child.stderr.take();

    // Drain stderr to prevent pipe buffer stall and surface helper logs.
    if let Some(err) = stderr {
        tokio::spawn(async move {
            use tokio::io::{AsyncBufReadExt, BufReader};
            let mut lines = BufReader::new(err).lines();
            while let Ok(Some(line)) = lines.next_line().await {
                eprintln!("[mirror] sim-capture: {line}");
            }
        });
    }

    // Read the single handshake line from stdout.
    let mut stdout_lines = {
        use tokio::io::{AsyncBufReadExt, BufReader};
        BufReader::new(stdout).lines()
    };
    let handshake_line = stdout_lines
        .next_line()
        .await
        .map_err(|e| format!("reading sim-capture handshake: {e}"))?
        .ok_or_else(|| "sim-capture exited before emitting handshake".to_string())?;

    let handshake: HelperHandshake = serde_json::from_str(&handshake_line)
        .map_err(|e| format!("invalid sim-capture handshake JSON: {e} — line={handshake_line:?}"))?;

    eprintln!(
        "[mirror] sck handshake: transport={} port={:?}",
        handshake.transport, handshake.port
    );

    let mut session = Session::new(BACKEND);
    let session_id = session.id.clone();
    session.scrcpy = Some(child);

    match handshake.transport.as_str() {
        "ws" => {
            let ws_port = handshake
                .port
                .ok_or_else(|| "WS handshake missing port".to_string())?;

            // Store the WS port so mirror_simctl_get_endpoint can return it.
            session.ws_port = Some(ws_port);

            // Drain remaining stdout in a background task. The helper
            // should not write more, but pipe buffer stall would block
            // the process if we don't read it.
            let registry_for_cleanup = registry.clone();
            let id_for_drain = session_id.clone();
            let drain_task = tokio::spawn(async move {
                let mut lines = stdout_lines;
                while let Ok(Some(line)) = lines.next_line().await {
                    eprintln!("[mirror] unexpected sim-capture stdout: {line}");
                }
                let _ = registry_for_cleanup.remove(&id_for_drain).await;
            });
            session.parser_task = Some(drain_task);
        }
        "stdout-fmp4" => {
            // fMP4 fallback: forward raw fMP4 bytes via the channel.
            // Unwrap the BufReader to get the raw ChildStdout back.
            let inner = stdout_lines.into_inner().into_inner();
            let registry_for_cleanup = registry.clone();
            let id_for_task = session_id.clone();
            let parser_task = tokio::spawn(async move {
                if let Err(e) = forward_fmp4_chunks(inner, &id_for_task, channel).await {
                    eprintln!("[mirror] sim-capture mp4 forwarder error: {e}");
                }
                let _ = registry_for_cleanup.remove(&id_for_task).await;
            });
            session.parser_task = Some(parser_task);
        }
        other => {
            return Err(format!("unknown sim-capture transport: {other}"));
        }
    }

    let wire = WireSession {
        id: session.id.clone(),
        started_at: session.started_at,
        video_width: max_size as u16,
        video_height: max_size as u16,
    };
    registry.insert(session).await;
    Ok(wire)
}

/// Returns the WS port for an active WS-transport session, or None if the
/// session is on the fMP4 fallback path.
pub async fn get_sck_endpoint(
    session_id: &str,
    registry: Arc<SessionRegistry>,
) -> Result<(u16, String), String> {
    let handle = registry
        .get(session_id)
        .await
        .ok_or_else(|| format!("unknown session {session_id}"))?;
    let guard = handle.lock().await;
    if let Some(port) = guard.ws_port {
        Ok((port, "ws".to_string()))
    } else {
        // fMP4 fallback path — port is not meaningful.
        Ok((0, "stdout-fmp4".to_string()))
    }
}

pub async fn stop_sck_session(
    session_id: &str,
    registry: Arc<SessionRegistry>,
) -> Result<(), String> {
    let handle = registry
        .remove(session_id)
        .await
        .ok_or_else(|| format!("unknown session {session_id}"))?;
    let mut session = extract_session(handle).await;
    session.shutdown().await;
    Ok(())
}

async fn extract_session(handle: Arc<Mutex<Session>>) -> Session {
    let mut guard = handle.lock().await;
    Session {
        id: guard.id.clone(),
        started_at: guard.started_at,
        backend: guard.backend.clone(),
        fifo_path: guard.fifo_path.take(),
        scrcpy: guard.scrcpy.take(),
        ffmpeg: guard.ffmpeg.take(),
        parser_task: guard.parser_task.take(),
        adb_reverse_cleanup: guard.adb_reverse_cleanup.take(),
        control_stream: guard.control_stream.take(),
        video_size: guard.video_size.take(),
        ws_port: guard.ws_port.take(),
    }
}

async fn forward_fmp4_chunks(
    mut reader: tokio::process::ChildStdout,
    session_id: &str,
    channel: Channel<WireFrame>,
) -> Result<(), String> {
    // Accumulate until we see the init segment's avcC box, patch
    // its `constraint_set1_flag` (Safari rejects plain Baseline),
    // then flush downstream. Subsequent media segments pass
    // through untouched — they don't carry parameter sets.
    let mut patched_init = false;
    let mut carry = Vec::<u8>::with_capacity(64 * 1024);
    let mut buf = vec![0u8; 32 * 1024];
    let mut total_bytes = 0u64;
    let mut chunks = 0u64;
    let mut pts_counter: i64 = 0;
    loop {
        match reader.read(&mut buf).await {
            Ok(0) => {
                eprintln!(
                    "[mirror] sck mp4 EOF after {chunks} chunks / {total_bytes} bytes"
                );
                return Ok(());
            }
            Ok(n) => {
                total_bytes += n as u64;
                chunks += 1;
                if !patched_init {
                    carry.extend_from_slice(&buf[..n]);
                    if let Some(patched_len) = patch_avcc_if_present(&mut carry) {
                        patched_init = true;
                        let to_send = carry[..patched_len].to_vec();
                        carry.drain(..patched_len);
                        let frame = WireFrame {
                            session_id: session_id.to_string(),
                            nal: to_send,
                            timestamp_us: pts_counter,
                            keyframe: true,
                        };
                        pts_counter += 1;
                        if channel.send(frame).is_err() {
                            return Ok(());
                        }
                        if !carry.is_empty() {
                            let tail = carry.split_off(0);
                            let tail_frame = WireFrame {
                                session_id: session_id.to_string(),
                                nal: tail,
                                timestamp_us: pts_counter,
                                keyframe: false,
                            };
                            pts_counter += 1;
                            if channel.send(tail_frame).is_err() {
                                return Ok(());
                            }
                        }
                    }
                    continue;
                }
                let frame = WireFrame {
                    session_id: session_id.to_string(),
                    nal: buf[..n].to_vec(),
                    timestamp_us: pts_counter,
                    keyframe: false,
                };
                pts_counter += 1;
                if channel.send(frame).is_err() {
                    return Ok(());
                }
            }
            Err(e) => return Err(format!("ffmpeg read: {e}")),
        }
    }
}

/// Locate the first `avcC` box inside `buf` and set the constraint
/// flags to `0xE0` (constraint_set0/1/2) in BOTH the box's
/// top-level `profile_compatibility` byte AND every embedded SPS
/// NAL's `profile_compatibility` byte.
///
/// Both bytes must agree, because:
///   - mp4box.js exposes the top-level byte via `videoTrack.codec`
///     (the codec string handed to `VideoDecoder.configure`).
///   - The UI prepends the embedded SPS NAL to every keyframe, and
///     WebKit's H.264 decoder parses that SPS at runtime; if the
///     parsed constraint bits are 0, it rejects the stream with
///     "Decoder failure" even when the codec string says
///     Constrained Baseline.
///
/// Returns the number of bytes inspected so the caller can flush
/// that prefix downstream once the box has been patched, or `None`
/// if the box hasn't shown up yet.
fn patch_avcc_if_present(buf: &mut [u8]) -> Option<usize> {
    for i in 0..buf.len().saturating_sub(8) {
        if &buf[i..i + 4] != b"avcC" {
            continue;
        }
        // avcC body layout (ISO/IEC 14496-15):
        //   configurationVersion : u8
        //   AVCProfileIndication : u8   = SPS profile_idc
        //   profile_compatibility: u8   ← top-level constraint byte
        //   AVCLevelIndication   : u8
        //   lengthSizeMinusOne   : u8 (low 2 bits)
        //   numOfSequenceParameterSets : u8 (low 5 bits)
        //   for each SPS:
        //     u16 sequenceParameterSetLength
        //     SPS NAL bytes (header u8 = 0x67, profile_idc u8,
        //                    profile_compatibility u8 ← second
        //                    byte to patch, level_idc u8, ...)
        //   numOfPictureParameterSets : u8
        //   ...
        let body = i + 4;
        if body + 6 > buf.len() {
            return None;
        }
        let top_compat = body + 2;
        let old_top = buf[top_compat];
        buf[top_compat] = old_top | 0xE0;
        eprintln!(
            "[mirror] patched avcC.profile_compatibility {:#04x} → {:#04x}",
            old_top, buf[top_compat]
        );

        let num_sps = (buf[body + 5] & 0x1F) as usize;
        let mut pos = body + 6;
        for _ in 0..num_sps {
            if pos + 2 > buf.len() {
                break;
            }
            let sps_len = ((buf[pos] as usize) << 8) | (buf[pos + 1] as usize);
            let sps_start = pos + 2;
            if sps_len < 3 || sps_start + sps_len > buf.len() {
                break;
            }
            if (buf[sps_start] & 0x1F) == 7 {
                let sps_compat = sps_start + 2;
                let old_sps = buf[sps_compat];
                buf[sps_compat] = old_sps | 0xE0;
                eprintln!(
                    "[mirror] patched SPS.profile_compatibility {:#04x} → {:#04x}",
                    old_sps, buf[sps_compat]
                );
            }
            pos = sps_start + sps_len;
        }
        return Some(buf.len());
    }
    None
}

fn resolve_helper_binary() -> Result<PathBuf, String> {
    // ScreenCaptureKit refuses to stream from ad-hoc signed
    // binaries, so the helper ships as a Developer-ID signed
    // `.app` bundle. The resolver checks the bundle executable
    // first, then falls back to any loose binary copied next to
    // the bundle by an older build.
    if let Ok(env_path) = std::env::var("ATOMYX_SIM_CAPTURE") {
        let p = PathBuf::from(env_path);
        if p.is_file() {
            return Ok(p);
        }
    }

    if let Ok(exe) = std::env::current_exe() {
        if let Some(parent) = exe.parent() {
            let candidates = [
                parent.join("atomyx-sim-capture.app/Contents/MacOS/atomyx-sim-capture"),
                parent.join("atomyx-sim-capture"),
            ];
            for p in candidates {
                if p.is_file() {
                    return Ok(p);
                }
            }
        }
    }

    let rel_candidates = [
        "helpers/atomyx-sim-capture/atomyx-sim-capture.app/Contents/MacOS/atomyx-sim-capture",
        "apps/studio/src-tauri/helpers/atomyx-sim-capture/atomyx-sim-capture.app/Contents/MacOS/atomyx-sim-capture",
        "src-tauri/helpers/atomyx-sim-capture/atomyx-sim-capture.app/Contents/MacOS/atomyx-sim-capture",
    ];
    for rel in rel_candidates {
        let p = Path::new(rel);
        if p.is_file() {
            return Ok(p.to_path_buf());
        }
        let with_manifest = Path::new(env!("CARGO_MANIFEST_DIR")).join(rel);
        if with_manifest.is_file() {
            return Ok(with_manifest);
        }
    }

    Err(
        "atomyx-sim-capture helper not found. Build it with \
         `bash apps/studio/src-tauri/helpers/atomyx-sim-capture/build.sh` \
         or set ATOMYX_SIM_CAPTURE to the bundle executable path."
            .to_string(),
    )
}
