//! IPC command surface. `runtime_*` delegates to the Atomyx core
//! runtime (driver + script engine) through the Sidecar process
//! manager — each command awaits a JSON-RPC round-trip over the
//! sidecar's stdio pipes. `settings_*`, `artifacts_*`,
//! `workspace_*`, and `themes_*` have real filesystem
//! implementations.

use crate::sidecar::Sidecar;
use crate::{App, Device, StudioError, StudioResult};
use serde_json::{json, Value};
use std::sync::Arc;
use tauri::ipc::Channel;
use tauri::State;

fn device_from_descriptor(v: &Value) -> Device {
    Device {
        id: v.get("id").and_then(|x| x.as_str()).unwrap_or("").to_string(),
        platform: v
            .get("platform")
            .and_then(|x| x.as_str())
            .unwrap_or("")
            .to_string(),
        kind: v
            .get("kind")
            .and_then(|x| x.as_str())
            .unwrap_or("device")
            .to_string(),
        name: v
            .get("name")
            .and_then(|x| x.as_str())
            .unwrap_or("")
            .to_string(),
        model: None,
        os_version: None,
    }
}

#[tauri::command]
pub async fn runtime_connect(sidecar: State<'_, Arc<Sidecar>>) -> StudioResult<()> {
    // Handshake: ping the sidecar so startup errors surface early.
    sidecar.call("ping", Value::Null).await.map(|_| ())
}

#[tauri::command]
pub async fn runtime_disconnect(
    sidecar: State<'_, Arc<Sidecar>>,
) -> StudioResult<()> {
    sidecar
        .call("deselectDevice", Value::Null)
        .await
        .map(|_| ())
}

#[tauri::command]
pub async fn runtime_list_devices(
    sidecar: State<'_, Arc<Sidecar>>,
) -> StudioResult<Vec<Device>> {
    let result = sidecar.call("listDevices", Value::Null).await?;
    let arr = result.as_array().cloned().unwrap_or_default();
    Ok(arr.iter().map(device_from_descriptor).collect())
}

#[tauri::command]
pub async fn runtime_list_apps(
    device_id: String,
    sidecar: State<'_, Arc<Sidecar>>,
) -> StudioResult<Vec<App>> {
    // Select the device if not already selected; harmless if it is.
    let _ = sidecar
        .call("selectDevice", json!({ "id": device_id }))
        .await?;
    let result = sidecar.call("listApps", Value::Null).await?;
    let arr = result.as_array().cloned().unwrap_or_default();
    Ok(arr
        .iter()
        .map(|v| App {
            id: v
                .get("bundleId")
                .and_then(|x| x.as_str())
                .unwrap_or("")
                .to_string(),
            name: v
                .get("displayName")
                .and_then(|x| x.as_str())
                .unwrap_or("")
                .to_string(),
        })
        .collect())
}

#[tauri::command]
pub async fn ios_agent_ensure(
    udid: String,
    kind: String,
    sidecar: State<'_, Arc<Sidecar>>,
) -> StudioResult<Value> {
    eprintln!("[ios-agent] ensure udid={} kind={}", udid, kind);
    sidecar
        .call("ensureIosAgent", json!({ "udid": udid, "kind": kind }))
        .await
}

#[tauri::command]
pub async fn ios_agent_status(
    udid: String,
    sidecar: State<'_, Arc<Sidecar>>,
) -> StudioResult<Value> {
    sidecar.call("iosAgentStatus", json!({ "udid": udid })).await
}

#[tauri::command]
pub async fn ios_sim_hid_status(
    udid: String,
    sidecar: State<'_, Arc<Sidecar>>,
) -> StudioResult<Value> {
    sidecar
        .call("iosSimHidStatus", json!({ "udid": udid }))
        .await
}

#[tauri::command]
pub async fn android_agent_ensure(
    serial: String,
    sidecar: State<'_, Arc<Sidecar>>,
) -> StudioResult<Value> {
    sidecar
        .call("ensureAndroidAgent", json!({ "serial": serial }))
        .await
}

#[tauri::command]
pub async fn android_agent_status(
    serial: String,
    sidecar: State<'_, Arc<Sidecar>>,
) -> StudioResult<Value> {
    sidecar
        .call("androidAgentStatus", json!({ "serial": serial }))
        .await
}

#[tauri::command]
pub async fn runtime_current_foreground(
    device_id: String,
    sidecar: State<'_, Arc<Sidecar>>,
) -> StudioResult<Option<String>> {
    let _ = sidecar
        .call("selectDevice", json!({ "id": device_id }))
        .await?;
    let result = sidecar.call("currentForeground", Value::Null).await?;
    Ok(result
        .get("bundleId")
        .and_then(|v| v.as_str())
        .map(|s| s.to_string()))
}

#[tauri::command]
pub async fn runtime_run_script(
    yaml: String,
    opts: Value,
    cwd: Option<String>,
    on_event: Channel<Value>,
    sidecar: State<'_, Arc<Sidecar>>,
) -> StudioResult<()> {
    let device_id = opts.get("deviceId").and_then(|v| v.as_str());
    if let Some(id) = device_id {
        let _ = sidecar
            .call("selectDevice", json!({ "id": id }))
            .await?;
    }
    let mut events = sidecar.subscribe_events();

    // Forward every event emitted during this run to the renderer.
    // The subscriber closes when the runScript call completes.
    let channel = on_event.clone();
    tokio::spawn(async move {
        while let Ok(event) = events.recv().await {
            if channel.send(event).is_err() {
                break;
            }
        }
    });

    let mut params = json!({ "yaml": yaml });
    if let Some(c) = cwd {
        if let Some(obj) = params.as_object_mut() {
            obj.insert("cwd".to_string(), Value::String(c));
        }
    }
    sidecar
        .call("runScript", params)
        .await
        .map(|_| ())
}

#[tauri::command]
pub async fn runtime_stop_script(
    sidecar: State<'_, Arc<Sidecar>>,
) -> StudioResult<()> {
    sidecar.call("stopScript", Value::Null).await.map(|_| ())
}

#[tauri::command]
pub async fn runtime_screenshot(
    device_id: String,
    sidecar: State<'_, Arc<Sidecar>>,
) -> StudioResult<Vec<u8>> {
    let _ = sidecar
        .call("selectDevice", json!({ "id": device_id }))
        .await?;
    let result = sidecar.call("screenshot", Value::Null).await?;
    let b64 = result
        .get("bytesBase64")
        .and_then(|v| v.as_str())
        .ok_or_else(|| StudioError::Message("screenshot response missing bytesBase64".into()))?;
    use base64::{engine::general_purpose::STANDARD, Engine};
    STANDARD
        .decode(b64)
        .map_err(|e| StudioError::Message(format!("screenshot decode failed: {}", e)))
}

