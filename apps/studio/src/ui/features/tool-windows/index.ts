// Tool-window feature. Side-effect imports register each
// descriptor with toolWindowRegistry at module load — these lines
// are order-sensitive; the stripe buttons render in the order
// below.
import "./registrations/tool-window-structure.js";
import "./registrations/tool-window-device.js";
import "./registrations/tool-window-inspector.js";
import "./registrations/tool-window-problems.js";
import "./registrations/tool-window-todos.js";
import "./registrations/tool-window-terminal.js";
import "./registrations/tool-window-logs.js";
import "../mirror/index.js";

// Public surface consumed by the shell.
export { LeftStripe, RightStripe, BottomStripe, Stripe } from "./stripe.js";
export { BottomToolWindow } from "./bottom-tool-window.js";
export { StructureView } from "./structure-view.js";
