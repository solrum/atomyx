# Atomyx Demo

Reference Flutter app that exercises every Atomyx YML script
command. Use it as the target when developing / smoke-testing
pointer gestures, selector resolution, input handling, and the
rest of the tool surface.

## Screens

| Screen | Route | Elements |
|---|---|---|
| Login | `/login` | Email field, Password field, Login button |
| OTP | `/otp` | OTP input, Verify button |
| Dashboard | `/dashboard` | Welcome text, Premium Features, Products, Account, Sign out |
| Settings | `/settings` | Profile, Notifications toggle, Dark Mode toggle, Version |
| Products | `/products` | Product list with name, category, price |
| Gestures | `/gestures` | Tap, long-press, drag, reorder, flick fixtures for `pointer` |

## Test flows

- **Login → Dashboard**: email `user@example.com` + any password
- **Login → OTP → Dashboard**: email containing `otp` (e.g. `otp@example.com`)
- **Login fail**: empty email or password
- **Dashboard → Settings → back**
- **Dashboard → Products**
- **Dashboard → Gestures**: drives the five pointer fixtures; each card surfaces a counter / status string the script can assert against
- **Dashboard → Sign out → confirm → Login**

## Gestures fixture identifiers

Stable Semantics identifiers on the `/gestures` screen, paired
with the pointer action each exercises:

| Identifier | Visible state | Pointer pattern |
|---|---|---|
| `gesture-tap-target` / `gesture-tap-count` | counter increments per tap | `[down, up]` at one point |
| `gesture-longpress-target` / `gesture-longpress-count` | counter on long-press, container turns green | `[down, up]` with ≥500 ms hold |
| `gesture-drag-target` / `gesture-drag-status` | status `idle` → `dragging` → `released`, dx/dy update | `[down, move…, up]` |
| `gesture-reorder-target` / `gesture-reorder-state` | comma-joined order updates after a row moves | press-and-drag (`[down, wait, move, up]`) |
| `gesture-flick-target` / `gesture-flick-bottom-hits` | counter increments when the list reaches Item 50 | flick (fast `[down, move, up]`) |

## Run

```bash
cd examples/atomyx-demo
flutter run
```

## Bundle ID

- Android: `dev.atomyx.demo`
- iOS: `dev.atomyx.demo`

## Matching YML scripts

- `examples/test-login-flow.yml` — login + OTP branching + handle
  (exercised end-to-end against this fixture)