#[tauri::command]
pub async fn runtime_get_ui_tree(
    device_id: String,
    sidecar: State<'_, Arc<Sidecar>>,
) -> StudioResult<Value> {
    let _ = sidecar
        .call("selectDevice", json!({ "id": device_id }))
        .await?;
    sidecar.call("getUiTree", json!({ "fresh": true })).await
}

fn runs_root(app: &tauri::AppHandle) -> StudioResult<std::path::PathBuf> {
    use tauri::Manager;
    let dir = app
        .path()
        .app_config_dir()
        .map_err(|e| StudioError::Message(e.to_string()))?;
    let runs = dir.join("runs");
    if !runs.exists() {
        std::fs::create_dir_all(&runs)?;
    }
    Ok(runs)
}

fn run_dir(app: &tauri::AppHandle, run_id: &str) -> StudioResult<std::path::PathBuf> {
    if run_id.contains('/') || run_id.contains("..") {
        return Err(StudioError::InvalidArgument(format!(
            "unsafe run id: {}",
            run_id
        )));
    }
    let path = runs_root(app)?.join(run_id);
    Ok(path)
}

#[tauri::command]
pub async fn artifacts_create_run(
    app: tauri::AppHandle,
    meta: Value,
) -> StudioResult<()> {
    let run_id = meta
        .get("runId")
        .and_then(|v| v.as_str())
        .ok_or_else(|| StudioError::InvalidArgument("runId missing".into()))?;
    let dir = run_dir(&app, run_id)?;
    std::fs::create_dir_all(dir.join("artifacts"))?;
    let meta_str = serde_json::to_string_pretty(&meta)
        .map_err(|e| StudioError::Message(e.to_string()))?;
    std::fs::write(dir.join("meta.json"), meta_str)?;
    std::fs::write(dir.join("steps.jsonl"), "")?;
    std::fs::write(dir.join("console.log"), "")?;
    Ok(())
}

#[tauri::command]
pub async fn artifacts_append_event(
    app: tauri::AppHandle,
    run_id: String,
    event: Value,
) -> StudioResult<()> {
    use std::io::Write;
    let dir = run_dir(&app, &run_id)?;
    if !dir.exists() {
        return Err(StudioError::InvalidArgument(format!(
            "run {} not created",
            run_id
        )));
    }
    let line = serde_json::to_string(&event)
        .map_err(|e| StudioError::Message(e.to_string()))?;
    let mut file = std::fs::OpenOptions::new()
        .append(true)
        .create(true)
        .open(dir.join("steps.jsonl"))?;
    writeln!(file, "{}", line)?;
    if let Some(msg) = event.get("line").and_then(|v| v.as_str()) {
        let mut log = std::fs::OpenOptions::new()
            .append(true)
            .create(true)
            .open(dir.join("console.log"))?;
        writeln!(log, "{}", msg)?;
    }
    Ok(())
}

#[tauri::command]
pub async fn artifacts_save(
    app: tauri::AppHandle,
    run_id: String,
    name: String,
    bytes: Vec<u8>,
    extras: Value,
) -> StudioResult<Value> {
    if name.contains('/') || name.contains("..") {
        return Err(StudioError::InvalidArgument(format!(
            "unsafe artifact name: {}",
            name
        )));
    }
    let dir = run_dir(&app, &run_id)?;
    std::fs::create_dir_all(dir.join("artifacts"))?;
    let path = dir.join("artifacts").join(&name);
    std::fs::write(&path, &bytes)?;
    let mut meta = serde_json::json!({
        "name": name,
        "size": bytes.len(),
    });
    if let Some(obj) = meta.as_object_mut() {
        if let Some(extras_obj) = extras.as_object() {
            for (k, v) in extras_obj {
                obj.insert(k.clone(), v.clone());
            }
        }
    }
    Ok(meta)
}

#[tauri::command]
pub async fn artifacts_finalize_run(
    app: tauri::AppHandle,
    run_id: String,
    patch: Value,
) -> StudioResult<()> {
    let dir = run_dir(&app, &run_id)?;
    let meta_path = dir.join("meta.json");
    let mut meta: Value = if meta_path.exists() {
        let raw = std::fs::read_to_string(&meta_path)?;
        serde_json::from_str(&raw).unwrap_or_else(|_| serde_json::json!({}))
    } else {
        serde_json::json!({})
    };
    if let (Some(meta_obj), Some(patch_obj)) = (meta.as_object_mut(), patch.as_object()) {
        for (k, v) in patch_obj {
            meta_obj.insert(k.clone(), v.clone());
        }
    }
    let pretty = serde_json::to_string_pretty(&meta)
        .map_err(|e| StudioError::Message(e.to_string()))?;
    std::fs::write(&meta_path, pretty)?;
    Ok(())
}

#[tauri::command]
pub async fn artifacts_list_runs(
    app: tauri::AppHandle,
) -> StudioResult<Vec<Value>> {
    let root = runs_root(&app)?;
    let mut out: Vec<Value> = Vec::new();
    if !root.exists() {
        return Ok(out);
    }
    for entry in std::fs::read_dir(&root)? {
        let entry = entry?;
        if !entry.file_type()?.is_dir() {
            continue;
        }
        let meta_path = entry.path().join("meta.json");
        if !meta_path.exists() {
            continue;
        }
        if let Ok(raw) = std::fs::read_to_string(&meta_path) {
            if let Ok(json) = serde_json::from_str::<Value>(&raw) {
                out.push(json);
            }
        }
    }
    // Sort by startedAt descending so the newest run is first.
    out.sort_by(|a, b| {
        let ai = a.get("startedAt").and_then(|v| v.as_i64()).unwrap_or(0);
        let bi = b.get("startedAt").and_then(|v| v.as_i64()).unwrap_or(0);
        bi.cmp(&ai)
    });
    Ok(out)
}

#[tauri::command]
pub async fn artifacts_get_run(
    app: tauri::AppHandle,
    run_id: String,
) -> StudioResult<Option<Value>> {
    let dir = run_dir(&app, &run_id)?;
    let meta_path = dir.join("meta.json");
    if !meta_path.exists() {
        return Ok(None);
    }
    let raw = std::fs::read_to_string(&meta_path)?;
    let json: Value = serde_json::from_str(&raw)
        .map_err(|e| StudioError::Message(e.to_string()))?;
    Ok(Some(json))
}

