//! Tauri command surface for the mirror module. Matches the
//! invoke names the TS adapters call:
//!
//!   - `mirror_scrcpy_*`  → Android.
//!   - `mirror_simctl_*`  → iOS Simulator.
//!
//! Frame streaming is delivered via Tauri `Channel<WireFrame>` —
//! the JS adapter provides the channel on `start`; subsequent
//! stop / record / clip calls reference the returned session id.

use std::sync::Arc;

use serde_json::json;
use tauri::ipc::Channel;
use tauri::State;

use super::scrcpy;
use super::sck;
use super::session::SessionRegistry;
use crate::sidecar::Sidecar;

// ---------- Android (scrcpy) ----------

#[tauri::command]
pub async fn mirror_scrcpy_start(
    target: scrcpy::MirrorTarget,
    opts: scrcpy::MirrorOptions,
    frame_channel: Channel<scrcpy::WireFrame>,
    registry: State<'_, Arc<SessionRegistry>>,
) -> Result<scrcpy::WireSession, String> {
    scrcpy::start_scrcpy_session(target, opts, frame_channel, registry.inner().clone()).await
}

#[tauri::command]
pub async fn mirror_scrcpy_stop(
    session_id: String,
    registry: State<'_, Arc<SessionRegistry>>,
) -> Result<(), String> {
    scrcpy::stop_scrcpy_session(&session_id, registry.inner().clone()).await
}

#[tauri::command]
pub async fn mirror_scrcpy_record(
    session_id: String,
    output_path: String,
) -> Result<(), String> {
    let _ = (session_id, output_path);
    // Recording while mirroring is a follow-up — scrcpy's
    // `--record` already owns the fifo. Teeing to a second output
    // requires an ffmpeg topology change; track that work
    // separately rather than bolting a stub in here.
    Err("mirror recording is not yet implemented for Android".into())
}

#[tauri::command]
pub async fn mirror_scrcpy_stop_recording(session_id: String) -> Result<(), String> {
    let _ = session_id;
    Err("mirror recording is not yet implemented for Android".into())
}

#[tauri::command]
pub async fn mirror_scrcpy_clip(
    session_id: String,
    output_path: String,
    start_ms: i64,
    end_ms: i64,
) -> Result<String, String> {
    let _ = (session_id, output_path, start_ms, end_ms);
    Err("mirror clip extraction is not yet implemented for Android".into())
}

#[tauri::command]
pub async fn mirror_scrcpy_send_touch(
    session_id: String,
    action: String,
    x: i32,
    y: i32,
    pressure: f32,
    registry: State<'_, Arc<SessionRegistry>>,
) -> Result<(), String> {
    scrcpy::send_touch_event(
        &session_id,
        &action,
        x,
        y,
        pressure,
        registry.inner().clone(),
    )
    .await
}

// ---------- iOS Simulator (ScreenCaptureKit via Swift helper) ----------

/// Transport endpoint for a live iOS Simulator mirror session.
/// Callers receive this after `mirror_simctl_start` and use it to
/// open the appropriate data path:
///   - `ws`: open a WebSocket to `ws://127.0.0.1:<port>`.
///   - `stdout-fmp4`: subscribe to the existing Channel<WireFrame>.
#[derive(serde::Serialize)]
pub struct MirrorEndpoint {
    pub port: u16,
    pub transport: String,
}

/// Returns the transport endpoint for the given iOS Simulator session.
/// Invoke immediately after `mirror_simctl_start` resolves.
#[tauri::command]
pub async fn mirror_simctl_get_endpoint(
    session_id: String,
    registry: State<'_, Arc<SessionRegistry>>,
) -> Result<MirrorEndpoint, String> {
    let (port, transport) =
        sck::get_sck_endpoint(&session_id, registry.inner().clone()).await?;
    Ok(MirrorEndpoint { port, transport })
}

