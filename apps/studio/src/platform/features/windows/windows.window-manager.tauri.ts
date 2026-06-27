/**
 * Read the `?workspace=` query parameter the Rust backend injects
 * when spawning a workspace window. Returns `null` for the primary
 * / Welcome window.
 */
export function getLaunchWorkspacePath(): string | null {
  try {
    const params = new URLSearchParams(window.location.search);
    const raw = params.get("workspace");
    return raw ? decodeURIComponent(raw) : null;
  } catch {
    return null;
  }
}
