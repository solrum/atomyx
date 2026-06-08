import { useEffect, useMemo, useState } from "react";
import { Check, Plus, Smartphone, X as XIcon } from "lucide-react";
import { useApps } from "../../../state/features/apps/index.js";
import { useDevices } from "../../../state/features/devices/index.js";
import { useEditor } from "../../../state/features/editor/index.js";
import { useWorkspace } from "../../../state/features/workspace/index.js";
import { SCRIPT_TEMPLATES } from "./welcome-templates.js";
import type { App, Device } from "../../../domain/features/runtime/index.js";

type Step = 0 | 1 | 2 | 3;

interface StepDef {
  readonly id: "device" | "app" | "details" | "template";
  readonly label: string;
}

const STEPS: readonly StepDef[] = [
  { id: "device", label: "Device" },
  { id: "app", label: "App" },
  { id: "details", label: "Details" },
  { id: "template", label: "Template" },
];

export interface NewTestWizardProps {
  readonly open: boolean;
  readonly onClose: () => void;
}

export function NewTestWizard({ open, onClose }: NewTestWizardProps) {
  const [step, setStep] = useState<Step>(0);
  const [deviceId, setDeviceId] = useState<string | null>(null);
  const [appId, setAppId] = useState<string | null>(null);
  const [name, setName] = useState("New test");
  const [path, setPath] = useState("flows/new-test.yml");
  const [tags, setTags] = useState<readonly string[]>(["smoke"]);
  const [tagInput, setTagInput] = useState("");
  const [templateId, setTemplateId] = useState<string>(
    SCRIPT_TEMPLATES[0]!.id,
  );

  const { currentPath, refresh, createScript } = useWorkspace();
  const { openFile } = useEditor();
  const { devices, refresh: refreshDevices } = useDevices();
  const { byDevice, refresh: refreshApps } = useApps();
  const apps: readonly App[] = deviceId ? (byDevice[deviceId] ?? []) : [];

  useEffect(() => {
    if (!open) return;
    setStep(0);
    void refreshDevices();
  }, [open, refreshDevices]);

  useEffect(() => {
    if (!deviceId) return;
    void refreshApps(deviceId);
  }, [deviceId, refreshApps]);

  const template = useMemo(
    () =>
      SCRIPT_TEMPLATES.find((t) => t.id === templateId) ??
      SCRIPT_TEMPLATES[0]!,
    [templateId],
  );

  const canNext = [
    !!deviceId,
    !!appId,
    name.trim().length > 0 && /\.ya?ml$/.test(path.trim()),
    !!templateId,
  ][step];

  if (!open) return null;

  const handleSetName = (v: string) => {
    setName(v);
    const slug = v
      .toLowerCase()
      .trim()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-|-$/g, "");
    if (slug) setPath(`flows/${slug}.yml`);
  };

  const addTag = (raw: string) => {
    const t = raw.trim().replace(/^[,#]+|[,#]+$/g, "");
    if (!t || tags.includes(t)) return;
    setTags([...tags, t]);
    setTagInput("");
  };

  async function handleFinish() {
    if (!currentPath || !appId) return;
    const fileName = path.trim().replace(/^\/+/, "");
    const content = template.render({
      appId,
      name,
      tags: [...tags],
    });
    const created = await createScript(currentPath, fileName, content);
    await refresh();
    await openFile(created);
    onClose();
  }

  return (
    <div
      className="overlay"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="modal" style={{ width: "min(760px, 92vw)" }}>
        <div className="modal-header">
          <div
            style={{
              width: 28,
              height: 28,
              borderRadius: 6,
              background: "var(--accent-bg)",
              display: "grid",
              placeItems: "center",
              color: "var(--accent)",
            }}
          >
            <Plus style={{ width: 14, height: 14 }} />
          </div>
          <div>
            <div className="modal-title">New test</div>
            <div className="modal-sub">
              Create a new script in this workspace.
            </div>
          </div>
          <div style={{ flex: 1 }} />
          <button type="button" className="btn ghost" onClick={onClose}>
            <XIcon style={{ width: 12, height: 12 }} />
          </button>
        </div>

        <Stepper current={step} />

        <div className="modal-body" style={{ minHeight: 340 }}>
          {step === 0 && (
            <DeviceStep
              devices={devices}
              deviceId={deviceId}
              onSelect={setDeviceId}
            />
          )}
          {step === 1 && (
            <AppStep apps={apps} appId={appId} onSelect={setAppId} />
          )}
          {step === 2 && (
            <DetailsStep
              name={name}
              setName={handleSetName}
              path={path}
              setPath={setPath}
              tags={tags}
              setTags={setTags}
              tagInput={tagInput}
              setTagInput={setTagInput}
              addTag={addTag}
            />
          )}
          {step === 3 && (
            <TemplateStep
              templateId={templateId}
              onSelect={setTemplateId}
              summary={{
                deviceName:
                  devices.find((d) => d.id === deviceId)?.name ?? "—",
                appLabel: apps.find((a) => a.id === appId)?.name ?? appId ?? "—",
                appId: appId ?? "",
                name,
                path,
                tags,
                templateLabel: template.label,
              }}
            />
          )}
        </div>

        <div className="modal-footer">
          <button type="button" className="btn ghost" onClick={onClose}>
            Cancel
          </button>
          <div style={{ flex: 1 }} />
          {step > 0 ? (
            <button
              type="button"
              className="btn"
              onClick={() => setStep((step - 1) as Step)}
            >
              Back
            </button>
          ) : null}
          {step < 3 ? (
            <button
              type="button"
              className="btn primary"
              disabled={!canNext}
              onClick={() => setStep((step + 1) as Step)}
            >
              Continue
            </button>
          ) : (
            <button
              type="button"
              className="btn primary"
              disabled={!canNext || !currentPath}
              title={
                currentPath
                  ? undefined
                  : "Open a folder first — the new script needs a destination."
              }
              onClick={handleFinish}
            >
              <Check style={{ width: 11, height: 11 }} /> Create test
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

function Stepper({ current }: { readonly current: Step }) {
  return (
    <div className="stepper">
      {STEPS.map((s, i) => (
        <span key={s.id} className="contents">
          <span
            className={
              i === current
                ? "s active"
                : i < current
                  ? "s done"
                  : "s"
            }
          >
            <span className="n">
              {i < current ? (
                <Check style={{ width: 9, height: 9 }} />
              ) : (
                i + 1
              )}
            </span>
            <span className="label">{s.label}</span>
          </span>
          {i < STEPS.length - 1 ? <span className="arrow">›</span> : null}
        </span>
      ))}
    </div>
  );
}

function DeviceStep({
  devices,
  deviceId,
  onSelect,
}: {
  readonly devices: readonly Device[];
  readonly deviceId: string | null;
  readonly onSelect: (id: string) => void;
}) {
  return (
    <div>
      <div
        style={{ color: "var(--fg-2)", fontSize: 12, marginBottom: 12 }}
      >
        Pick the simulator or device the script will target by default.
        You can override per-run later.
      </div>
      {devices.length === 0 ? (
        <div
          style={{
            color: "var(--warn)",
            fontSize: 12,
            padding: 12,
            background: "var(--warn-bg)",
            borderRadius: "var(--r-3)",
          }}
        >
          No devices detected. Connect a device or start a simulator,
          then reopen this wizard.
        </div>
      ) : (
        <div className="option-grid cols-2">
          {devices.map((d) => (
            <button
              key={d.id}
              type="button"
              className={d.id === deviceId ? "option selected" : "option"}
              onClick={() => onSelect(d.id)}
            >
              <span className="opt-ico">
                <Smartphone style={{ width: 18, height: 18 }} />
              </span>
              <span style={{ minWidth: 0 }}>
                <div className="opt-title truncate">{d.name}</div>
                <div className="opt-sub">
                  {d.platform}
                  {d.osVersion ? ` · ${d.osVersion}` : ""}
                </div>
              </span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function AppStep({
  apps,
  appId,
  onSelect,
}: {
  readonly apps: readonly App[];
  readonly appId: string | null;
  readonly onSelect: (id: string) => void;
}) {
  const [manual, setManual] = useState("");
  return (
    <div>
      <div
        style={{ color: "var(--fg-2)", fontSize: 12, marginBottom: 12 }}
      >
        Bundle ID of the app under test. Used as{" "}
        <code style={{ fontFamily: "var(--font-mono)" }}>appId</code> in
        the script header.
      </div>
      {apps.length > 0 ? (
        <div className="option-grid">
          {apps.map((a) => (
            <button
              key={a.id}
              type="button"
              className={a.id === appId ? "option selected" : "option"}
              onClick={() => onSelect(a.id)}
            >
              <span className="opt-ico">📦</span>
              <span style={{ minWidth: 0 }}>
                <div className="opt-title truncate">{a.name}</div>
                <div className="opt-sub" style={{ fontFamily: "var(--font-mono)" }}>
                  {a.id}
                </div>
              </span>
            </button>
          ))}
        </div>
      ) : null}
      <div className="field" style={{ marginTop: 16 }}>
        <label>Or enter bundle ID</label>
        <div style={{ display: "flex", gap: 6 }}>
          <input
            className="input mono"
            placeholder="com.company.app"
            value={manual}
            onChange={(e) => setManual(e.target.value)}
          />
          <button
            type="button"
            className="btn"
            disabled={!manual.trim()}
            onClick={() => onSelect(manual.trim())}
          >
            Use
          </button>
        </div>
      </div>
    </div>
  );
}

function DetailsStep({
  name,
  setName,
  path,
  setPath,
  tags,
  setTags,
  tagInput,
  setTagInput,
  addTag,
}: {
  readonly name: string;
  readonly setName: (v: string) => void;
  readonly path: string;
  readonly setPath: (v: string) => void;
  readonly tags: readonly string[];
  readonly setTags: (v: readonly string[]) => void;
  readonly tagInput: string;
  readonly setTagInput: (v: string) => void;
  readonly addTag: (raw: string) => void;
}) {
  return (
    <div>
      <div className="field">
        <label>Test name</label>
        <input
          className="input"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Login — valid credentials"
        />
        <span className="hint">
          Human-readable title. Stored as{" "}
          <code style={{ fontFamily: "var(--font-mono)" }}>name:</code> in
          the YAML.
        </span>
      </div>
      <div className="field">
        <label>File path</label>
        <input
          className="input mono"
          value={path}
          onChange={(e) => setPath(e.target.value)}
        />
        <span className="hint">
          Relative to workspace root. Must end in{" "}
          <code style={{ fontFamily: "var(--font-mono)" }}>.yml</code>.
        </span>
      </div>
      <div className="field">
        <label>Tags</label>
        <div
          style={{
            display: "flex",
            gap: 4,
            flexWrap: "wrap",
            padding: "4px 6px",
            border: "1px solid var(--line)",
            borderRadius: "var(--r-3)",
            background: "var(--bg-2)",
            minHeight: 32,
            alignItems: "center",
          }}
        >
          {tags.map((t) => (
            <span key={t} className="chip">
              {t}
              <span
                className="x"
                onClick={() => setTags(tags.filter((x) => x !== t))}
              >
                ×
              </span>
            </span>
          ))}
          <input
            value={tagInput}
            onChange={(e) => setTagInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === ",") {
                e.preventDefault();
                addTag(tagInput);
              } else if (e.key === "Backspace" && !tagInput && tags.length) {
                setTags(tags.slice(0, -1));
              }
            }}
            onBlur={() => tagInput && addTag(tagInput)}
            placeholder={tags.length ? "" : "smoke, login, auth"}
            style={{
              border: 0,
              outline: 0,
              background: "transparent",
              color: "var(--fg-0)",
              font: "inherit",
              fontSize: "var(--fs-13)",
              flex: 1,
              minWidth: 80,
              padding: "2px 4px",
            }}
          />
        </div>
        <span className="hint">
          Press <span className="kbd">Enter</span> or{" "}
          <span className="kbd">,</span> to add. Used for filtering runs in
          CI.
        </span>
      </div>
    </div>
  );
}

interface TemplateSummary {
  readonly deviceName: string;
  readonly appLabel: string;
  readonly appId: string;
  readonly name: string;
  readonly path: string;
  readonly tags: readonly string[];
  readonly templateLabel: string;
}

function TemplateStep({
  templateId,
  onSelect,
  summary,
}: {
  readonly templateId: string;
  readonly onSelect: (id: string) => void;
  readonly summary: TemplateSummary;
}) {
  return (
    <div>
      <div
        style={{ color: "var(--fg-2)", fontSize: 12, marginBottom: 12 }}
      >
        Start from a template. You can always strip it down or reshape
        after creation.
      </div>
      <div className="option-grid cols-2">
        {SCRIPT_TEMPLATES.map((t) => (
          <button
            key={t.id}
            type="button"
            className={t.id === templateId ? "option selected" : "option"}
            onClick={() => onSelect(t.id)}
          >
            <span style={{ minWidth: 0 }}>
              <div className="opt-title">{t.label}</div>
              <div className="opt-sub">{t.description}</div>
            </span>
          </button>
        ))}
      </div>
      <div
        style={{
          marginTop: 18,
          padding: 12,
          background: "var(--bg-2)",
          border: "1px solid var(--line-soft)",
          borderRadius: 6,
        }}
      >
        <div
          style={{
            fontSize: 10,
            color: "var(--fg-2)",
            textTransform: "uppercase",
            letterSpacing: "0.06em",
            fontWeight: 600,
            marginBottom: 6,
          }}
        >
          Summary
        </div>
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "100px 1fr",
            rowGap: 4,
            fontSize: 12,
          }}
        >
          <div style={{ color: "var(--fg-2)" }}>Device</div>
          <div>{summary.deviceName}</div>
          <div style={{ color: "var(--fg-2)" }}>App</div>
          <div style={{ fontFamily: "var(--font-mono)" }}>
            {summary.appId || summary.appLabel}
          </div>
          <div style={{ color: "var(--fg-2)" }}>Name</div>
          <div>{summary.name}</div>
          <div style={{ color: "var(--fg-2)" }}>File</div>
          <div
            style={{
              fontFamily: "var(--font-mono)",
              color: "var(--accent)",
            }}
          >
            {summary.path}
          </div>
          <div style={{ color: "var(--fg-2)" }}>Tags</div>
          <div>
            {summary.tags.length === 0 ? (
              <span style={{ color: "var(--fg-3)" }}>none</span>
            ) : (
              summary.tags.join(", ")
            )}
          </div>
          <div style={{ color: "var(--fg-2)" }}>Template</div>
          <div>{summary.templateLabel}</div>
        </div>
      </div>
    </div>
  );
}
