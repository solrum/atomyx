import { useState } from "react";
import { CornerDownLeft } from "lucide-react";

import { getFeature } from "../../../state/core/registry.js";
import type {
  MirrorApi,
  MirrorSessionStatus,
} from "../../../state/features/mirror/index.js";
import { MIRROR_KEY } from "../../../state/features/mirror/index.js";

/**
 * Inline text-entry field for the mirror toolbar. Mirroring has
 * inherent round-trip latency, so typing directly while watching the
 * stream feels laggy. The user composes text in this native Studio
 * field (instant, full IME + paste) and commits it on Enter to
 * whichever field is focused on the device.
 *
 * The commit calls `inputText`, which appends to the device's
 * currently-focused field — the user taps the field on the mirror
 * first, then types here. No device-side field detection is needed:
 * the device routes the text to its focused input.
 */
export function MirrorTextInsert({
  session,
}: {
  readonly session: MirrorSessionStatus;
}) {
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);

  if (!session.capabilities.supportsKeyboard) return null;

  const send = async () => {
    if (!text) return;
    setBusy(true);
    try {
      await getFeature<MirrorApi>(MIRROR_KEY).inputText(session.id, text);
      setText("");
    } catch (err) {
      console.error("[mirror-text-insert] inputText failed", err);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex items-center gap-1">
      <input
        type="text"
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          // Ignore Enter while the IME is still composing (e.g. it
          // confirms a Vietnamese diacritic) so a half-typed word is
          // not sent prematurely.
          if (e.key === "Enter" && !e.nativeEvent.isComposing) {
            e.preventDefault();
            void send();
          }
        }}
        placeholder="Type to send to focused field…"
        className="h-[22px] w-44 rounded px-1.5 text-xs"
        style={{
          background: "var(--bg-1)",
          border: "1px solid var(--line)",
          color: "var(--fg-0)",
        }}
      />
      <button
        type="button"
        onClick={() => void send()}
        disabled={busy || !text}
        title="Send to focused field (↵)"
        className="inline-flex items-center rounded px-1.5 py-0.5 hover:bg-[color:var(--bg-hover)] disabled:opacity-40"
      >
        <CornerDownLeft className="h-3 w-3" />
      </button>
    </div>
  );
}
