//! Atomyx runtime sidecar process manager.
//!
//! Spawns the Atomyx Go runtime binary directly and owns the stdio
//! pipes, routing JSON-RPC traffic:
//!
//!   - responses (with `id`) resolve the matching pending future
//!   - events (with `event`) are fanned out on a broadcast channel
//!   - stderr is tee'd to the Studio stderr for debugging
//!
//! The manager is registered as Tauri state once at startup; every
//! `runtime_*` command borrows it and awaits a single round-trip.

use std::collections::HashMap;
use std::path::PathBuf;
use std::process::Stdio;
use std::sync::Arc;

use serde_json::{json, Value};
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::process::{Child, ChildStdin, Command};
use tokio::sync::{broadcast, oneshot, Mutex};

use crate::{StudioError, StudioResult};

type PendingMap = Arc<Mutex<HashMap<String, oneshot::Sender<SidecarResponse>>>>;

#[derive(Debug)]
enum SidecarResponse {
    Ok(Value),
    Err { code: String, message: String },
}

pub struct Sidecar {
    writer: Mutex<ChildStdin>,
    pending: PendingMap,
    events: broadcast::Sender<Value>,
    _child: Mutex<Child>,
}

impl Sidecar {
    pub async fn spawn(app_handle: Option<&tauri::AppHandle>) -> StudioResult<Arc<Self>> {
        let binary = resolve_runtime_binary(app_handle)?;
        ensure_executable(&binary);

        let mut cmd = Command::new(&binary);
        cmd.stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .kill_on_drop(true);

        let mut child = cmd.spawn().map_err(|e| {
            StudioError::Message(format!(
                "failed to spawn sidecar ({}): {}",
                binary.display(),
                e
            ))
        })?;

        let stdin = child.stdin.take().ok_or_else(|| {
            StudioError::Message("sidecar stdin unavailable".into())
        })?;
        let stdout = child.stdout.take().ok_or_else(|| {
            StudioError::Message("sidecar stdout unavailable".into())
        })?;
        let stderr = child.stderr.take().ok_or_else(|| {
            StudioError::Message("sidecar stderr unavailable".into())
        })?;

        let pending: PendingMap = Arc::new(Mutex::new(HashMap::new()));
        let (events_tx, _) = broadcast::channel::<Value>(256);

        // Response / event reader.
        let pending_reader = pending.clone();
        let events_reader = events_tx.clone();
        tokio::spawn(async move {
            let mut lines = BufReader::new(stdout).lines();
            loop {
                match lines.next_line().await {
                    Ok(Some(line)) => {
                        handle_line(&line, &pending_reader, &events_reader).await;
                    }
                    Ok(None) => break,
                    Err(_) => break,
                }
            }
        });

        // Stderr tee — protocol uses stdout exclusively. Every line
        // also flows into the centralized Logs store so the Logs
        // tool window shows sidecar output without round-tripping
        // through the terminal.
        tokio::spawn(async move {
            let mut lines = BufReader::new(stderr).lines();
            while let Ok(Some(line)) = lines.next_line().await {
                let level = classify_level(&line);
                crate::logs::emit("sidecar", level, line);
            }
        });

        let sidecar = Arc::new(Self {
            writer: Mutex::new(stdin),
            pending,
            events: events_tx,
            _child: Mutex::new(child),
        });

        // Handshake: handshake failure is fatal for startup but does
        // not kill Studio — commands will surface NotConnected when
        // the handshake promise is awaited downstream.
        let _ = sidecar.call("protocolVersion", Value::Null).await;

        Ok(sidecar)
    }

    pub async fn call(&self, method: &str, params: Value) -> StudioResult<Value> {
        let id = uuid::Uuid::new_v4().to_string();
        let (tx, rx) = oneshot::channel();
        self.pending.lock().await.insert(id.clone(), tx);

        let req = json!({
            "id": id,
            "method": method,
            "params": if params.is_null() { Value::Object(Default::default()) } else { params },
        });
        let mut line = serde_json::to_vec(&req).map_err(|e| StudioError::Message(e.to_string()))?;
        line.push(b'\n');
        self.writer
            .lock()
            .await
            .write_all(&line)
            .await
            .map_err(|e| StudioError::Message(format!("sidecar write failed: {}", e)))?;

        match rx.await {
            Ok(SidecarResponse::Ok(result)) => Ok(result),
            Ok(SidecarResponse::Err { code, message }) => {
                Err(StudioError::Message(format!("{}: {}", code, message)))
            }
            Err(_) => {
                self.pending.lock().await.remove(&id);
                Err(StudioError::Message(
                    "sidecar closed before sending response".into(),
                ))
            }
        }
    }