#[tauri::command]
pub async fn artifacts_get_events(
    app: tauri::AppHandle,
    run_id: String,
) -> StudioResult<Vec<Value>> {
    let dir = run_dir(&app, &run_id)?;
    let path = dir.join("steps.jsonl");
    if !path.exists() {
        return Ok(vec![]);
    }
    let raw = std::fs::read_to_string(&path)?;
    let mut out: Vec<Value> = Vec::new();
    for line in raw.lines() {
        if line.trim().is_empty() {
            continue;
        }
        if let Ok(v) = serde_json::from_str::<Value>(line) {
            out.push(v);
        }
    }
    Ok(out)
}

#[tauri::command]
pub async fn artifacts_list(
    app: tauri::AppHandle,
    run_id: String,
) -> StudioResult<Vec<Value>> {
    let dir = run_dir(&app, &run_id)?.join("artifacts");
    if !dir.exists() {
        return Ok(vec![]);
    }
    let mut out: Vec<Value> = Vec::new();
    for entry in std::fs::read_dir(&dir)? {
        let entry = entry?;
        if !entry.file_type()?.is_file() {
            continue;
        }
        let name = entry.file_name().to_string_lossy().into_owned();
        let size = entry.metadata()?.len();
        out.push(serde_json::json!({
            "name": name,
            "size": size,
        }));
    }
    Ok(out)
}

#[tauri::command]
pub async fn artifacts_read(
    app: tauri::AppHandle,
    run_id: String,
    name: String,
) -> StudioResult<Vec<u8>> {
    if name.contains('/') || name.contains("..") {
        return Err(StudioError::InvalidArgument(format!(
            "unsafe artifact name: {}",
            name
        )));
    }
    let path = run_dir(&app, &run_id)?.join("artifacts").join(&name);
    if !path.exists() {
        return Err(StudioError::InvalidArgument(format!(
            "artifact {} not found in run {}",
            name, run_id
        )));
    }
    Ok(std::fs::read(&path)?)
}

#[tauri::command]
pub async fn artifacts_delete_run(
    app: tauri::AppHandle,
    run_id: String,
) -> StudioResult<()> {
    let dir = run_dir(&app, &run_id)?;
    if dir.exists() {
        std::fs::remove_dir_all(&dir)?;
    }
    Ok(())
}

fn settings_path(app: &tauri::AppHandle) -> StudioResult<std::path::PathBuf> {
    use tauri::Manager;
    let dir = app
        .path()
        .app_config_dir()
        .map_err(|e| StudioError::Message(e.to_string()))?;
    if !dir.exists() {
        std::fs::create_dir_all(&dir)?;
    }
    Ok(dir.join("config.json"))
}

#[tauri::command]
pub async fn settings_load(
    app: tauri::AppHandle,
) -> StudioResult<Option<Value>> {
    let path = settings_path(&app)?;
    if !path.exists() {
        return Ok(None);
    }
    let raw = std::fs::read_to_string(&path)?;
    let json: Value = serde_json::from_str(&raw)
        .map_err(|e| StudioError::Message(format!("settings parse: {}", e)))?;
    Ok(Some(json))
}

#[tauri::command]
pub async fn settings_save(
    app: tauri::AppHandle,
    settings: Value,
) -> StudioResult<()> {
    let path = settings_path(&app)?;
    let pretty = serde_json::to_string_pretty(&settings)
        .map_err(|e| StudioError::Message(e.to_string()))?;
    std::fs::write(&path, pretty)?;
    Ok(())
}

/// Directory names we never descend into — tree-renderer would
/// drown, watcher would fire constantly, and nobody authors test
/// scripts there. Matches conventions of VS Code / IntelliJ when
/// listing a workspace by default.
const WORKSPACE_SKIP_DIRS: &[&str] = &[
    "node_modules",
    ".git",
    ".hg",
    ".svn",
    "dist",
    "build",
    "target",
    ".next",
    ".turbo",
    ".cache",
    "__pycache__",
    ".venv",
    "venv",
    ".DS_Store",
];

/// Defence against runaway symlinks and misconfigured workspaces
/// (e.g. the home folder). Deeper than a normal project but
/// shallow enough that a bad pick surfaces immediately instead of
/// hanging the UI.
const WORKSPACE_WALK_MAX_DEPTH: usize = 12;

fn workspace_walk(
    dir: &std::path::Path,
    depth: usize,
) -> StudioResult<Vec<Value>> {
    let mut entries: Vec<Value> = Vec::new();
    let iter = match std::fs::read_dir(dir) {
        Ok(it) => it,
        Err(_) => return Ok(entries),
    };
    for entry in iter.flatten() {
        let name = entry.file_name().to_string_lossy().into_owned();
        if WORKSPACE_SKIP_DIRS.contains(&name.as_str()) {
            continue;
        }
        let file_type = match entry.file_type() {
            Ok(t) => t,
            Err(_) => continue,
        };
        let entry_path = entry.path();
        if file_type.is_dir() {
            let children = if depth + 1 < WORKSPACE_WALK_MAX_DEPTH {
                workspace_walk(&entry_path, depth + 1)?
            } else {
                Vec::new()
            };
            entries.push(serde_json::json!({
                "path": entry_path.to_string_lossy(),
                "name": name,
                "type": "directory",
                "children": children,
            }));
        } else if file_type.is_file() {
            entries.push(serde_json::json!({
                "path": entry_path.to_string_lossy(),
                "name": name,
                "type": "file",
            }));
        }
    }
    // Directories first, files second; alphabetical within each
    // group. Mirrors the convention every mainstream IDE uses.
    entries.sort_by(|a, b| {
        let a_dir = a["type"].as_str() == Some("directory");
        let b_dir = b["type"].as_str() == Some("directory");
        let a_name = a["name"].as_str().unwrap_or("").to_lowercase();
        let b_name = b["name"].as_str().unwrap_or("").to_lowercase();
        match (a_dir, b_dir) {
            (true, false) => std::cmp::Ordering::Less,
            (false, true) => std::cmp::Ordering::Greater,
            _ => a_name.cmp(&b_name),
        }
    });
    Ok(entries)
}

