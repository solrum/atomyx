import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ChevronRight,
  ChevronDown,
  ChevronsDownUp,
  ChevronsUpDown,
  RotateCcw,
  Copy,
  Filter,
  Code,
  Play,
  Pause,
} from "lucide-react";
import {
  SCRIPT_ACTIONS,
  selectorsFromNode,
  bestSelector,
  type ScriptAction,
  type SelectorCandidate,
} from "../../../domain/features/script-actions/index.js";
import { useDevices } from "../../../state/features/devices/index.js";
import { useIosAgentStatus } from "../../../state/features/ios-agent/index.js";
import { useMirror } from "../../../state/features/mirror/index.js";
import { getFeature } from "../../../state/core/registry.js";
import type { NotificationsApi } from "../../../state/features/notifications/index.js";
import { NOTIFICATIONS_KEY } from "../../../state/features/notifications/index.js";
import {
  attributeRows,
  collectBranchPaths,
  collectInterestingPaths,
  isInformative,
  resolveUiNode,
  summarize,
  truncate,
  useUiInspector,
  type UiNodePath,
  type UiTreeNode,
} from "../../../state/features/ui-inspector/index.js";
import { insertAtActiveCursor } from "../editor/index.js";
import {
  Button,
  ContextMenu,
  type ContextMenuEntry,
} from "../../primitives/index.js";

/**
 * UI inspection panel. Renders the device's current UI hierarchy as
 * a collapsible tree plus an attribute detail pane for the selected
 * node. Snapshot-based — hit Refresh to re-capture; the tree does
 * not auto-update when the device screen changes.
 */
export interface InspectorPaneProps {
  /**
   * When mounted inside another floating chrome (e.g. the
   * DeviceMirrorWindow), the standalone header is suppressed and
   * its three buttons (expand/collapse/refresh) move into a thin
   * inline toolbar above the tree so the host avoids stacking two
   * sets of chrome and the body fills the available height.
   */
  readonly embedded?: boolean;
}