#[tauri::command]
pub async fn mirror_simctl_start(
    target: scrcpy::MirrorTarget,
    opts: scrcpy::MirrorOptions,
    frame_channel: Channel<scrcpy::WireFrame>,
    registry: State<'_, Arc<SessionRegistry>>,
) -> Result<scrcpy::WireSession, String> {
    sck::start_sck_session(target, opts, frame_channel, registry.inner().clone()).await
}

#[tauri::command]
pub async fn mirror_simctl_stop(
    session_id: String,
    registry: State<'_, Arc<SessionRegistry>>,
) -> Result<(), String> {
    sck::stop_sck_session(&session_id, registry.inner().clone()).await
}

#[tauri::command]
pub async fn mirror_simctl_record(
    _session_id: String,
    _output_path: String,
) -> Result<(), String> {
    Err("recording during live mirror is not yet implemented for iOS Simulator".into())
}

#[tauri::command]
pub async fn mirror_simctl_stop_recording(_session_id: String) -> Result<(), String> {
    Err("recording during live mirror is not yet implemented for iOS Simulator".into())
}

#[tauri::command]
pub async fn mirror_simctl_clip(
    _session_id: String,
    _output_path: String,
    _start_ms: i64,
    _end_ms: i64,
) -> Result<String, String> {
    Err("clip extraction is not yet implemented for iOS Simulator".into())
}

/// Forwards a tap on the simulator mirror canvas to the connected
/// iOS driver via the sidecar. Only `up` is acted on (single tap on
/// release); `down` and `move` are accepted but ignored to keep the
/// XCUITest tap rate within its dispatch budget. The caller passes
/// normalized ratios in `[0, 1]`; the sidecar scales them against
/// the device's logical screen size.
///
/// Dispatches through `mirrorTapRatio` so the sidecar serializes
/// device selection and tap dispatch into a single atomic step —
/// a split `selectDevice` + `tapRatio` pair would race with any
/// concurrent selection (script runs, picker changes) and send
/// the tap to the wrong device.
///
/// `bundle_id` is optional. When provided, the sidecar performs a
/// `noReset` launch so the tap attaches to that app (useful when
/// the caller knows which app should own the tap). When omitted,
/// the XCUITest agent resolves the tap against Springboard's
/// coordinate space, which matches what the user sees on the
/// mirror canvas regardless of which app happens to be foreground.
#[tauri::command]
pub async fn mirror_simctl_send_touch(
    session_id: String,
    device_id: String,
    action: String,
    x_ratio: f64,
    y_ratio: f64,
    bundle_id: Option<String>,
    sidecar: State<'_, Arc<Sidecar>>,
) -> Result<(), String> {
    let _ = session_id;
    eprintln!(
        "[mirror] send_touch action={} xRatio={:.4} yRatio={:.4} bundle={:?} device={}",
        action, x_ratio, y_ratio, bundle_id, device_id
    );
    if action != "up" {
        return Ok(());
    }
    let mut params = json!({
        "deviceId": device_id,
        "xRatio": x_ratio,
        "yRatio": y_ratio,
    });
    if let Some(bundle) = bundle_id {
        if !bundle.is_empty() {
            params
                .as_object_mut()
                .expect("mirrorTapRatio params is an object")
                .insert("bundleId".into(), json!(bundle));
        }
    }
    sidecar
        .call("mirrorTapRatio", params.clone())
        .await
        .map(|_| {
            eprintln!("[mirror] mirrorTapRatio ok params={}", params);
        })
        .map_err(|e| {
            eprintln!("[mirror] mirrorTapRatio failed: {}", e);
            e.to_string()
        })
}