    pub fn subscribe_events(&self) -> broadcast::Receiver<Value> {
        self.events.subscribe()
    }
}

fn classify_level(line: &str) -> crate::logs::LogLevel {
    let lower = line.to_ascii_lowercase();
    if lower.contains("error") || lower.contains("fatal") || lower.contains("failed") {
        crate::logs::LogLevel::Error
    } else if lower.contains("warn") {
        crate::logs::LogLevel::Warn
    } else {
        crate::logs::LogLevel::Info
    }
}

async fn handle_line(
    line: &str,
    pending: &PendingMap,
    events: &broadcast::Sender<Value>,
) {
    let trimmed = line.trim();
    if trimmed.is_empty() {
        return;
    }
    let Ok(msg) = serde_json::from_str::<Value>(trimmed) else {
        return;
    };
    if let Some(id) = msg.get("id").and_then(|v| v.as_str()) {
        let mut map = pending.lock().await;
        if let Some(tx) = map.remove(id) {
            let response = if let Some(err) = msg.get("error") {
                let code = err
                    .get("code")
                    .and_then(|v| v.as_str())
                    .unwrap_or("InternalError")
                    .to_string();
                let message = err
                    .get("message")
                    .and_then(|v| v.as_str())
                    .unwrap_or("unknown sidecar error")
                    .to_string();
                SidecarResponse::Err { code, message }
            } else {
                SidecarResponse::Ok(msg.get("result").cloned().unwrap_or(Value::Null))
            };
            let _ = tx.send(response);
        }
    } else if msg.get("event").is_some() {
        let _ = events.send(msg);
    }
}

/// Best-effort executable-bit guarantee. A bundled resource copy of
/// the binary can lose its mode bits during packaging, so restore
/// the owner-execute bit before every spawn attempt. Failures are
/// ignored: a missing or unreadable file is caught by the spawn
/// call right after, which reports a clearer error.
#[cfg(unix)]
fn ensure_executable(path: &std::path::Path) {
    use std::os::unix::fs::PermissionsExt;
    if let Ok(meta) = std::fs::metadata(path) {
        let mut perms = meta.permissions();
        if perms.mode() & 0o100 == 0 {
            perms.set_mode(0o755);
            let _ = std::fs::set_permissions(path, perms);
        }
    }
}

#[cfg(not(unix))]
fn ensure_executable(_path: &std::path::Path) {}

fn resolve_runtime_binary(app_handle: Option<&tauri::AppHandle>) -> StudioResult<PathBuf> {
    // Explicit override wins for CI / integration tests.
    if let Ok(p) = std::env::var("ATOMYX_RUNTIME_BIN") {
        let path = PathBuf::from(p);
        if path.exists() {
            return Ok(path);
        }
    }

    // Production: Tauri-bundled resource under Resources/atomyx-runtime.
    if let Some(app) = app_handle {
        use tauri::Manager;
        if let Ok(res) = app.path().resource_dir() {
            let bundled = res.join("atomyx-runtime");
            if bundled.exists() {
                return Ok(bundled);
            }
        }
    }

    // Development layout: the repo-root core build output, discovered
    // by walking up from the process cwd until core/bin/atomyx-runtime
    // is found.
    let here = std::env::current_dir()
        .map_err(|e| StudioError::Message(e.to_string()))?;
    let candidates = [
        here.join("../../core/bin/atomyx-runtime"),
        here.join("../../../core/bin/atomyx-runtime"),
        here.join("core/bin/atomyx-runtime"),
    ];
    for c in candidates {
        if c.exists() {
            return Ok(c);
        }
    }

    Err(StudioError::Message(
        "runtime binary not found — set ATOMYX_RUNTIME_BIN, run `make build-core`, or install Atomyx Studio from a bundle".into(),
    ))
}