#[tauri::command]
pub async fn workspace_open_folder(path: String) -> StudioResult<Value> {
    let root = std::path::PathBuf::from(&path);
    if !root.is_dir() {
        return Err(StudioError::InvalidArgument(format!(
            "{} is not a directory",
            path
        )));
    }
    let entries = workspace_walk(&root, 0)?;
    Ok(serde_json::json!({
        "rootPath": path,
        "entries": entries,
    }))
}

#[tauri::command]
pub async fn workspace_read_script(path: String) -> StudioResult<String> {
    Ok(std::fs::read_to_string(&path)?)
}

#[tauri::command]
pub async fn workspace_write_script(
    path: String,
    content: String,
) -> StudioResult<()> {
    std::fs::write(&path, content)?;
    Ok(())
}

#[tauri::command]
pub async fn workspace_create_script(
    parent_path: String,
    file_name: String,
    content: String,
) -> StudioResult<String> {
    let mut path = std::path::PathBuf::from(&parent_path);
    path.push(&file_name);
    if path.exists() {
        return Err(StudioError::InvalidArgument(format!(
            "file already exists at {}",
            path.to_string_lossy()
        )));
    }
    std::fs::write(&path, content)?;
    Ok(path.to_string_lossy().into_owned())
}

#[tauri::command]
pub async fn workspace_create_directory(
    parent_path: String,
    folder_name: String,
) -> StudioResult<String> {
    let trimmed = folder_name.trim();
    if trimmed.is_empty() {
        return Err(StudioError::InvalidArgument(
            "folder name is empty".into(),
        ));
    }
    // Reject separators so a caller can't bury a folder three
    // levels deep from a single string — matches how VS Code's
    // New Folder action behaves.
    if trimmed.contains('/') || trimmed.contains('\\') {
        return Err(StudioError::InvalidArgument(
            "folder name must not contain path separators".into(),
        ));
    }
    let mut path = std::path::PathBuf::from(&parent_path);
    path.push(trimmed);
    if path.exists() {
        return Err(StudioError::InvalidArgument(format!(
            "a file or folder already exists at {}",
            path.to_string_lossy()
        )));
    }
    std::fs::create_dir(&path)?;
    Ok(path.to_string_lossy().into_owned())
}

#[tauri::command]
pub async fn workspace_delete_script(path: String) -> StudioResult<()> {
    // Accepts both files and directories. Directories are removed
    // recursively — the caller is responsible for confirming
    // destructive intent at the UI layer.
    let p = std::path::Path::new(&path);
    let meta = std::fs::symlink_metadata(p)?;
    if meta.file_type().is_dir() {
        std::fs::remove_dir_all(p)?;
    } else {
        std::fs::remove_file(p)?;
    }
    Ok(())
}

#[tauri::command]
pub async fn workspace_rename_script(
    path: String,
    new_name: String,
) -> StudioResult<String> {
    if new_name.is_empty() || new_name.contains('/') || new_name.contains('\\') {
        return Err(StudioError::InvalidArgument(
            "new name must be a simple file name".into(),
        ));
    }
    let src = std::path::PathBuf::from(&path);
    let parent = src
        .parent()
        .ok_or_else(|| StudioError::InvalidArgument("cannot rename root".into()))?;
    let dst = parent.join(&new_name);
    if dst.exists() {
        return Err(StudioError::InvalidArgument(format!(
            "target already exists: {}",
            dst.to_string_lossy()
        )));
    }
    std::fs::rename(&src, &dst)?;
    Ok(dst.to_string_lossy().into_owned())
}

#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct RecentProject {
    pub id: String,
    pub path: String,
    #[serde(rename = "displayName")]
    pub display_name: String,
    pub pinned: bool,
    #[serde(rename = "lastOpenedAt")]
    pub last_opened_at: i64,
    #[serde(rename = "addedAt")]
    pub added_at: i64,
}

fn projects_path(app: &tauri::AppHandle) -> StudioResult<std::path::PathBuf> {
    use tauri::Manager;
    let dir = app
        .path()
        .app_config_dir()
        .map_err(|e| StudioError::Message(e.to_string()))?;
    if !dir.exists() {
        std::fs::create_dir_all(&dir)?;
    }
    Ok(dir.join("recent-projects.json"))
}

fn read_projects(app: &tauri::AppHandle) -> StudioResult<Vec<RecentProject>> {
    let path = projects_path(app)?;
    if !path.exists() {
        return Ok(vec![]);
    }
    let raw = std::fs::read_to_string(&path)?;
    if raw.trim().is_empty() {
        return Ok(vec![]);
    }
    serde_json::from_str(&raw)
        .map_err(|e| StudioError::Message(format!("recent-projects parse: {}", e)))
}

fn write_projects(
    app: &tauri::AppHandle,
    projects: &[RecentProject],
) -> StudioResult<()> {
    let path = projects_path(app)?;
    let pretty = serde_json::to_string_pretty(projects)
        .map_err(|e| StudioError::Message(e.to_string()))?;
    std::fs::write(&path, pretty)?;
    Ok(())
}

fn djb2(s: &str) -> String {
    let mut h: u32 = 5381;
    for b in s.bytes() {
        h = h.wrapping_mul(33).wrapping_add(u32::from(b));
    }
    format!("{:x}", h)
}

fn basename_of(path: &str) -> String {
    let trimmed = path.trim_end_matches('/');
    match trimmed.rsplit_once('/') {
        Some((_, name)) => name.to_string(),
        None => trimmed.to_string(),
    }
}

fn now_ms() -> i64 {
    use std::time::{SystemTime, UNIX_EPOCH};
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_millis() as i64)
        .unwrap_or(0)
}

#[tauri::command]
pub async fn projects_list(
    app: tauri::AppHandle,
) -> StudioResult<Vec<RecentProject>> {
    read_projects(&app)
}

#[tauri::command]
pub async fn projects_touch(
    app: tauri::AppHandle,
    path: String,
) -> StudioResult<RecentProject> {
    let canonical = std::fs::canonicalize(&path)
        .map(|p| p.to_string_lossy().into_owned())
        .unwrap_or_else(|_| path.clone());
    let id = djb2(&canonical);
    let now = now_ms();

    let mut projects = read_projects(&app)?;
    let existing_index = projects.iter().position(|p| p.id == id);

    let updated = if let Some(i) = existing_index {
        let mut p = projects.remove(i);
        p.last_opened_at = now;
        p
    } else {
        RecentProject {
            id: id.clone(),
            path: canonical.clone(),
            display_name: basename_of(&canonical),
            pinned: false,
            last_opened_at: now,
            added_at: now,
        }
    };
    projects.push(updated.clone());
    write_projects(&app, &projects)?;
    Ok(updated)
}