/// Forwards a streaming touch phase (down / move / up) directly to
/// the atomyx-sim-hid helper via the sidecar. Only valid for iOS
/// Simulator sessions backed by IosSimDriver (arm64 + Xcode 26+);
/// for other configurations the sidecar rejects with a clear error
/// so the UI can fall back to the classified touch path.
///
/// `phase` must be one of: "down", "move", "up".
/// `touch_id` is the per-finger identifier; use 1 for single-touch.
/// Coordinates are normalized ratios in [0, 1].
#[tauri::command]
pub async fn mirror_simctl_streaming_touch(
    session_id: String,
    device_id: String,
    phase: String,
    x_ratio: f64,
    y_ratio: f64,
    touch_id: u32,
    sidecar: State<'_, Arc<Sidecar>>,
) -> Result<(), String> {
    let _ = session_id;
    let rpc = match phase.as_str() {
        "down" => "mirrorStreamingTouchDown",
        "move" => "mirrorStreamingTouchMove",
        "up" => "mirrorStreamingTouchUp",
        other => return Err(format!("unknown streaming touch phase: {other}")),
    };
    let params = serde_json::json!({
        "deviceId": device_id,
        "xRatio": x_ratio,
        "yRatio": y_ratio,
        "touchId": touch_id,
    });
    sidecar
        .call(rpc, params)
        .await
        .map(|_| ())
        .map_err(|e| e.to_string())
}

/// Forwards a long-press gesture from the simulator mirror canvas
/// to the connected iOS driver. The XCUITest agent presses at the
/// requested point for `durationMs` (default 500ms when None) and
/// releases. `bundleId` follows the same noReset semantics as
/// `mirror_simctl_send_touch`.
#[tauri::command]
pub async fn mirror_simctl_long_press(
    session_id: String,
    device_id: String,
    x_ratio: f64,
    y_ratio: f64,
    duration_ms: Option<u32>,
    bundle_id: Option<String>,
    sidecar: State<'_, Arc<Sidecar>>,
) -> Result<(), String> {
    let _ = session_id;
    eprintln!(
        "[mirror] long_press xRatio={:.4} yRatio={:.4} durationMs={:?} bundle={:?} device={}",
        x_ratio, y_ratio, duration_ms, bundle_id, device_id
    );
    let mut params = json!({
        "deviceId": device_id,
        "xRatio": x_ratio,
        "yRatio": y_ratio,
    });
    if let Some(d) = duration_ms {
        params
            .as_object_mut()
            .expect("mirrorLongPressRatio params is an object")
            .insert("durationMs".into(), json!(d));
    }
    if let Some(bundle) = bundle_id {
        if !bundle.is_empty() {
            params
                .as_object_mut()
                .expect("mirrorLongPressRatio params is an object")
                .insert("bundleId".into(), json!(bundle));
        }
    }
    sidecar
        .call("mirrorLongPressRatio", params.clone())
        .await
        .map(|_| {
            eprintln!("[mirror] mirrorLongPressRatio ok params={}", params);
        })
        .map_err(|e| {
            eprintln!("[mirror] mirrorLongPressRatio failed: {}", e);
            e.to_string()
        })
}

/// Forwards a swipe gesture from the simulator mirror canvas to
/// the connected iOS driver. Coordinates are normalized ratios in
/// `[0, 1]`; the sidecar scales them against the device's logical
/// screen size. `durationMs` defaults to 200ms when omitted.
#[tauri::command]
pub async fn mirror_simctl_swipe(
    session_id: String,
    device_id: String,
    from_x_ratio: f64,
    from_y_ratio: f64,
    to_x_ratio: f64,
    to_y_ratio: f64,
    duration_ms: Option<u32>,
    bundle_id: Option<String>,
    sidecar: State<'_, Arc<Sidecar>>,
) -> Result<(), String> {
    let _ = session_id;
    eprintln!(
        "[mirror] swipe from=({:.4},{:.4}) to=({:.4},{:.4}) durationMs={:?} bundle={:?} device={}",
        from_x_ratio, from_y_ratio, to_x_ratio, to_y_ratio, duration_ms, bundle_id, device_id
    );
    let mut params = json!({
        "deviceId": device_id,
        "fromXRatio": from_x_ratio,
        "fromYRatio": from_y_ratio,
        "toXRatio": to_x_ratio,
        "toYRatio": to_y_ratio,
    });
    if let Some(d) = duration_ms {
        params
            .as_object_mut()
            .expect("mirrorSwipeRatio params is an object")
            .insert("durationMs".into(), json!(d));
    }
    if let Some(bundle) = bundle_id {
        if !bundle.is_empty() {
            params
                .as_object_mut()
                .expect("mirrorSwipeRatio params is an object")
                .insert("bundleId".into(), json!(bundle));
        }
    }
    sidecar
        .call("mirrorSwipeRatio", params.clone())
        .await
        .map(|_| {
            eprintln!("[mirror] mirrorSwipeRatio ok params={}", params);
        })
        .map_err(|e| {
            eprintln!("[mirror] mirrorSwipeRatio failed: {}", e);
            e.to_string()
        })
}

