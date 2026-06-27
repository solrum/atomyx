// Tool-window feature. Side-effect imports register each
// descriptor with toolWindowRegistry at module load — these lines
// are order-sensitive; the stripe buttons render in the order
// below.
import "./registrations/tool-windows-tool-window-structure.js";
import "./registrations/tool-windows-tool-window-device.js";
import "./registrations/tool-windows-tool-window-inspector.js";
import "./registrations/tool-windows-tool-window-problems.js";
import "./registrations/tool-windows-tool-window-todos.js";
import "./registrations/tool-windows-tool-window-terminal.js";
import "./registrations/tool-windows-tool-window-logs.js";
import "../mirror/index.js";

// Public surface consumed by the shell.
export { LeftStripe, RightStripe, BottomStripe, Stripe } from "./tool-windows-stripe.js";
export { BottomToolWindow } from "./tool-windows-bottom-tool-window.js";
export { StructureView } from "./tool-windows-structure-view.js";