#[tauri::command]
pub async fn projects_set_pinned(
    app: tauri::AppHandle,
    id: String,
    pinned: bool,
) -> StudioResult<()> {
    let mut projects = read_projects(&app)?;
    for p in &mut projects {
        if p.id == id {
            p.pinned = pinned;
        }
    }
    write_projects(&app, &projects)?;
    Ok(())
}

#[tauri::command]
pub async fn projects_remove(
    app: tauri::AppHandle,
    id: String,
) -> StudioResult<()> {
    let mut projects = read_projects(&app)?;
    projects.retain(|p| p.id != id);
    write_projects(&app, &projects)?;
    Ok(())
}

/// Built-in theme JSONs compiled into the binary. Each entry is
/// the raw JSON string exactly as authored under
/// `apps/studio/src/domain/features/theme/built-ins/*.json`.
const BUILT_IN_THEMES: &[&str] = &[
    include_str!("../../src/domain/features/theme/built-ins/intellij-darcula.json"),
    include_str!("../../src/domain/features/theme/built-ins/intellij-light.json"),
    include_str!("../../src/domain/features/theme/built-ins/atomyx-dark-teal.json"),
    include_str!("../../src/domain/features/theme/built-ins/atomyx-dark-violet.json"),
];

fn user_themes_dir(app: &tauri::AppHandle) -> StudioResult<std::path::PathBuf> {
    use tauri::Manager;
    let dir = app
        .path()
        .app_config_dir()
        .map_err(|e| StudioError::Message(e.to_string()))?;
    let themes = dir.join("themes");
    if !themes.exists() {
        std::fs::create_dir_all(&themes)?;
    }
    Ok(themes)
}

#[tauri::command]
pub async fn themes_list_builtin() -> StudioResult<Vec<Value>> {
    let mut out = Vec::with_capacity(BUILT_IN_THEMES.len());
    for raw in BUILT_IN_THEMES {
        let json: Value = serde_json::from_str(raw)
            .map_err(|e| StudioError::Message(format!("bundled theme parse error: {}", e)))?;
        out.push(json);
    }
    Ok(out)
}

#[tauri::command]
pub async fn themes_list_user(
    app: tauri::AppHandle,
) -> StudioResult<Vec<Value>> {
    let dir = user_themes_dir(&app)?;
    let mut out: Vec<Value> = Vec::new();
    for entry in std::fs::read_dir(&dir)? {
        let entry = entry?;
        if entry.file_type()?.is_file()
            && entry
                .path()
                .extension()
                .and_then(|s| s.to_str())
                == Some("json")
        {
            let raw = std::fs::read_to_string(entry.path())?;
            if let Ok(json) = serde_json::from_str::<Value>(&raw) {
                out.push(json);
            }
        }
    }
    Ok(out)
}

#[tauri::command]
pub async fn themes_read(
    app: tauri::AppHandle,
    id: String,
) -> StudioResult<Option<Value>> {
    let dir = user_themes_dir(&app)?;
    let path = dir.join(format!("{}.json", id));
    if !path.exists() {
        for raw in BUILT_IN_THEMES {
            let json: Value = serde_json::from_str(raw)
                .map_err(|e| StudioError::Message(e.to_string()))?;
            if json.get("id").and_then(|v| v.as_str()) == Some(id.as_str()) {
                return Ok(Some(json));
            }
        }
        return Ok(None);
    }
    let raw = std::fs::read_to_string(&path)?;
    let json: Value =
        serde_json::from_str(&raw).map_err(|e| StudioError::Message(e.to_string()))?;
    Ok(Some(json))
}

#[tauri::command]
pub async fn themes_write(
    app: tauri::AppHandle,
    theme: Value,
) -> StudioResult<()> {
    let id = theme
        .get("id")
        .and_then(|v| v.as_str())
        .ok_or_else(|| StudioError::InvalidArgument("theme.id missing".into()))?;
    let dir = user_themes_dir(&app)?;
    let path = dir.join(format!("{}.json", id));
    let pretty = serde_json::to_string_pretty(&theme)
        .map_err(|e| StudioError::Message(e.to_string()))?;
    std::fs::write(&path, pretty)?;
    Ok(())
}

#[tauri::command]
pub async fn themes_delete(
    app: tauri::AppHandle,
    id: String,
) -> StudioResult<()> {
    let dir = user_themes_dir(&app)?;
    let path = dir.join(format!("{}.json", id));
    if path.exists() {
        std::fs::remove_file(&path)?;
    }
    Ok(())
}

#[tauri::command]
pub async fn themes_open_dir(app: tauri::AppHandle) -> StudioResult<()> {
    let dir = user_themes_dir(&app)?;
    // Best-effort: open in the system file explorer via `open`.
    let status = std::process::Command::new("open")
        .arg(&dir)
        .status()?;
    if !status.success() {
        return Err(StudioError::Message(format!(
            "failed to open {}",
            dir.display()
        )));
    }
    Ok(())
}

use std::sync::Mutex;

static THEME_WATCHERS: Mutex<Vec<notify::RecommendedWatcher>> = Mutex::new(Vec::new());
static WORKSPACE_WATCHER: Mutex<Option<notify::RecommendedWatcher>> = Mutex::new(None);

fn path_should_skip(path: &std::path::Path) -> bool {
    for comp in path.components() {
        if let std::path::Component::Normal(c) = comp {
            let s = c.to_string_lossy();
            if s.starts_with('.') || s == "node_modules" || s == "target" {
                return true;
            }
        }
    }
    false
}

/// Install a filesystem watcher on the user themes dir and emit
/// events to the frontend via the provided Tauri channel. The
/// watcher is kept alive in a static — safe to call multiple
/// times, each call stacks another watcher.
#[tauri::command]
pub async fn themes_watch(
    app: tauri::AppHandle,
    on_event: tauri::ipc::Channel<Value>,
) -> StudioResult<()> {
    use notify::{RecursiveMode, Watcher};

    let dir = user_themes_dir(&app)?;
    let cb = move |res: notify::Result<notify::Event>| {
        if let Ok(event) = res {
            let payload = serde_json::json!({
                "kind": format!("{:?}", event.kind),
                "paths": event
                    .paths
                    .iter()
                    .map(|p| p.to_string_lossy().to_string())
                    .collect::<Vec<_>>(),
            });
            let _ = on_event.send(payload);
        }
    };
    let mut watcher = notify::recommended_watcher(cb)
        .map_err(|e| StudioError::Message(e.to_string()))?;
    watcher
        .watch(&dir, RecursiveMode::NonRecursive)
        .map_err(|e| StudioError::Message(e.to_string()))?;
    THEME_WATCHERS
        .lock()
        .map_err(|e| StudioError::Message(e.to_string()))?
        .push(watcher);
    Ok(())
}