export function InspectorPane({ embedded = false }: InspectorPaneProps = {}) {
  const { devices, selectedId } = useDevices();
  const mirrorSnap = useMirror();
  const {
    tree,
    selectedPath,
    loading,
    error,
    capturedForDeviceId,
    showRaw,
    autoRefreshEnabled,
    autoRefreshIntervalMs,
    autoRefreshPaused,
    refresh,
    select,
    setShowRaw,
    setAutoRefreshEnabled,
  } = useUiInspector();

  const activeSession = Object.values(mirrorSnap.sessions)[0] ?? null;
  // Prefer the device the live mirror session is bound to so the
  // inspector dumps from the same source the user sees on screen.
  const activeDevice = activeSession
    ? (devices.find((d) => d.id === activeSession.target.id) ?? null)
    : (devices.find((d) => d.id === selectedId) ?? devices[0] ?? null);

  // For iOS targets the XCUITest agent must be in `ready` state
  // before dump calls will succeed; firing earlier hangs at
  // "Capturing UI tree…" until the JSON-RPC request times out.
  const iosStatus = useIosAgentStatus(
    activeDevice?.platform === "ios" ? activeDevice.id : null,
  );
  const agentReady =
    activeDevice?.platform === "ios" ? iosStatus?.state === "ready" : true;

  // Per-device retry budget for the auto-capture effect. Failures
  // accumulate per device; once exhausted the effect stops firing
  // until the user picks a different device or hits Refresh
  // manually. Without this cap a crashing runner would loop the
  // dump call forever and leak memory until the app is force-
  // quit.
  const MAX_AUTO_RETRIES = 2;
  const retryBudgetRef = useRef<{ deviceId: string | null; attempts: number }>({
    deviceId: null,
    attempts: 0,
  });
  const [budgetExhausted, setBudgetExhausted] = useState(false);

  const onRefresh = () => {
    if (!activeDevice) return;
    // Manual refresh resets the auto-capture budget for this device.
    retryBudgetRef.current = { deviceId: activeDevice.id, attempts: 0 };
    setBudgetExhausted(false);
    void refresh(activeDevice.id);
  };

  // Auto-capture, gated on agent readiness for iOS and bounded by
  // the retry budget. After a fresh mirror session the agent may
  // still be building; we wait until it flips to `ready`, then
  // trigger one capture. Errors retry with a longer backoff up to
  // MAX_AUTO_RETRIES; beyond that we stop and rely on the user.
  useEffect(() => {
    if (!activeDevice) return;
    if (!agentReady) return;
    if (capturedForDeviceId === activeDevice.id) return;
    if (loading) return;

    // Reset the budget when the active device changes.
    if (retryBudgetRef.current.deviceId !== activeDevice.id) {
      retryBudgetRef.current = { deviceId: activeDevice.id, attempts: 0 };
      setBudgetExhausted(false);
    }
    if (retryBudgetRef.current.attempts >= MAX_AUTO_RETRIES) {
      if (!budgetExhausted) setBudgetExhausted(true);
      return;
    }

    let cancelled = false;
    const t = setTimeout(() => {
      if (cancelled) return;
      retryBudgetRef.current = {
        deviceId: activeDevice.id,
        attempts: retryBudgetRef.current.attempts + 1,
      };
      void refresh(activeDevice.id);
    }, error ? 1500 : 400);
    return () => {
      cancelled = true;
      clearTimeout(t);
    };
  }, [
    activeDevice,
    agentReady,
    capturedForDeviceId,
    loading,
    error,
    refresh,
    budgetExhausted,
  ]);

  // Membership = "this path is collapsed". Empty set ⇒ every node
  // expanded, which is the default the user asked for. Toggling a
  // chevron flips membership; the two toolbar buttons clear or
  // populate it in bulk.
  const [collapsedPaths, setCollapsedPaths] = useState<ReadonlySet<UiNodePath>>(
    () => new Set<UiNodePath>(),
  );

  // XCUITest dumps wrap every SwiftUI / UIKit container in `other`
  // nodes that carry no identifier. The inspector tree drowns in
  // them — e.g. ten levels of `other` between `window` and a
  // `button`. Hide-noise mode skips those wrapper rows during
  // render and promotes their children up to the nearest
  // informative ancestor, so the visible tree only has nodes the
  // user can actually target. Default ON; toggle exposed in the
  // toolbar so power users can reveal the raw structure.
  const [hideNoise, setHideNoise] = useState(true);

  const togglePath = useCallback((path: UiNodePath) => {
    setCollapsedPaths((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  }, []);

  const expandAll = useCallback(() => {
    setCollapsedPaths(new Set());
  }, []);

  const collapseAll = useCallback(() => {
    if (!tree) return;
    setCollapsedPaths(new Set(collectBranchPaths(tree)));
  }, [tree]);

  const inlineToolbar = (
    <div
      className="flex items-center justify-end flex-none"
      style={{
        height: "24px",
        padding: "0 var(--gap-3)",
        gap: "var(--gap-2)",
        background: "var(--bg-1)",
        borderBottom: "1px solid var(--line-soft)",
      }}
    >
      <span
        style={{ fontSize: "var(--fs-11)", color: "var(--fg-2)", marginRight: "auto" }}
      >
        {activeDevice ? activeDevice.name : "no device"}
      </span>
      <Button
        variant="ghost"
        size="sm"
        onClick={() => setHideNoise((v) => !v)}
        disabled={!tree}
        title={hideNoise ? "Show noise (raw structure)" : "Hide noise (skip wrapper rows)"}
        style={hideNoise ? { color: "var(--accent)" } : undefined}
      >
        <Filter className="h-3 w-3" />
      </Button>
      <Button
        variant="ghost"
        size="sm"
        onClick={() => setShowRaw(!showRaw)}
        disabled={!tree}
        title={showRaw ? "Hide raw class" : "Show raw class next to role"}
        style={showRaw ? { color: "var(--accent)" } : undefined}
      >
        <Code className="h-3 w-3" />
      </Button>
      <Button
        variant="ghost"
        size="sm"
        onClick={expandAll}
        disabled={!tree || collapsedPaths.size === 0}
        title="Expand all"
      >
        <ChevronsUpDown className="h-3 w-3" />
      </Button>
      <Button
        variant="ghost"
        size="sm"
        onClick={collapseAll}
        disabled={!tree}
        title="Collapse all"
      >
        <ChevronsDownUp className="h-3 w-3" />
      </Button>
      <Button
        variant="ghost"
        size="sm"
        onClick={onRefresh}
        disabled={!activeDevice || loading}
        title="Refresh UI tree"
      >
        <RotateCcw className="h-3 w-3" />
      </Button>
      <AutoRefreshToggle
        enabled={autoRefreshEnabled}
        paused={autoRefreshPaused}
        intervalMs={autoRefreshIntervalMs}
        disabled={!activeDevice}
        onToggle={() => setAutoRefreshEnabled(!autoRefreshEnabled)}
      />
    </div>
  );

  return (
    <section
      className="h-full flex flex-col min-h-0"
      style={{
        background: "var(--bg-1)",
        color: "var(--fg-0)",
        borderLeft: embedded ? undefined : "1px solid var(--line)",
      }}
    >
      {embedded ? (
        inlineToolbar
      ) : (
        <header
          className="flex items-center justify-between px-3 py-1.5 text-xs"
          style={{ borderBottom: "1px solid var(--line)" }}
        >
          <div
            className="uppercase tracking-wider"
            style={{ color: "var(--fg-2)" }}
          >
            Inspector
          </div>
          <div className="flex items-center gap-2">
            <span
              className="text-[11px]"
              style={{ color: "var(--fg-2)" }}
            >
              {activeDevice ? activeDevice.name : "no device"}
            </span>
            <Button
              variant="ghost"
              size="sm"
              onClick={expandAll}
              disabled={!tree || collapsedPaths.size === 0}
              title="Expand all"
            >
              <ChevronsUpDown className="h-3 w-3" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={collapseAll}
              disabled={!tree}
              title="Collapse all"
            >
              <ChevronsDownUp className="h-3 w-3" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={onRefresh}
              disabled={!activeDevice || loading}
              title="Refresh UI tree"
            >
              <RotateCcw className="h-3 w-3" />
            </Button>
            <AutoRefreshToggle
              enabled={autoRefreshEnabled}
              paused={autoRefreshPaused}
              intervalMs={autoRefreshIntervalMs}
              disabled={!activeDevice}
              onToggle={() => setAutoRefreshEnabled(!autoRefreshEnabled)}
            />
          </div>
        </header>
      )}
      <div className="flex-1 min-h-0 flex flex-col">
        <TreeBody
          activeDeviceId={activeDevice?.id ?? null}
          tree={tree}
          loading={loading}
          error={error}
          capturedForDeviceId={capturedForDeviceId}
          selectedPath={selectedPath}
          onSelect={select}
          collapsedPaths={collapsedPaths}
          onToggleExpanded={togglePath}
          hideNoise={hideNoise}
          showRaw={showRaw}
        />
        <AttributesPanel node={resolveUiNode(tree, selectedPath)} />
      </div>
    </section>
  );
}

interface TreeBodyProps {
  readonly activeDeviceId: string | null;
  readonly tree: UiTreeNode | null;
  readonly loading: boolean;
  readonly error: string | null;
  readonly capturedForDeviceId: string | null;
  readonly selectedPath: UiNodePath | null;
  readonly onSelect: (path: UiNodePath) => void;
  readonly collapsedPaths: ReadonlySet<UiNodePath>;
  readonly onToggleExpanded: (path: UiNodePath) => void;
  readonly hideNoise: boolean;
  readonly showRaw: boolean;
}

function TreeBody({
  activeDeviceId,
  tree,
  loading,
  error,
  capturedForDeviceId,
  selectedPath,
  onSelect,
  collapsedPaths,
  onToggleExpanded,
  hideNoise,
  showRaw,
}: TreeBodyProps) {
  const interestingPaths = useMemo(
    () => collectInterestingPaths(tree),
    [tree],
  );
  if (!activeDeviceId) {
    return (
      <EmptyState message="Select a device to inspect its UI tree." />
    );
  }
  if (loading && !tree) {
    return <EmptyState message="Capturing UI tree…" />;
  }
  if (error) {
    return (
      <EmptyState
        message={`Capture failed: ${error}`}
        tone="danger"
      />
    );
  }
  if (!tree) {
    return <EmptyState message="Press Refresh to capture the UI tree." />;
  }
  const stale =
    capturedForDeviceId !== null && capturedForDeviceId !== activeDeviceId;
  return (
    <div
      className="flex-1 min-h-0 overflow-auto font-mono"
      style={{ fontSize: "var(--fs-11)", whiteSpace: "nowrap" }}
    >
      {stale ? (
        <div
          className="px-3 py-1.5 text-[11px]"
          style={{
            background: "var(--bg-3)",
            color: "var(--fg-2)",
            borderBottom: "1px solid var(--line)",
          }}
        >
          Snapshot is from a different device — refresh to re-capture.
        </div>
      ) : null}
      <div style={{ minWidth: "max-content" }}>
        <TreeNode
          node={tree}
          root={tree}
          path=""
          depth={0}
          selectedPath={selectedPath}
          onSelect={onSelect}
          collapsedPaths={collapsedPaths}
          onToggleExpanded={onToggleExpanded}
          hideNoise={hideNoise}
          showRaw={showRaw}
          interestingPaths={interestingPaths}
        />
      </div>
    </div>
  );
}

interface TreeNodeProps {
  readonly node: UiTreeNode;
  readonly root: UiTreeNode;
  readonly path: UiNodePath;
  readonly depth: number;
  readonly selectedPath: UiNodePath | null;
  readonly onSelect: (path: UiNodePath) => void;
  readonly collapsedPaths: ReadonlySet<UiNodePath>;
  readonly onToggleExpanded: (path: UiNodePath) => void;
  readonly hideNoise: boolean;
  readonly showRaw: boolean;
  readonly interestingPaths: ReadonlySet<UiNodePath>;
}

function TreeNode({
  node,
  root,
  path,
  depth,
  selectedPath,
  onSelect,
  collapsedPaths,
  onToggleExpanded,
  hideNoise,
  showRaw,
  interestingPaths,
}: TreeNodeProps) {
  const hasChildren = node.children.length > 0;
  const expanded = !collapsedPaths.has(path);
  const isSelected = selectedPath === path;
  const label = summarize(node, showRaw);
  const informative = isInformative(node);
  const menuItems = useMemo(() => buildNodeActionItems(node, root), [node, root]);

  // Hide-noise filtering uses a precomputed interest set. A node
  // is "interesting" when it carries its own id/text/label or
  // when any descendant does. Anything outside the set is dead
  // — spacers, transparent wrappers, and class-only rows like
  // empty `window` / `container` whose subtree filters down to
  // nothing all fall through.
  //
  // Single-child `other` wrappers fold even when interesting:
  // the wrapper is structurally redundant (one effective child
  // adds no grouping signal) so the row is skipped and the only
  // effective child renders at the wrapper's own depth. Group
  // `other` with multiple effective children keep their row so
  // the grouping signal is preserved.
  //
  // Tree paths still index through every node regardless of
  // visibility, so selection, attributes, and the canvas overlay
  // stay accurate. Selected rows always render so the user never
  // loses the node they just picked.
  const cls = node.attributes["class"];
  const eligibleChildren: { readonly child: UiTreeNode; readonly index: number }[] =
    [];
  for (let i = 0; i < node.children.length; i++) {
    const c = node.children[i]!;
    const childPath = path === "" ? String(i) : `${path}.${i}`;
    if (hideNoise && !interestingPaths.has(childPath)) continue;
    eligibleChildren.push({ child: c, index: i });
  }
  if (hideNoise && path !== "" && !isSelected) {
    if (!interestingPaths.has(path)) return null;
    if (cls === "other" && eligibleChildren.length === 1) {
      const { child, index } = eligibleChildren[0]!;
      return (
        <TreeNode
          node={child}
          root={root}
          path={`${path}.${index}`}
          depth={depth}
          selectedPath={selectedPath}
          onSelect={onSelect}
          collapsedPaths={collapsedPaths}
          onToggleExpanded={onToggleExpanded}
          hideNoise={hideNoise}
          showRaw={showRaw}
          interestingPaths={interestingPaths}
        />
      );
    }
  }
  const childrenToRender = hideNoise
    ? eligibleChildren
    : node.children.map((child, index) => ({ child, index }));

  return (
    <div>
      <ContextMenu items={menuItems}>
        {({ onContextMenu }) => (
          <button
            type="button"
            onClick={() => onSelect(path)}
            onContextMenu={(e) => {
              onSelect(path);
              onContextMenu(e);
            }}
            className="w-full text-left flex items-center gap-1 py-0.5 pr-2"
            style={{
              minWidth: "max-content",
              paddingLeft: 8 + depth * 12,
              background: isSelected
                ? "var(--bg-sel-inactive)"
                : "transparent",
              color: isSelected
                ? "var(--fg-0)"
                : informative
                  ? "var(--fg-1)"
                  : "var(--fg-3)",
              opacity: !isSelected && !informative ? 0.7 : 1,
              fontStyle: !informative ? "italic" : "normal",
            }}
          >
            <span
              className="inline-flex items-center justify-center w-3 h-3 flex-none"
              onClick={(e) => {
                if (!hasChildren) return;
                e.stopPropagation();
                onToggleExpanded(path);
              }}
              style={{ color: "var(--fg-2)" }}
            >
              {hasChildren ? (
                expanded ? (
                  <ChevronDown className="h-3 w-3" />
                ) : (
                  <ChevronRight className="h-3 w-3" />
                )
              ) : null}
            </span>
            <span style={{ whiteSpace: "nowrap" }}>{label}</span>
          </button>
        )}
      </ContextMenu>
      {hasChildren && expanded
        ? childrenToRender.map(({ child, index }) => (
            <TreeNode
              key={index}
              node={child}
              root={root}
              path={path === "" ? String(index) : `${path}.${index}`}
              depth={depth + 1}
              selectedPath={selectedPath}
              onSelect={onSelect}
              collapsedPaths={collapsedPaths}
              onToggleExpanded={onToggleExpanded}
              hideNoise={hideNoise}
              showRaw={showRaw}
              interestingPaths={interestingPaths}
            />
          ))
        : null}
    </div>
  );
}

function AttributesPanel({ node }: { readonly node: UiTreeNode | null }) {
  const rows = useMemo(() => attributeRows(node), [node]);

  const copyRow = (key: string, value: string) => {
    if (typeof navigator === "undefined") return;
    void navigator.clipboard?.writeText(`${key}=${value}`);
  };

  return (
    <div
      className="flex-none min-h-[8rem] max-h-[40%] overflow-auto"
      style={{
        fontSize: "var(--fs-11)",
        borderTop: "1px solid var(--line)",
        background: "var(--bg-2)",
      }}
    >
      <header
        className="px-3 py-1 uppercase tracking-wider text-[10px]"
        style={{
          color: "var(--fg-2)",
          borderBottom: "1px solid var(--line)",
        }}
      >
        Attributes
      </header>
      {!node ? (
        <div
          className="px-3 py-2 text-[11px]"
          style={{ color: "var(--fg-2)" }}
        >
          Select a node to view its attributes.
        </div>
      ) : rows.length === 0 ? (
        <div
          className="px-3 py-2 text-[11px]"
          style={{ color: "var(--fg-2)" }}
        >
          No attributes reported for this node.
        </div>
      ) : (
        <table className="w-full font-mono">
          <tbody>
            {rows.map(({ key, value }) => (
              <tr
                key={key}
                className="group"
                style={{ borderBottom: "1px solid var(--line)" }}
              >
                <td
                  className="px-3 py-1 align-top"
                  style={{ color: "var(--fg-2)", width: "40%" }}
                >
                  {key}
                </td>
                <td
                  className="px-3 py-1 align-top whitespace-pre-wrap break-all"
                  style={{ color: "var(--fg-0)" }}
                  data-selectable="true"
                >
                  {key === "ext:ios-traits" ? (
                    <TraitChips value={value} />
                  ) : (
                    value
                  )}
                </td>
                <td className="px-2 py-1 align-top w-6">
                  <button
                    type="button"
                    aria-label={`Copy ${key}`}
                    onClick={() => copyRow(key, value)}
                    className="opacity-0 group-hover:opacity-60 hover:!opacity-100"
                  >
                    <Copy className="h-3 w-3" />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

function TraitChips({ value }: { readonly value: string }) {
  const traits = value.split(",").map((s) => s.trim()).filter(Boolean);
  return (
    <span className="inline-flex flex-wrap gap-1">
      {traits.map((t) => (
        <span
          key={t}
          className="px-1.5 py-0.5 rounded-full text-[10px]"
          style={{
            background: "var(--accent-bg)",
            color: "var(--accent)",
            border: "1px solid var(--accent-border)",
            fontFamily: "var(--font-ui)",
          }}
        >
          {t}
        </span>
      ))}
    </span>
  );
}

interface AutoRefreshToggleProps {
  readonly enabled: boolean;
  readonly paused: boolean;
  readonly intervalMs: number;
  readonly disabled: boolean;
  readonly onToggle: () => void;
}

function AutoRefreshToggle({
  enabled,
  paused,
  intervalMs,
  disabled,
  onToggle,
}: AutoRefreshToggleProps) {
  const seconds = Math.round(intervalMs / 1000);
  const title = enabled
    ? paused
      ? `Auto-refresh paused (every ${seconds}s) — touch the mirror to pause; click to disable`
      : `Auto-refresh ON (every ${seconds}s) — click to disable`
    : `Auto-refresh OFF — click to enable (every ${seconds}s)`;
  const color = enabled
    ? paused
      ? "var(--fg-2)"
      : "var(--accent)"
    : undefined;
  return (
    <Button
      variant="ghost"
      size="sm"
      onClick={onToggle}
      disabled={disabled}
      title={title}
      style={color ? { color } : undefined}
    >
      {enabled ? (
        <Pause className="h-3 w-3" />
      ) : (
        <Play className="h-3 w-3" />
      )}
    </Button>
  );
}

function EmptyState({
  message,
  tone,
}: {
  readonly message: string;
  readonly tone?: "danger";
}) {
  return (
    <div
      className="flex-1 flex items-center justify-center px-4 text-center text-xs"
      style={{
        color:
          tone === "danger"
            ? "var(--err)"
            : "var(--fg-2)",
      }}
    >
      {message}
    </div>
  );
}

/**
 * Right-click menu items for a node. Every registered script
 * action becomes one entry, labelled with the default selector
 * so the user sees what YAML they are about to generate. If the
 * node carries no attribute selector, a single disabled hint row
 * explains why nothing is offered.
 */
function buildNodeActionItems(
  node: UiTreeNode,
  root: UiTreeNode,
): readonly ContextMenuEntry[] {
  const selectors = selectorsFromNode(node, root);
  const primary = bestSelector(selectors);
  if (!primary) {
    return [
      {
        id: "no-selector",
        label: "No stable selector",
        disabled: true,
        onSelect: () => undefined,
      },
    ];
  }
  const applicable = SCRIPT_ACTIONS.filter((a) => a.appliesTo(node));
  if (applicable.length === 0) {
    return [
      {
        id: "no-action",
        label: "No applicable actions",
        disabled: true,
        onSelect: () => undefined,
      },
    ];
  }
  const header: ContextMenuEntry = {
    id: "selector-header",
    label: truncate(primary.display, 42),
    disabled: true,
    onSelect: () => undefined,
  };
  return [
    header,
    "separator",
    ...applicable.map((action) => ({
      id: action.id,
      label: action.label,
      onSelect: () => applyAction(action, node, primary),
    })),
  ];
}

function applyAction(
  action: ScriptAction,
  node: UiTreeNode,
  selector: SelectorCandidate,
): void {
  const notifications = getFeature<NotificationsApi>(NOTIFICATIONS_KEY);
  const built = action.buildYaml(node, selector);
  const firstPlaceholder = built.placeholders[0];
  const inserted = insertAtActiveCursor(built.yaml, firstPlaceholder);
  if (inserted) {
    notifications.show({
      kind: "info",
      title: "Inserted into editor",
      detail: `${action.label} by ${selector.display}`,
    });
    return;
  }
  if (typeof navigator !== "undefined" && navigator.clipboard) {
    void navigator.clipboard.writeText(built.yaml);
    notifications.show({
      kind: "info",
      title: "Copied to clipboard",
      detail: "Open a script file to insert directly.",
    });
    return;
  }
  notifications.show({
    kind: "error",
    title: "Cannot insert",
    detail: "No active editor and clipboard is unavailable.",
  });
}

