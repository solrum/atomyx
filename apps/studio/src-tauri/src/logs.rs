//! Centralized log fan-out for Studio.
//!
//! Every backend subsystem (sidecar capture, mirror adapters,
//! Studio bootstrap) routes its diagnostic output through
//! `logs::emit`, which:
//!
//!   1. echoes the line to stderr so a developer running
//!      `pnpm tauri dev` still sees it in the terminal, and
//!   2. emits the `logs://entry` Tauri event so the Logs tool
//!      window can render it.
//!
//! The webview proxies its own `console.*` calls into the same
//! store via the `log_emit` command below, giving the Logs tab a
//! single unified stream.

use std::sync::OnceLock;
use std::time::{SystemTime, UNIX_EPOCH};

use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Emitter, Manager};

static APP: OnceLock<AppHandle> = OnceLock::new();

#[derive(Debug, Clone, Copy, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum LogLevel {
    Debug,
    Info,
    Warn,
    Error,
}

impl LogLevel {
    fn as_str(self) -> &'static str {
        match self {
            LogLevel::Debug => "debug",
            LogLevel::Info => "info",
            LogLevel::Warn => "warn",
            LogLevel::Error => "error",
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LogEntry {
    pub id: String,
    pub ts: i64,
    pub source: String,
    pub level: String,
    pub message: String,
}

pub fn install(handle: AppHandle) {
    let _ = APP.set(handle);
}

pub fn emit(source: &str, level: LogLevel, message: impl Into<String>) {
    let message = message.into();
    eprintln!("[{}] {}", source, message);
    let entry = LogEntry {
        id: uuid::Uuid::new_v4().to_string(),
        ts: SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|d| d.as_millis() as i64)
            .unwrap_or(0),
        source: source.to_string(),
        level: level.as_str().to_string(),
        message,
    };
    if let Some(app) = APP.get() {
        let _ = app.emit("logs://entry", entry);
    }
}

#[macro_export]
macro_rules! log_info {
    ($source:expr, $($arg:tt)*) => {
        $crate::logs::emit($source, $crate::logs::LogLevel::Info, format!($($arg)*))
    };
}

#[macro_export]
macro_rules! log_warn {
    ($source:expr, $($arg:tt)*) => {
        $crate::logs::emit($source, $crate::logs::LogLevel::Warn, format!($($arg)*))
    };
}

#[macro_export]
macro_rules! log_error {
    ($source:expr, $($arg:tt)*) => {
        $crate::logs::emit($source, $crate::logs::LogLevel::Error, format!($($arg)*))
    };
}

#[macro_export]
macro_rules! log_debug {
    ($source:expr, $($arg:tt)*) => {
        $crate::logs::emit($source, $crate::logs::LogLevel::Debug, format!($($arg)*))
    };
}

/// Webview console proxy → backend store. Exposed via
/// `tauri::generate_handler!` so the UI can re-emit its own logs.
#[tauri::command]
pub fn log_emit(
    source: String,
    level: String,
    message: String,
) -> Result<(), String> {
    let lvl = match level.as_str() {
        "debug" => LogLevel::Debug,
        "warn" => LogLevel::Warn,
        "error" => LogLevel::Error,
        _ => LogLevel::Info,
    };
    emit(&source, lvl, message);
    Ok(())
}

#[allow(dead_code)]
pub fn handle() -> Option<AppHandle> {
    APP.get().cloned()
}

#[allow(dead_code)]
pub fn from_app(app: &impl Manager<tauri::Wry>) -> Option<AppHandle> {
    Some(app.app_handle().clone())
}