#[tauri::command]
pub async fn workspace_watch(
    workspace_path: String,
    on_event: tauri::ipc::Channel<Value>,
) -> StudioResult<()> {
    use notify::{EventKind, RecursiveMode, Watcher};

    let root = std::path::PathBuf::from(&workspace_path);
    if !root.is_dir() {
        return Err(StudioError::InvalidArgument(format!(
            "not a directory: {}",
            workspace_path
        )));
    }
    let root_for_filter = root.clone();
    let cb = move |res: notify::Result<notify::Event>| {
        let Ok(event) = res else { return };
        let kind_str = match event.kind {
            EventKind::Create(_) => "created",
            EventKind::Modify(_) => "modified",
            EventKind::Remove(_) => "removed",
            _ => return,
        };
        let relevant: Vec<String> = event
            .paths
            .iter()
            .filter(|p| {
                if let Ok(rel) = p.strip_prefix(&root_for_filter) {
                    !path_should_skip(rel)
                } else {
                    !path_should_skip(p)
                }
            })
            .map(|p| p.to_string_lossy().into_owned())
            .collect();
        if relevant.is_empty() {
            return;
        }
        let payload = serde_json::json!({
            "kind": kind_str,
            "paths": relevant,
        });
        let _ = on_event.send(payload);
    };
    let mut watcher = notify::recommended_watcher(cb)
        .map_err(|e| StudioError::Message(e.to_string()))?;
    watcher
        .watch(&root, RecursiveMode::Recursive)
        .map_err(|e| StudioError::Message(e.to_string()))?;
    let mut slot = WORKSPACE_WATCHER
        .lock()
        .map_err(|e| StudioError::Message(e.to_string()))?;
    *slot = Some(watcher);
    Ok(())
}

#[tauri::command]
pub async fn workspace_unwatch() -> StudioResult<()> {
    let mut slot = WORKSPACE_WATCHER
        .lock()
        .map_err(|e| StudioError::Message(e.to_string()))?;
    *slot = None;
    Ok(())
}

struct TerminalHandle {
    writer: Box<dyn std::io::Write + Send>,
    master: Box<dyn portable_pty::MasterPty + Send>,
    child: Box<dyn portable_pty::Child + Send + Sync>,
}

static TERMINALS: Mutex<Option<std::collections::HashMap<String, TerminalHandle>>> =
    Mutex::new(None);

fn with_terminals<T>(
    f: impl FnOnce(&mut std::collections::HashMap<String, TerminalHandle>) -> T,
) -> StudioResult<T> {
    let mut slot = TERMINALS
        .lock()
        .map_err(|e| StudioError::Message(e.to_string()))?;
    if slot.is_none() {
        *slot = Some(std::collections::HashMap::new());
    }
    Ok(f(slot.as_mut().expect("initialized above")))
}

#[tauri::command]
pub async fn terminal_spawn(
    workspace_path: Option<String>,
    cols: u16,
    rows: u16,
    on_data: tauri::ipc::Channel<String>,
) -> StudioResult<String> {
    use portable_pty::{native_pty_system, CommandBuilder, PtySize};

    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(PtySize {
            rows,
            cols,
            pixel_width: 0,
            pixel_height: 0,
        })
        .map_err(|e| StudioError::Message(e.to_string()))?;

    let shell = std::env::var("SHELL").unwrap_or_else(|_| "/bin/bash".to_string());
    let mut cmd = CommandBuilder::new(shell);
    if let Some(cwd) = workspace_path.as_ref() {
        if !cwd.is_empty() {
            cmd.cwd(cwd);
        }
    }
    for (k, v) in std::env::vars() {
        cmd.env(k, v);
    }
    cmd.env("TERM", "xterm-256color");

    let child = pair
        .slave
        .spawn_command(cmd)
        .map_err(|e| StudioError::Message(e.to_string()))?;
    drop(pair.slave);

    let reader = pair
        .master
        .try_clone_reader()
        .map_err(|e| StudioError::Message(e.to_string()))?;
    let writer = pair
        .master
        .take_writer()
        .map_err(|e| StudioError::Message(e.to_string()))?;

    let id = uuid::Uuid::new_v4().to_string();
    let id_for_thread = id.clone();
    let channel = on_data.clone();
    std::thread::spawn(move || {
        let mut reader = reader;
        let mut buf = [0u8; 4096];
        loop {
            match std::io::Read::read(&mut reader, &mut buf) {
                Ok(0) => break,
                Ok(n) => {
                    let s = String::from_utf8_lossy(&buf[..n]).into_owned();
                    if channel.send(s).is_err() {
                        break;
                    }
                }
                Err(_) => break,
            }
        }
        let _ = with_terminals(|m| m.remove(&id_for_thread));
    });

    with_terminals(|m| {
        m.insert(
            id.clone(),
            TerminalHandle {
                writer,
                master: pair.master,
                child,
            },
        );
    })?;

    Ok(id)
}

#[tauri::command]
pub async fn terminal_write(id: String, data: String) -> StudioResult<()> {
    with_terminals(|m| -> StudioResult<()> {
        if let Some(h) = m.get_mut(&id) {
            h.writer
                .write_all(data.as_bytes())
                .map_err(StudioError::Io)?;
            h.writer.flush().map_err(StudioError::Io)?;
        }
        Ok(())
    })??;
    Ok(())
}

#[tauri::command]
pub async fn terminal_resize(id: String, cols: u16, rows: u16) -> StudioResult<()> {
    use portable_pty::PtySize;
    with_terminals(|m| -> StudioResult<()> {
        if let Some(h) = m.get_mut(&id) {
            h.master
                .resize(PtySize {
                    rows,
                    cols,
                    pixel_width: 0,
                    pixel_height: 0,
                })
                .map_err(|e| StudioError::Message(e.to_string()))?;
        }
        Ok(())
    })??;
    Ok(())
}

#[tauri::command]
pub async fn terminal_kill(id: String) -> StudioResult<()> {
    with_terminals(|m| {
        if let Some(mut h) = m.remove(&id) {
            let _ = h.child.kill();
        }
    })?;
    Ok(())
}

