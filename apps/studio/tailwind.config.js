/**
 * Tailwind theme extension — every color utility resolves through
 * the Atomyx design-system CSS custom properties. Components use
 * `bg-ui-primary`, `text-ui-muted`, `border-ui-border`, etc., and
 * the values come from whichever theme is active.
 *
 * Keep this list 1:1 with the attribute catalogue in
 * `domain/theme/types.ts`. Adding a UI-chrome attribute there =
 * exposing a new utility here.
 */
/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        "ui-primary": {
          DEFAULT: "var(--ui-bg-primary-bg, var(--ui-bg-primary))",
          fg: "var(--ui-text-primary)",
        },
        "ui-secondary": {
          DEFAULT: "var(--ui-bg-secondary-bg, var(--ui-bg-secondary))",
          fg: "var(--ui-text-secondary)",
        },
        "ui-tertiary": {
          DEFAULT: "var(--ui-bg-tertiary-bg, var(--ui-bg-tertiary))",
        },
        "ui-border": "var(--ui-border)",
        "ui-text": "var(--ui-text-primary)",
        "ui-text-muted": "var(--ui-text-muted)",
        "ui-accent": {
          DEFAULT: "var(--ui-accent)",
          hover: "var(--ui-accent-hover)",
        },
        "ui-danger": "var(--ui-danger)",
        "ui-success": "var(--ui-success)",
        "diagnostic-error": "var(--diagnostic-error-fg)",
        "diagnostic-warning": "var(--diagnostic-warning-fg)",
        "diagnostic-info": "var(--diagnostic-info-fg)",
        "run-pending": "var(--run-step-pending)",
        "run-running": "var(--run-step-running)",
        "run-pass": "var(--run-step-pass)",
        "run-fail": "var(--run-step-fail)",
        "run-skip": "var(--run-step-skip)",
        "editor-bg": "var(--editor-background-bg, var(--editor-background))",
      },
    },
  },
  plugins: [],
};
