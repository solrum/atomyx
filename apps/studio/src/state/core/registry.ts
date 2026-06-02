/**
 * Feature instance registry. One singleton per app process.
 *
 * The composition root (`main.tsx`) constructs each feature's
 * concrete instance via its factory and calls `registerFeature`.
 * Every other consumer reads instances via `getFeature` (imperative
 * callers) or the feature's own `useXxx()` React hook (which wraps
 * getFeature + useSyncExternalStore).
 *
 * No code outside the composition root or tests is allowed to
 * register. If a test needs to swap an instance it calls
 * `registerFeature` inside its setup block; this is by design.
 */

const instances = new Map<string, unknown>();

export function registerFeature<T>(key: string, instance: T): void {
  instances.set(key, instance);
}

export function getFeature<T>(key: string): T {
  const value = instances.get(key);
  if (value === undefined) {
    throw new Error(
      `Feature "${key}" not registered. The composition root must call registerFeature() before any consumer calls getFeature().`,
    );
  }
  return value as T;
}

export function hasFeature(key: string): boolean {
  return instances.has(key);
}

/**
 * Test-only helper — clears the registry between cases so that
 * setups remain isolated. Never call from production code.
 */
export function resetFeaturesForTest(): void {
  instances.clear();
}
