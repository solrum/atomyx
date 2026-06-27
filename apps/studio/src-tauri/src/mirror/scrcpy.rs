//! Direct scrcpy-server integration. We push the server JAR, set
//! up an adb reverse tunnel, launch the Java server, and speak the
//! binary wire protocol ourselves — no scrcpy CLI, no ffmpeg, no
//! fifo. This mirrors what Android Studio's Device Mirror does:
//! pull raw H.264 NAL units plus PTS straight out of the server.
//!
//! Wire protocol (video only; audio+control disabled):
//!
//!   - optional 1-byte dummy (`send_dummy_byte=true`)
//!   - 64-byte device name, NUL-padded (`send_device_meta=true`)
//!   - 12-byte video codec meta: codec_id BE u32 | width BE u32 |
//!     height BE u32 (`send_codec_meta=true`)
//!   - loop:
//!       - 12-byte frame meta: pts_and_flags BE i64 | packet_size
//!         BE u32. Bit 63 = CONFIG packet (SPS/PPS), bit 62 =
//!         KEY_FRAME, bits 0..62 = pts µs.
//!       - `packet_size` bytes of Annex-B NAL payload.

use std::io::ErrorKind;
use std::path::{Path, PathBuf};
use std::process::Stdio;
use std::sync::Arc;

use serde::{Deserialize, Serialize};
use tauri::ipc::Channel;
use tokio::io::{AsyncReadExt, BufReader};
use tokio::net::{TcpListener, TcpStream};
use tokio::process::Command;
use tokio::sync::Mutex;

use super::session::{Session, SessionRegistry};

