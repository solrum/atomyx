const DOUBLE_SHIFT_WINDOW_MS = 350;

let lastShiftAt = 0;
let installed = false;

/**
 * Detect IntelliJ-style "double Shift" keypress and fire the
 * supplied callback. The watcher listens on `window` capture; two
 * bare Shift presses within `DOUBLE_SHIFT_WINDOW_MS` of each
 * other trigger. Any other key in between resets the counter.
 */
export function installDoubleShift(callback: () => void): void {
  if (installed) return;
  installed = true;

  window.addEventListener(
    "keydown",
    (event) => {
      if (event.key !== "Shift") {
        lastShiftAt = 0;
        return;
      }
      if (event.ctrlKey || event.metaKey || event.altKey) {
        lastShiftAt = 0;
        return;
      }
      const now = performance.now();
      if (now - lastShiftAt <= DOUBLE_SHIFT_WINDOW_MS) {
        lastShiftAt = 0;
        event.preventDefault();
        callback();
      } else {
        lastShiftAt = now;
      }
    },
    { capture: true },
  );
}
