use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::Arc;

use tokio::net::TcpStream;
use tokio::process::Child;
use tokio::sync::Mutex;
use tokio::task::JoinHandle;

/// Handle for a live mirror session. Dropping the struct triggers
/// `stop` — both child processes are signalled, the parser task is
/// aborted, and the fifo is unlinked.
pub struct Session {
    pub id: String,
    pub started_at: i64,
    pub backend: String,
    pub fifo_path: Option<PathBuf>,
    pub scrcpy: Option<Child>,
    pub ffmpeg: Option<Child>,
    pub parser_task: Option<JoinHandle<()>>,
    /// When the scrcpy adapter sets up an adb-reverse tunnel, we
    /// stash the triple `(adb_path, device_serial, socket_name)`
    /// here so shutdown can remove the tunnel without the caller
    /// re-deriving it.
    pub adb_reverse_cleanup: Option<(PathBuf, String, String)>,
    /// Writable half of the scrcpy control socket, shared between
    /// the session handle and the commands that inject input.
    pub control_stream: Option<Arc<Mutex<TcpStream>>>,
    /// Device screen size as reported in the codec meta header.
    /// Touch events must carry these dimensions so scrcpy can
    /// scale back to the physical display.
    pub video_size: Option<(u16, u16)>,
    /// WebSocket port assigned by the iOS Simulator capture helper.
    /// Present only for sessions using the VT+WS transport; None
    /// for the fMP4 fallback path and for Android (scrcpy) sessions.
    pub ws_port: Option<u16>,
}

impl Session {
    pub fn new(backend: &str) -> Self {
        Self {
            id: uuid::Uuid::new_v4().to_string(),
            started_at: chrono_now_ms(),
            backend: backend.to_string(),
            fifo_path: None,
            scrcpy: None,
            ffmpeg: None,
            parser_task: None,
            adb_reverse_cleanup: None,
            control_stream: None,
            video_size: None,
            ws_port: None,
        }
    }

    pub async fn shutdown(&mut self) {
        // Abort the parser first so it stops pulling from the
        // socket while we signal the subprocess tree.
        if let Some(task) = self.parser_task.take() {
            task.abort();
            let _ = task.await;
        }
        if let Some(mut child) = self.ffmpeg.take() {
            let _ = child.kill().await;
        }
        if let Some(mut child) = self.scrcpy.take() {
            if let Some(pid) = child.id() {
                let _ = nix_signal(pid as i32, nix_sigint());
            }
            let _ = child.kill().await;
        }
        if let Some(stream) = self.control_stream.take() {
            // Drop the Arc; the underlying socket closes when the
            // last reference goes away. A fallback shutdown below
            // guarantees the FD is released even if another Arc
            // clone is still held by an in-flight command.
            use tokio::io::AsyncWriteExt;
            let mut guard = stream.lock().await;
            let _ = guard.shutdown().await;
        }
        if let Some((adb, serial, socket_name)) = self.adb_reverse_cleanup.take() {
            let _ = super::scrcpy::adb_reverse_remove(&adb, &serial, &socket_name).await;
        }
        if let Some(path) = self.fifo_path.take() {
            let _ = tokio::fs::remove_file(path).await;
        }
    }
}

/// Thread-safe registry of live sessions keyed by session id.
pub struct SessionRegistry {
    inner: Mutex<HashMap<String, Arc<Mutex<Session>>>>,
}

impl SessionRegistry {
    pub fn new() -> Self {
        Self {
            inner: Mutex::new(HashMap::new()),
        }
    }

    pub async fn insert(&self, session: Session) -> Arc<Mutex<Session>> {
        let id = session.id.clone();
        let handle = Arc::new(Mutex::new(session));
        self.inner.lock().await.insert(id, handle.clone());
        handle
    }

    pub async fn get(&self, id: &str) -> Option<Arc<Mutex<Session>>> {
        self.inner.lock().await.get(id).cloned()
    }

    pub async fn remove(&self, id: &str) -> Option<Arc<Mutex<Session>>> {
        self.inner.lock().await.remove(id)
    }
}

impl Default for SessionRegistry {
    fn default() -> Self {
        Self::new()
    }
}

fn chrono_now_ms() -> i64 {
    use std::time::{SystemTime, UNIX_EPOCH};
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_millis() as i64)
        .unwrap_or(0)
}

// Local SIGINT sender. Pulling in the `nix` crate for one signal
// call is heavy; shell out to libc directly.
fn nix_sigint() -> i32 {
    2 // SIGINT
}

fn nix_signal(pid: i32, sig: i32) -> Result<(), std::io::Error> {
    // Safety: libc::kill is an FFI call with no Rust invariants.
    let res = unsafe { libc::kill(pid, sig) };
    if res == 0 {
        Ok(())
    } else {
        Err(std::io::Error::last_os_error())
    }
}
