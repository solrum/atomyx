//! Atomyx Studio Tauri backend.
//!
//! Responsibilities kept intentionally narrow:
//!   - Own the core-runtime child process — a Node sidecar
//!     bundled as `sidecar.cjs` that loads `@atomyx/driver` +
//!     `@atomyx/script` in-process. Forwards Studio's `runtime_*`
//!     invokes as line-delimited JSON-RPC over stdio and streams
//!     events back via Tauri Channels.
//!   - Own the on-disk Studio artifact store + settings file +
//!     themes dir.
//!   - Expose a native folder picker + script read/write.

use serde::{Deserialize, Serialize};

pub mod commands;
pub mod logs;
pub mod mirror;
pub mod sidecar;

use commands::*;
use mirror::{
    mirror_scrcpy_clip, mirror_scrcpy_record, mirror_scrcpy_send_touch, mirror_scrcpy_start,
    mirror_scrcpy_stop, mirror_scrcpy_stop_recording, mirror_simctl_clip,
    mirror_simctl_get_endpoint, mirror_simctl_long_press, mirror_simctl_record,
    mirror_simctl_send_touch, mirror_simctl_start, mirror_simctl_stop,
    mirror_erase_text, mirror_input_text, mirror_pinch, mirror_press_key,
    mirror_simctl_stop_recording, mirror_simctl_streaming_touch,
    mirror_simctl_swipe, SessionRegistry,
};
use sidecar::Sidecar;
use std::sync::Arc;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .setup(|app| {
            use tauri::Manager;
            // Wire the logs fan-out before any subsystem starts so
            // early lines (sidecar boot, mirror startup) reach the
            // Logs tool window once it mounts.
            logs::install(app.handle().clone());

            // Shared mirror session registry. Every
            // mirror_scrcpy_* command takes it as managed state.
            app.manage::<Arc<SessionRegistry>>(Arc::new(SessionRegistry::new()));

            // Spawn the Node sidecar once per Studio process. Every
            // runtime_* command borrows it as managed state.
            let handle = app.handle().clone();
            tauri::async_runtime::spawn(async move {
                match Sidecar::spawn(Some(&handle)).await {
                    Ok(sc) => {
                        handle.manage::<Arc<Sidecar>>(sc);
                    }
                    Err(e) => {
                        crate::log_error!("studio", "sidecar startup failed: {}", e);
                    }
                }
            });
            Ok(())
        })
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_fs::init())
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_updater::Builder::new().build())
        .invoke_handler(tauri::generate_handler![
            runtime_connect,
            runtime_disconnect,
            runtime_list_devices,
            runtime_list_apps,
            runtime_current_foreground,
            ios_agent_ensure,
            ios_agent_status,
            ios_sim_hid_status,
            android_agent_ensure,
            android_agent_status,
            crate::logs::log_emit,
            runtime_run_script,
            runtime_stop_script,
            runtime_screenshot,
            runtime_get_ui_tree,
            artifacts_create_run,
            artifacts_append_event,
            artifacts_save,
            artifacts_finalize_run,
            artifacts_list_runs,
            artifacts_get_run,
            artifacts_get_events,
            artifacts_list,
            artifacts_read,
            artifacts_delete_run,
            settings_load,
            settings_save,
            workspace_open_folder,
            workspace_read_script,
            workspace_write_script,
            workspace_create_script,
            workspace_create_directory,
            workspace_delete_script,
            workspace_rename_script,
            themes_list_builtin,
            themes_list_user,
            themes_read,
            themes_write,
            themes_delete,
            themes_open_dir,
            themes_watch,
            projects_list,
            projects_touch,
            projects_set_pinned,
            projects_remove,
            window_open_workspace,
            project_config_read_json,
            project_config_write_json,
            project_config_read_text,
            project_config_write_text,
            project_config_list_json_directory,
            workspace_search,
            workspace_scan_todos,
            workspace_watch,
            workspace_unwatch,
            terminal_spawn,
            terminal_write,
            terminal_resize,
            terminal_kill,
            mirror_scrcpy_start,
            mirror_scrcpy_stop,
            mirror_scrcpy_record,
            mirror_scrcpy_stop_recording,
            mirror_scrcpy_clip,
            mirror_scrcpy_send_touch,
            mirror_simctl_start,
            mirror_simctl_stop,
            mirror_simctl_get_endpoint,
            mirror_simctl_record,
            mirror_simctl_stop_recording,
            mirror_simctl_clip,
            mirror_simctl_send_touch,
            mirror_simctl_streaming_touch,
            mirror_simctl_long_press,
            mirror_simctl_swipe,
            mirror_pinch,
            mirror_input_text,
            mirror_erase_text,
            mirror_press_key,
        ])
        .run(tauri::generate_context!())
        .expect("error while running Atomyx Studio");
}

#[derive(Debug, thiserror::Error)]
pub enum StudioError {
    #[error("invalid argument: {0}")]
    InvalidArgument(String),
    #[error("io: {0}")]
    Io(#[from] std::io::Error),
    #[error("{0}")]
    Message(String),
}

impl Serialize for StudioError {
    fn serialize<S: serde::Serializer>(&self, s: S) -> Result<S::Ok, S::Error> {
        s.serialize_str(&self.to_string())
    }
}

pub type StudioResult<T> = Result<T, StudioError>;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Device {
    pub id: String,
    pub platform: String,
    pub kind: String,
    pub name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub model: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none", rename = "osVersion")]
    pub os_version: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct App {
    pub id: String,
    pub name: String,
}
