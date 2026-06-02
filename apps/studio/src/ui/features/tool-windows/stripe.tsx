import { Maximize2 } from "lucide-react";
import { useLayout } from "../../../state/features/layout/index.js";
import {
  toolWindowRegistry,
  type ToolWindowDescriptor,
} from "../../shell/tool-window-registry.js";

interface StripeButton {
  readonly id: string;
  readonly icon: React.ReactNode;
  readonly label: string;
  readonly active: boolean;
  readonly badge: number | null;
  readonly onClick: () => void;
}

function buttonsFor(side: "left" | "right" | "bottom"): StripeButton[] {
  return toolWindowRegistry.bySide(side).map(descriptorToButton);
}

function descriptorToButton(d: ToolWindowDescriptor): StripeButton {
  return {
    id: d.id,
    icon: d.icon,
    label: d.label,
    active: d.isVisible(),
    badge: d.badge?.() ?? null,
    onClick: d.toggle,
  };
}

export function LeftStripe() {
  useLayout(); // subscribe for re-render when visibility toggles
  return <Stripe orientation="vertical" buttons={buttonsFor("left")} />;
}

export function RightStripe() {
  useLayout();
  return <Stripe orientation="vertical" buttons={buttonsFor("right")} />;
}

export function BottomStripe() {
  const layout = useLayout();
  const registered = buttonsFor("bottom");
  // Zen is a shell-level toggle, not a tool window — kept in the
  // bottom stripe for discoverability next to the other workspace
  // toggles. If other shell-level toggles appear, extract a small
  // "shell toggles" file and merge here at render time.
  const zen: StripeButton = {
    id: "zen",
    icon: <Maximize2 className="h-3.5 w-3.5" />,
    label: "Zen",
    active: layout.zenMode,
    badge: null,
    onClick: () => layout.toggleZen(),
  };
  return (
    <Stripe orientation="horizontal" buttons={[...registered, zen]} />
  );
}

export function Stripe({
  orientation,
  buttons,
}: {
  readonly orientation: "vertical" | "horizontal";
  readonly buttons: readonly StripeButton[];
}) {
  const containerClass =
    orientation === "vertical"
      ? "flex flex-col items-center"
      : "flex items-stretch h-6";
  const containerStyle: React.CSSProperties =
    orientation === "vertical"
      ? {
          width: 36,
          padding: "6px 0",
          gap: 2,
          background: "var(--chrome-bg)",
          borderLeft: "1px solid var(--line)",
          display: "flex",
          flexDirection: "column",
        }
      : {
          background: "var(--bg-2)",
          borderTop: "1px solid var(--line)",
        };
  return (
    <div className={containerClass} style={containerStyle}>
      {buttons.map((b) => (
        <StripeBtn key={b.id} button={b} orientation={orientation} />
      ))}
    </div>
  );
}

function StripeBtn({
  button,
  orientation,
}: {
  readonly button: StripeButton;
  readonly orientation: "vertical" | "horizontal";
}) {
  if (orientation === "vertical") {
    return (
      <button
        type="button"
        onClick={button.onClick}
        title={button.label}
        aria-label={button.label}
        aria-pressed={button.active}
        className={button.active ? "activity-btn right active" : "activity-btn right"}
      >
        {button.icon}
        {button.badge != null ? (
          <span
            aria-hidden
            style={{
              position: "absolute",
              top: 2,
              right: 2,
              minWidth: 12,
              height: 12,
              padding: "0 3px",
              borderRadius: "var(--r-pill)",
              background: "var(--accent)",
              color: "#0e1e33",
              fontSize: 9,
              fontWeight: 700,
              display: "grid",
              placeItems: "center",
              lineHeight: 1,
            }}
          >
            {button.badge > 99 ? "99+" : button.badge}
          </span>
        ) : null}
      </button>
    );
  }
  return (
    <button
      type="button"
      onClick={button.onClick}
      title={button.label}
      className="flex items-center gap-1 px-2 text-[10px] uppercase tracking-wider h-6"
      style={{
        color: button.active ? "var(--fg-0)" : "var(--fg-2)",
        background: button.active ? "var(--bg-hover)" : "transparent",
      }}
    >
      {button.icon}
      <span>{button.label}</span>
      {button.badge !== null ? (
        <span className="ml-1 text-[10px]" style={{ color: "var(--fg-2)" }}>
          {button.badge}
        </span>
      ) : null}
    </button>
  );
}
