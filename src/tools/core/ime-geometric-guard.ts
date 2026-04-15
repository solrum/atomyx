import type { CompactElement, DeviceController } from "../../adapters/device-controller.port.js";

/**
 * Structural guard that refuses coordinate-based gestures (tap, long-press,
 * swipe endpoints) that fall inside the soft-keyboard region. Universal
 * signal — any element with `isInIme: true` whose bounds contain the point
 * means the agent is trying to dispatch touches at a keyboard key directly.
 *
 * The right tool for that is `input_text`, not coordinate tapping. This
 * guard enforces the contract at the tool layer so agents can't brute-force
 * typing by tapping individual keys — even via `swipe(x,y → x,y)` dummy
 * tricks, because the geometry doesn't lie.
 *
 * The check is O(n) over the compact tree, ~30 elements typical — cost is
 * dominated by the `getUiSummary()` HTTP roundtrip, not the filter.
 */
export class ImeGeometricGuard {
  async blocks(
    x: number,
    y: number,
    controller: Pick<DeviceController, "getUiSummary">,
  ): Promise<boolean> {
    try {
      const summary = await controller.getUiSummary();
      return summary.some((e) => this.isInIme(e) && this.contains(e, x, y));
    } catch {
      return false;
    }
  }

  /**
   * Bulk variant — check the (x, y) once against a pre-fetched summary.
   * Used inside polling loops where we already have a recent summary and
   * want to avoid the extra roundtrip.
   */
  blocksInSummary(x: number, y: number, summary: CompactElement[]): boolean {
    return summary.some((e) => this.isInIme(e) && this.contains(e, x, y));
  }

  private isInIme(e: CompactElement): boolean {
    return e.isInIme === true;
  }

  private contains(e: CompactElement, x: number, y: number): boolean {
    return (
      x >= e.bounds.left &&
      x <= e.bounds.right &&
      y >= e.bounds.top &&
      y <= e.bounds.bottom
    );
  }
}