#[derive(Debug, Clone, Deserialize)]
pub struct MirrorTarget {
    pub id: String,
    pub kind: String,
    #[allow(dead_code)]
    #[serde(rename = "displayName")]
    pub display_name: Option<String>,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct MirrorOptions {
    #[serde(rename = "bitrate")]
    pub bit_rate: Option<u32>,
    #[serde(rename = "maxSize")]
    pub max_size: Option<u32>,
    #[allow(dead_code)]
    pub orientation: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct WireSession {
    pub id: String,
    #[serde(rename = "startedAt")]
    pub started_at: i64,
    #[serde(rename = "videoWidth")]
    pub video_width: u16,
    #[serde(rename = "videoHeight")]
    pub video_height: u16,
}

#[derive(Debug, Clone, Serialize)]
pub struct WireFrame {
    #[serde(rename = "sessionId")]
    pub session_id: String,
    pub nal: Vec<u8>,
    #[serde(rename = "timestampUs")]
    pub timestamp_us: i64,
    pub keyframe: bool,
}

const SERVER_REMOTE_PATH: &str = "/data/local/tmp/atomyx-scrcpy-server.jar";
// scrcpy-server.jar ships with the scrcpy CLI via Homebrew. Version
// must match the jar strictly — mismatched version makes the server
// abort with "SERVER_DIED_UNEXPECTEDLY" immediately.
const SERVER_VERSION: &str = "3.3.4";

pub async fn start_scrcpy_session(
    target: MirrorTarget,
    opts: MirrorOptions,
    channel: Channel<WireFrame>,
    registry: Arc<SessionRegistry>,
) -> Result<WireSession, String> {
    if target.kind != "android" {
        return Err(format!(
            "scrcpy backend does not support target kind '{}'",
            target.kind
        ));
    }

    let adb = which_ish("adb").map_err(|_| "adb not found in PATH".to_string())?;
    let server_jar = locate_server_jar()?;

    eprintln!(
        "[mirror] scrcpy direct: adb={:?} server_jar={:?} target={}",
        adb, server_jar, target.id
    );

    // Push the server jar under our own name so multiple tools
    // (scrcpy CLI, Atomyx) don't contend for the same remote path.
    adb_push(&adb, &target.id, &server_jar, SERVER_REMOTE_PATH).await?;

    // Random 31-bit session id → 8-char lowercase hex. The server
    // expects exactly this format for the abstract socket name.
    let scid = rand_scid();
    let scid_hex = format!("{scid:08x}");
    let socket_name = format!("scrcpy_{scid_hex}");

    // Bind a local TCP listener, then `adb reverse` the device's
    // abstract socket to that port. Once the server starts it will
    // connect back through the reverse tunnel.
    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .map_err(|e| format!("failed to bind local tcp port: {e}"))?;
    let local_port = listener
        .local_addr()
        .map_err(|e| format!("local_addr failed: {e}"))?
        .port();
    adb_reverse(&adb, &target.id, &socket_name, local_port).await?;

    // Spawn the Java server on the device. tunnel_forward=false so
    // the server becomes the *connecting* peer (connects to our
    // local listener via the reverse tunnel).
    // tunnel_forward=false means the server connects to the host
    // listener via the reverse tunnel; in that mode the server
    // never sends the leading dummy byte (it's only written from
    // the tunnel_forward=true path). We still ask explicitly so
    // behaviour is unambiguous if scrcpy changes its default.
    let mut server_args = vec![
        format!("CLASSPATH={SERVER_REMOTE_PATH}"),
        "app_process".into(),
        "/".into(),
        "com.genymobile.scrcpy.Server".into(),
        SERVER_VERSION.into(),
        format!("scid={scid_hex}"),
        "log_level=info".into(),
        "audio=false".into(),
        // Control enabled so Studio can inject touch / key events
        // through the second socket the server opens.
        "control=true".into(),
        "cleanup=false".into(),
        "send_device_meta=true".into(),
        "send_frame_meta=true".into(),
        "send_dummy_byte=false".into(),
        "send_codec_meta=true".into(),
    ];
    if let Some(max_size) = opts.max_size {
        server_args.push(format!("max_size={max_size}"));
    }
    if let Some(bit_rate) = opts.bit_rate {
        server_args.push(format!("video_bit_rate={bit_rate}"));
    }

    let mut shell_args = vec!["-s".to_string(), target.id.clone(), "shell".to_string()];
    shell_args.extend(server_args);

    let mut server_child = Command::new(&adb)
        .args(&shell_args)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .kill_on_drop(true)
        .spawn()
        .map_err(|e| format!("failed to spawn adb shell for server: {e}"))?;

    // Stream server stdout / stderr into the Studio dev log so
    // protocol errors surface instead of dying silently.
    if let Some(stdout) = server_child.stdout.take() {
        tokio::spawn(tail_lines(stdout, "[mirror] server(stdout)"));
    }
    if let Some(stderr) = server_child.stderr.take() {
        tokio::spawn(tail_lines(stderr, "[mirror] server(stderr)"));
    }

    // scrcpy opens one socket per enabled stream — in order:
    // video, then control (audio disabled). Accept both.
    let (video_socket, _) =
        tokio::time::timeout(std::time::Duration::from_secs(10), listener.accept())
            .await
            .map_err(|_| "timeout waiting for scrcpy-server (video)".to_string())?
            .map_err(|e| format!("accept video: {e}"))?;
    let _ = video_socket.set_nodelay(true);
    eprintln!("[mirror] scrcpy-server video socket connected");

    let (control_socket, _) =
        tokio::time::timeout(std::time::Duration::from_secs(10), listener.accept())
            .await
            .map_err(|_| "timeout waiting for scrcpy-server (control)".to_string())?
            .map_err(|e| format!("accept control: {e}"))?;
    let _ = control_socket.set_nodelay(true);
    eprintln!("[mirror] scrcpy-server control socket connected");

    // Peek the codec meta off the video socket before the parser
    // task takes it over, so the caller gets the device resolution
    // in the WireSession handshake response.
    let mut video_reader = BufReader::new(video_socket);
    let (video_width, video_height) = read_video_prelude(&mut video_reader).await?;

    let mut session = Session::new("scrcpy");
    let session_id = session.id.clone();
    session.scrcpy = Some(server_child);
    session.control_stream = Some(Arc::new(Mutex::new(control_socket)));
    session.video_size = Some((video_width, video_height));

    let registry_for_cleanup = registry.clone();
    let id_for_task = session_id.clone();
    let parser_task = tokio::spawn(async move {
        if let Err(e) = parse_video_stream(video_reader, &id_for_task, channel).await {
            eprintln!("[mirror] parser error: {e}");
        }
        let _ = registry_for_cleanup.remove(&id_for_task).await;
    });
    session.parser_task = Some(parser_task);

    // Tear down the reverse tunnel after the session ends. We
    // carry the device id + socket name on the session so shutdown
    // can reach adb without the caller re-deriving them.
    session.adb_reverse_cleanup = Some((adb.clone(), target.id.clone(), socket_name));

    let wire = WireSession {
        id: session.id.clone(),
        started_at: session.started_at,
        video_width,
        video_height,
    };
    registry.insert(session).await;
    Ok(wire)
}

/// Pulled out of `parse_video_stream` so `start_scrcpy_session`
/// can report the device resolution synchronously before handing
/// the socket to the parser task.
async fn read_video_prelude(
    reader: &mut BufReader<TcpStream>,
) -> Result<(u16, u16), String> {
    let mut name_bytes = [0u8; 64];
    read_exact(reader, &mut name_bytes, "device name").await?;
    let name_end = name_bytes.iter().position(|&b| b == 0).unwrap_or(64);
    eprintln!(
        "[mirror] device name: {:?}",
        String::from_utf8_lossy(&name_bytes[..name_end])
    );
    let mut codec_meta = [0u8; 12];
    read_exact(reader, &mut codec_meta, "codec meta").await?;
    let codec_id = u32::from_be_bytes([codec_meta[0], codec_meta[1], codec_meta[2], codec_meta[3]]);
    let width =
        u32::from_be_bytes([codec_meta[4], codec_meta[5], codec_meta[6], codec_meta[7]]) as u16;
    let height = u32::from_be_bytes([
        codec_meta[8],
        codec_meta[9],
        codec_meta[10],
        codec_meta[11],
    ]) as u16;
    eprintln!("[mirror] codec id=0x{codec_id:08x} size={width}x{height}");
    Ok((width, height))
}

/// scrcpy touch actions as they appear on the wire. Keep in sync
/// with Android's `MotionEvent.ACTION_*`.
#[repr(u8)]
pub enum TouchAction {
    Down = 0,
    Up = 1,
    Move = 2,
}

impl TouchAction {
    fn from_str(s: &str) -> Result<Self, String> {
        match s {
            "down" => Ok(Self::Down),
            "up" => Ok(Self::Up),
            "move" => Ok(Self::Move),
            other => Err(format!("unknown touch action '{other}'")),
        }
    }
}

pub async fn send_touch_event(
    session_id: &str,
    action: &str,
    x: i32,
    y: i32,
    pressure: f32,
    registry: Arc<SessionRegistry>,
) -> Result<(), String> {
    let handle = registry
        .get(session_id)
        .await
        .ok_or_else(|| format!("unknown session {session_id}"))?;
    let (stream, size) = {
        let guard = handle.lock().await;
        let stream = guard
            .control_stream
            .clone()
            .ok_or_else(|| "session has no control socket".to_string())?;
        let size = guard
            .video_size
            .ok_or_else(|| "session is missing video size".to_string())?;
        (stream, size)
    };

    let action_enum = TouchAction::from_str(action)?;
    let pressure_fp = float_to_u16_fixed(pressure);
    let mut buf = [0u8; 32];
    buf[0] = 2; // TYPE_INJECT_TOUCH_EVENT
    buf[1] = action_enum as u8;
    // Pointer id — use the well-known "primary" id (all ones
    // upper bits means "generic" per scrcpy app/input.h). A fixed
    // value per pointer finger would be cleaner for multi-touch;
    // for now single-pointer covers tap / swipe.
    buf[2..10].copy_from_slice(&(-1i64).to_be_bytes());
    buf[10..14].copy_from_slice(&x.to_be_bytes());
    buf[14..18].copy_from_slice(&y.to_be_bytes());
    buf[18..20].copy_from_slice(&size.0.to_be_bytes());
    buf[20..22].copy_from_slice(&size.1.to_be_bytes());
    buf[22..24].copy_from_slice(&pressure_fp.to_be_bytes());
    // action_button + buttons left 0 — fine for plain touch.

    use tokio::io::AsyncWriteExt;
    let mut guard = stream.lock().await;
    guard
        .write_all(&buf)
        .await
        .map_err(|e| format!("control write: {e}"))?;
    guard.flush().await.ok();
    eprintln!("[mirror] touch action={action} x={x} y={y} pressure={pressure:.2}");
    Ok(())
}

fn float_to_u16_fixed(v: f32) -> u16 {
    let clamped = v.clamp(0.0, 1.0);
    if clamped >= 1.0 {
        0xFFFF
    } else {
        (clamped * 65536.0) as u16
    }
}

pub async fn stop_scrcpy_session(
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

async fn parse_video_stream(
    mut reader: BufReader<TcpStream>,
    session_id: &str,
    channel: Channel<WireFrame>,
) -> Result<(), String> {
    // Prelude (device name + codec meta) is read up-front by the
    // caller so the session handshake can carry the video size.
    // The reader arrives here already positioned at the first
    // frame meta header.

    // Frame loop.
    const FLAG_CONFIG: u64 = 1 << 63;
    const FLAG_KEYFRAME: u64 = 1 << 62;
    let mut total = 0u64;
    // scrcpy's PTS is wall-clock µs since boot (≥ 10^11). Mux.js
    // derives 90 kHz ticks (pts × 90000 / 1e6), which would
    // overflow uint32 fields in the fmp4 writer. Normalize to
    // session start: first media packet becomes pts=0, subsequent
    // frames are deltas in µs.
    let mut first_media_pts: Option<i64> = None;
    loop {
        let mut frame_header = [0u8; 12];
        match reader.read_exact(&mut frame_header).await {
            Ok(_) => {}
            Err(e) if e.kind() == ErrorKind::UnexpectedEof => break,
            Err(e) => return Err(format!("read frame header: {e}")),
        }
        let pts_and_flags = u64::from_be_bytes([
            frame_header[0],
            frame_header[1],
            frame_header[2],
            frame_header[3],
            frame_header[4],
            frame_header[5],
            frame_header[6],
            frame_header[7],
        ]);
        let packet_size = u32::from_be_bytes([
            frame_header[8],
            frame_header[9],
            frame_header[10],
            frame_header[11],
        ]) as usize;

        if packet_size == 0 {
            continue;
        }
        let mut payload = vec![0u8; packet_size];
        read_exact(&mut reader, &mut payload, "frame payload").await?;

        let is_config = (pts_and_flags & FLAG_CONFIG) != 0;
        let is_keyframe = (pts_and_flags & FLAG_KEYFRAME) != 0;
        let raw_pts = (pts_and_flags & !(FLAG_CONFIG | FLAG_KEYFRAME)) as i64;

        // Config packets ride PTS=0 on the wire (they have no
        // meaningful presentation time); anchor the session clock
        // on the first media packet and emit deltas in µs.
        let pts = if is_config {
            0
        } else {
            match first_media_pts {
                Some(base) => raw_pts - base,
                None => {
                    first_media_pts = Some(raw_pts);
                    0
                }
            }
        };

        total += 1;
        if total <= 5 || total % 60 == 0 {
            eprintln!(
                "[mirror] frame #{total} config={is_config} keyframe={is_keyframe} pts_us={pts} size={packet_size}"
            );
        }

        // mux.js's `VideoSegmentStream.flush()` drops NAL units
        // preceding the first access-unit-delimiter (AUD, nal_type
        // 9). MediaCodec on Android does not emit AUDs in its raw
        // Annex-B output, so the muxer would otherwise produce no
        // segments. Prepend an AUD (primary_pic_type=7 so every
        // frame type is allowed) before each non-config packet.
        let nal_bytes = if is_config {
            payload
        } else {
            let mut buf = Vec::with_capacity(payload.len() + 6);
            buf.extend_from_slice(&[0x00, 0x00, 0x00, 0x01, 0x09, 0xF0]);
            buf.extend_from_slice(&payload);
            buf
        };

        let frame = WireFrame {
            session_id: session_id.to_string(),
            nal: nal_bytes,
            timestamp_us: pts,
            // For MSE bootstrapping we want SPS/PPS (CONFIG) AND
            // IDR slices to mark keyframe-ness at the mux layer.
            keyframe: is_keyframe || is_config,
        };
        if channel.send(frame).is_err() {
            eprintln!("[mirror] channel closed, parser exiting");
            break;
        }
    }
    Ok(())
}

async fn read_exact<R: tokio::io::AsyncRead + Unpin>(
    reader: &mut R,
    buf: &mut [u8],
    what: &'static str,
) -> Result<(), String> {
    reader
        .read_exact(buf)
        .await
        .map_err(|e| format!("read {what}: {e}"))
        .map(|_| ())
}

async fn tail_lines<R: tokio::io::AsyncRead + Unpin + Send + 'static>(reader: R, prefix: &'static str) {
    use tokio::io::AsyncBufReadExt;
    let mut lines = tokio::io::BufReader::new(reader).lines();
    while let Ok(Some(line)) = lines.next_line().await {
        eprintln!("{prefix}: {line}");
    }
}

async fn adb_push(
    adb: &Path,
    serial: &str,
    src: &Path,
    dst: &str,
) -> Result<(), String> {
    let out = Command::new(adb)
        .args([
            "-s",
            serial,
            "push",
            src.to_str().ok_or("server jar path not utf-8")?,
            dst,
        ])
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .output()
        .await
        .map_err(|e| format!("adb push: {e}"))?;
    if !out.status.success() {
        return Err(format!(
            "adb push failed ({}): {}",
            out.status,
            String::from_utf8_lossy(&out.stderr)
        ));
    }
    Ok(())
}

async fn adb_reverse(
    adb: &Path,
    serial: &str,
    socket_name: &str,
    local_port: u16,
) -> Result<(), String> {
    let out = Command::new(adb)
        .args([
            "-s",
            serial,
            "reverse",
            &format!("localabstract:{socket_name}"),
            &format!("tcp:{local_port}"),
        ])
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .output()
        .await
        .map_err(|e| format!("adb reverse: {e}"))?;
    if !out.status.success() {
        return Err(format!(
            "adb reverse failed ({}): {}",
            out.status,
            String::from_utf8_lossy(&out.stderr)
        ));
    }
    Ok(())
}

pub async fn adb_reverse_remove(
    adb: &Path,
    serial: &str,
    socket_name: &str,
) -> Result<(), String> {
    let out = Command::new(adb)
        .args([
            "-s",
            serial,
            "reverse",
            "--remove",
            &format!("localabstract:{socket_name}"),
        ])
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .output()
        .await
        .map_err(|e| format!("adb reverse --remove: {e}"))?;
    if !out.status.success() {
        return Err(format!(
            "adb reverse --remove failed ({}): {}",
            out.status,
            String::from_utf8_lossy(&out.stderr)
        ));
    }
    Ok(())
}

fn locate_server_jar() -> Result<PathBuf, String> {
    if let Ok(env_path) = std::env::var("ATOMYX_SCRCPY_SERVER") {
        return Ok(PathBuf::from(env_path));
    }
    // Homebrew ships the jar alongside the scrcpy binary.
    const CANDIDATES: &[&str] = &[
        "/opt/homebrew/share/scrcpy/scrcpy-server",
        "/usr/local/share/scrcpy/scrcpy-server",
        "/usr/share/scrcpy/scrcpy-server",
    ];
    for c in CANDIDATES {
        let p = PathBuf::from(c);
        if p.is_file() {
            return Ok(p);
        }
    }
    Err("scrcpy-server jar not found. Install scrcpy (brew install scrcpy) or set ATOMYX_SCRCPY_SERVER.".into())
}

fn which_ish(name: &str) -> Result<PathBuf, ()> {
    let Ok(path) = std::env::var("PATH") else {
        return Err(());
    };
    for dir in path.split(':') {
        let candidate = PathBuf::from(dir).join(name);
        if candidate.is_file() {
            return Ok(candidate);
        }
    }
    Err(())
}

fn rand_scid() -> u32 {
    // scrcpy's protocol says scid must be a 31-bit non-negative
    // integer so the hex representation is always 8 chars.
    use std::time::{SystemTime, UNIX_EPOCH};
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.subsec_nanos())
        .unwrap_or(0);
    let pid = std::process::id();
    ((nanos ^ pid.wrapping_mul(2654435761)) & 0x7FFF_FFFF).max(1)
}