/// Workspace-scoped Studio config directory: `<workspace>/.atomyx`.
///
/// Creates the directory on first call. The companion `.gitignore`
/// is seeded exactly once — the helper never overwrites an
/// existing file, so a team can commit their own tweaks without
/// the next Studio launch clobbering them.
///
/// Files inside `.atomyx/` fall into two categories:
///
///   - Team-shareable (committed): `run-configs.json`,
///     `themes/`, and future `settings.json` / `environment.yml`.
///   - Local-only (gitignored by default): `workspace.json`
///     (open tabs / layout / bookmarks — user-specific state),
///     `cache/`, `artifacts/` (run outputs).
///
/// The seeded `.gitignore` lists the second set so the first set
/// stays committable by default without teaching contributors the
/// convention by hand.
fn atomyx_dir(workspace: &str) -> StudioResult<std::path::PathBuf> {
    let dir = std::path::PathBuf::from(workspace).join(".atomyx");
    if !dir.exists() {
        std::fs::create_dir_all(&dir)?;
    }
    let gitignore = dir.join(".gitignore");
    if !gitignore.exists() {
        let _ = std::fs::write(
            &gitignore,
            "# Studio — user-specific state, not shared with the team\n\
             workspace.json\n\
             \n\
             # Studio — run artifacts & local caches (regenerable)\n\
             cache/\n\
             artifacts/\n",
        );
    }
    Ok(dir)
}


fn window_label_for(workspace_path: &str) -> String {
    format!("ws-{}", djb2(workspace_path))
}

#[tauri::command]
pub async fn window_open_workspace(
    app: tauri::AppHandle,
    workspace_path: String,
) -> StudioResult<()> {
    use tauri::{Manager, WebviewUrl, WebviewWindowBuilder};

    let label = window_label_for(&workspace_path);
    // Focus an existing window for the same workspace instead of
    // spawning a duplicate — IntelliJ-style single-window-per-project.
    if let Some(existing) = app.get_webview_window(&label) {
        let _ = existing.set_focus();
        return Ok(());
    }

    let url = format!(
        "index.html?workspace={}",
        urlencoding_encode(&workspace_path)
    );
    let title = format!(
        "Atomyx Studio — {}",
        basename_of(&workspace_path)
    );
    WebviewWindowBuilder::new(&app, label, WebviewUrl::App(url.into()))
        .title(title)
        .inner_size(1280.0, 800.0)
        .min_inner_size(900.0, 600.0)
        .build()
        .map_err(|e| StudioError::Message(e.to_string()))?;
    Ok(())
}

fn urlencoding_encode(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    for b in s.bytes() {
        match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(b as char);
            }
            _ => out.push_str(&format!("%{:02X}", b)),
        }
    }
    out
}

/// Resolve a caller-supplied relative path to an absolute file
/// location under `<workspace>/.atomyx/`. Absolute paths, `..`
/// segments, and `RootDir` / `Prefix` components are rejected so
/// the renderer can't escape the config folder.
fn project_config_resolve(
    workspace: &str,
    rel: &str,
) -> StudioResult<std::path::PathBuf> {
    use std::path::Component;
    let trimmed = rel.trim();
    if trimmed.is_empty() {
        return Err(StudioError::Message(
            "project config: relative path is empty".into(),
        ));
    }
    let rel_path = std::path::Path::new(trimmed);
    if rel_path.is_absolute() {
        return Err(StudioError::Message(
            "project config: absolute path is not allowed".into(),
        ));
    }
    for component in rel_path.components() {
        match component {
            Component::Normal(_) | Component::CurDir => {}
            _ => {
                return Err(StudioError::Message(format!(
                    "project config: rejected path segment in {:?}",
                    rel_path
                )));
            }
        }
    }
    Ok(atomyx_dir(workspace)?.join(rel_path))
}

#[tauri::command]
pub async fn project_config_read_json(
    workspace_path: String,
    rel_path: String,
) -> StudioResult<Option<Value>> {
    let path = project_config_resolve(&workspace_path, &rel_path)?;
    if !path.exists() {
        return Ok(None);
    }
    let raw = std::fs::read_to_string(&path)?;
    if raw.trim().is_empty() {
        return Ok(None);
    }
    let json: Value = serde_json::from_str(&raw).map_err(|e| {
        StudioError::Message(format!("project config parse {}: {}", rel_path, e))
    })?;
    Ok(Some(json))
}

#[tauri::command]
pub async fn project_config_write_json(
    workspace_path: String,
    rel_path: String,
    value: Value,
) -> StudioResult<()> {
    let path = project_config_resolve(&workspace_path, &rel_path)?;
    if let Some(parent) = path.parent() {
        if !parent.exists() {
            std::fs::create_dir_all(parent)?;
        }
    }
    let pretty = serde_json::to_string_pretty(&value)
        .map_err(|e| StudioError::Message(e.to_string()))?;
    std::fs::write(&path, pretty)?;
    Ok(())
}

#[tauri::command]
pub async fn project_config_read_text(
    workspace_path: String,
    rel_path: String,
) -> StudioResult<Option<String>> {
    let path = project_config_resolve(&workspace_path, &rel_path)?;
    if !path.exists() {
        return Ok(None);
    }
    Ok(Some(std::fs::read_to_string(&path)?))
}

#[tauri::command]
pub async fn project_config_write_text(
    workspace_path: String,
    rel_path: String,
    content: String,
) -> StudioResult<()> {
    let path = project_config_resolve(&workspace_path, &rel_path)?;
    if let Some(parent) = path.parent() {
        if !parent.exists() {
            std::fs::create_dir_all(parent)?;
        }
    }
    std::fs::write(&path, content)?;
    Ok(())
}