#[tauri::command]
pub async fn mirror_pinch(
    session_id: String,
    device_id: String,
    center_x_ratio: f64,
    center_y_ratio: f64,
    from_scale: f64,
    to_scale: f64,
    duration_ms: Option<u32>,
    bundle_id: Option<String>,
    sidecar: State<'_, Arc<Sidecar>>,
) -> Result<(), String> {
    let _ = session_id;
    eprintln!(
        "[mirror] pinch center=({:.4},{:.4}) scale={:.2}->{:.2} durationMs={:?} bundle={:?} device={}",
        center_x_ratio, center_y_ratio, from_scale, to_scale, duration_ms, bundle_id, device_id
    );
    let mut params = json!({
        "deviceId": device_id,
        "centerXRatio": center_x_ratio,
        "centerYRatio": center_y_ratio,
        "fromScale": from_scale,
        "toScale": to_scale,
    });
    if let Some(d) = duration_ms {
        params
            .as_object_mut()
            .expect("mirrorPinch params is an object")
            .insert("durationMs".into(), json!(d));
    }
    if let Some(bundle) = bundle_id {
        if !bundle.is_empty() {
            params
                .as_object_mut()
                .expect("mirrorPinch params is an object")
                .insert("bundleId".into(), json!(bundle));
        }
    }
    sidecar
        .call("mirrorPinch", params.clone())
        .await
        .map(|_| {
            eprintln!("[mirror] mirrorPinch ok params={}", params);
        })
        .map_err(|e| {
            eprintln!("[mirror] mirrorPinch failed: {}", e);
            e.to_string()
        })
}

#[tauri::command]
pub async fn mirror_input_text(
    session_id: String,
    device_id: String,
    text: String,
    sidecar: State<'_, Arc<Sidecar>>,
) -> Result<(), String> {
    let _ = session_id;
    eprintln!("[mirror] input_text len={} device={}", text.len(), device_id);
    let params = json!({ "deviceId": device_id, "text": text });
    sidecar
        .call("mirrorInputText", params)
        .await
        .map(|_| ())
        .map_err(|e| {
            eprintln!("[mirror] mirrorInputText failed: {}", e);
            e.to_string()
        })
}

#[tauri::command]
pub async fn mirror_erase_text(
    session_id: String,
    device_id: String,
    count: u32,
    sidecar: State<'_, Arc<Sidecar>>,
) -> Result<(), String> {
    let _ = session_id;
    eprintln!("[mirror] erase_text count={} device={}", count, device_id);
    let params = json!({ "deviceId": device_id, "count": count });
    sidecar
        .call("mirrorEraseText", params)
        .await
        .map(|_| ())
        .map_err(|e| {
            eprintln!("[mirror] mirrorEraseText failed: {}", e);
            e.to_string()
        })
}

#[tauri::command]
pub async fn mirror_press_key(
    session_id: String,
    device_id: String,
    key: String,
    sidecar: State<'_, Arc<Sidecar>>,
) -> Result<(), String> {
    let _ = session_id;
    eprintln!("[mirror] press_key key={} device={}", key, device_id);
    let params = json!({ "deviceId": device_id, "key": key });
    sidecar
        .call("mirrorPressKey", params)
        .await
        .map(|_| ())
        .map_err(|e| {
            eprintln!("[mirror] mirrorPressKey failed: {}", e);
            e.to_string()
        })
}

