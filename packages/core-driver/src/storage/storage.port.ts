/**
 * Storage port — persistent key/value-ish storage for framework
 * data that needs to outlive the process (bug reports, case
 * studies, recorded test runs). Each record is a JSON-
 * serializable object identified by a string key.
 *
 * Separate from the `Driver` port because storage is a
 * cross-cutting concern — multiple drivers, test-mgmt module,
 * and cloud module all need the same abstraction. Feature
 * consumers inject the implementation they want:
 *
 *   - `FileStorage` — default, writes to `~/.atomyx/<namespace>/`
 *   - `SynapseStorage` — future, POST to Synapse API
 *   - `InMemoryStorage` — tests
 *
 * Namespacing is by string prefix, not directories. Consumers
 * append their own prefix to avoid collision ("bugs/123",
 * "case-studies/2026-04", "runs/abc").
 */
export interface Storage {
  save(key: string, data: unknown): Promise<void>;
  load<T = unknown>(key: string): Promise<T | null>;
  list(prefix?: string): Promise<string[]>;
  delete(key: string): Promise<void>;
}