#[tauri::command]
pub async fn project_config_list_json_directory(
    workspace_path: String,
    rel_path: String,
) -> StudioResult<Vec<Value>> {
    // Read-only: resolve the directory without materialising
    // `.atomyx/` or seeding the gitignore. A workspace that has
    // never written config shouldn't acquire the folder just from
    // a directory scan.
    use std::path::Component;
    let trimmed = rel_path.trim();
    if trimmed.is_empty() {
        return Err(StudioError::Message(
            "project config: relative path is empty".into(),
        ));
    }
    let rel_path_p = std::path::Path::new(trimmed);
    if rel_path_p.is_absolute() {
        return Err(StudioError::Message(
            "project config: absolute path is not allowed".into(),
        ));
    }
    for component in rel_path_p.components() {
        match component {
            Component::Normal(_) | Component::CurDir => {}
            _ => {
                return Err(StudioError::Message(format!(
                    "project config: rejected path segment in {:?}",
                    rel_path_p
                )));
            }
        }
    }
    let dir = std::path::PathBuf::from(&workspace_path)
        .join(".atomyx")
        .join(rel_path_p);
    if !dir.exists() || !dir.is_dir() {
        return Ok(vec![]);
    }
    let mut out: Vec<Value> = Vec::new();
    for entry in std::fs::read_dir(&dir)? {
        let entry = entry?;
        if !entry.file_type()?.is_file() {
            continue;
        }
        if entry.path().extension().and_then(|s| s.to_str()) != Some("json") {
            continue;
        }
        let Ok(raw) = std::fs::read_to_string(entry.path()) else {
            continue;
        };
        if let Ok(json) = serde_json::from_str::<Value>(&raw) {
            out.push(json);
        }
    }
    Ok(out)
}

#[derive(Debug, Clone, serde::Serialize)]
pub struct SearchHit {
    pub path: String,
    pub line: u32,
    pub snippet: String,
}

#[derive(Debug, Clone, serde::Serialize)]
pub struct TodoHit {
    pub path: String,
    pub line: u32,
    pub kind: String,
    pub snippet: String,
}

const TODO_KINDS: &[&str] = &["TODO", "FIXME", "HACK", "XXX", "NOTE"];
const TODO_HIT_CAP: usize = 500;

#[tauri::command]
pub async fn workspace_scan_todos(
    workspace_path: String,
) -> StudioResult<Vec<TodoHit>> {
    let root = std::path::PathBuf::from(&workspace_path);
    if !root.is_dir() {
        return Ok(vec![]);
    }
    let mut hits: Vec<TodoHit> = Vec::new();
    let mut stack: Vec<std::path::PathBuf> = vec![root];
    while let Some(dir) = stack.pop() {
        if hits.len() >= TODO_HIT_CAP {
            break;
        }
        let iter = match std::fs::read_dir(&dir) {
            Ok(it) => it,
            Err(_) => continue,
        };
        for entry in iter.flatten() {
            if hits.len() >= TODO_HIT_CAP {
                break;
            }
            let path = entry.path();
            let name = entry.file_name();
            let name_str = name.to_string_lossy();
            if name_str.starts_with('.') || name_str == "node_modules" {
                continue;
            }
            let file_type = match entry.file_type() {
                Ok(t) => t,
                Err(_) => continue,
            };
            if file_type.is_dir() {
                stack.push(path);
                continue;
            }
            if !file_type.is_file() {
                continue;
            }
            let ext = path
                .extension()
                .and_then(|e| e.to_str())
                .map(|e| e.to_lowercase())
                .unwrap_or_default();
            if !WORKSPACE_SEARCH_EXTS.iter().any(|e| *e == ext) {
                continue;
            }
            let contents = match std::fs::read_to_string(&path) {
                Ok(s) => s,
                Err(_) => continue,
            };
            for (idx, raw_line) in contents.lines().enumerate() {
                if hits.len() >= TODO_HIT_CAP {
                    break;
                }
                let upper = raw_line.to_uppercase();
                for kind in TODO_KINDS {
                    if let Some(pos) = upper.find(kind) {
                        let after = pos + kind.len();
                        let next = upper.as_bytes().get(after).copied();
                        let before = if pos == 0 {
                            None
                        } else {
                            upper.as_bytes().get(pos - 1).copied()
                        };
                        let word_boundary_before =
                            before.map_or(true, |b| !(b as char).is_alphanumeric());
                        let word_boundary_after =
                            next.map_or(true, |b| !(b as char).is_alphanumeric());
                        if !word_boundary_before || !word_boundary_after {
                            continue;
                        }
                        hits.push(TodoHit {
                            path: path.to_string_lossy().into_owned(),
                            line: (idx as u32) + 1,
                            kind: (*kind).to_string(),
                            snippet: raw_line
                                .trim()
                                .chars()
                                .take(200)
                                .collect(),
                        });
                        break;
                    }
                }
            }
        }
    }
    Ok(hits)
}

const WORKSPACE_SEARCH_HIT_CAP: usize = 200;
const WORKSPACE_SEARCH_EXTS: &[&str] = &[
    "yml", "yaml", "md", "txt", "json", "env",
];

#[tauri::command]
pub async fn workspace_search(
    workspace_path: String,
    query: String,
) -> StudioResult<Vec<SearchHit>> {
    let q = query.trim();
    if q.len() < 2 {
        return Ok(vec![]);
    }
    let needle = q.to_lowercase();
    let root = std::path::PathBuf::from(&workspace_path);
    if !root.is_dir() {
        return Ok(vec![]);
    }
    let mut hits: Vec<SearchHit> = Vec::new();
    let mut stack: Vec<std::path::PathBuf> = vec![root];
    while let Some(dir) = stack.pop() {
        if hits.len() >= WORKSPACE_SEARCH_HIT_CAP {
            break;
        }
        let iter = match std::fs::read_dir(&dir) {
            Ok(it) => it,
            Err(_) => continue,
        };
        for entry in iter.flatten() {
            if hits.len() >= WORKSPACE_SEARCH_HIT_CAP {
                break;
            }
            let path = entry.path();
            let name = entry.file_name();
            let name_str = name.to_string_lossy();
            if name_str.starts_with('.') || name_str == "node_modules" {
                continue;
            }
            let file_type = match entry.file_type() {
                Ok(t) => t,
                Err(_) => continue,
            };
            if file_type.is_dir() {
                stack.push(path);
                continue;
            }
            if !file_type.is_file() {
                continue;
            }
            let ext = path
                .extension()
                .and_then(|e| e.to_str())
                .map(|e| e.to_lowercase())
                .unwrap_or_default();
            if !WORKSPACE_SEARCH_EXTS.iter().any(|e| *e == ext) {
                continue;
            }
            let contents = match std::fs::read_to_string(&path) {
                Ok(s) => s,
                Err(_) => continue,
            };
            for (idx, raw_line) in contents.lines().enumerate() {
                if hits.len() >= WORKSPACE_SEARCH_HIT_CAP {
                    break;
                }
                if raw_line.to_lowercase().contains(&needle) {
                    hits.push(SearchHit {
                        path: path.to_string_lossy().into_owned(),
                        line: (idx as u32) + 1,
                        snippet: raw_line.trim().chars().take(200).collect(),
                    });
                }
            }
        }
    }
    Ok(hits)
}
