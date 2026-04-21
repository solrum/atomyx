# Device Setup Guide

One-time setup to get Atomyx ready for testing on Android or iOS.

## Quick Start

```bash
# Build everything
make build

# Android — plug device + run
make ready-android

# iOS simulator — boot sim + run
make ready-ios-sim

# iOS physical device — setup once, then run
make ready-ios

# With API capture (mitmproxy)
make ready-ios-capture
```

---

## Android

### Prerequisites
- `adb` installed (Android SDK)
- USB debugging enabled on device
- Atomyx agent APK installed:
  ```bash
  cd platforms/android-agent
  ./gradlew :app:assembleDebug
  adb install -r app/build/outputs/apk/debug/app-debug.apk
  ```

### Run
```bash
make ready-android
atomyx run \
  --file examples/test-login-flow.yml \
  --platform android --device <serial>
```

---

## iOS Simulator

### Prerequisites
- Xcode installed
- Simulator booted (`xcrun simctl boot <UDID>`)

### Run
```bash
make ready-ios-sim
# In another terminal:
atomyx run \
  --file examples/test-login-flow.yml \
  --platform ios --device <UDID>
```

---

## iOS Physical Device

### One-time setup

1. **Create config file:**
   ```bash
   cd platforms/ios-agent
   cp device.env.example device.env
   ```

2. **Edit `device.env`:**
   ```bash
   DEVICE_UDID=<your-device-udid>    # idevice_id -l
   DEV_TEAM=<your-team-id>           # developer.apple.com → Membership
   BUNDLE_ID=com.example.atomyx      # any bundle id under your team
   ```

3. **Build + install (first time, no proxy):**
   ```bash
   cd platforms/ios-agent
   make build-device
   make install-device
   ```

4. **Trust developer cert on device:**
   Settings → General → VPN & Device Management → Trust

### Run
```bash
make ready-ios
# In another terminal:
atomyx run \
  --file examples/test-login-flow.yml \
  --platform ios --device <UDID>
```

---

## API Capture (mitmproxy)

Capture HTTP/HTTPS traffic from the app under test to validate
API responses in your test scripts.

### One-time setup

1. **Install mitmproxy:**
   ```bash
   brew install mitmproxy
   ```

2. **Install mitmproxy CA cert on device:**

   Start a temp HTTP server:
   ```bash
   cd ~/.mitmproxy && python3 -m http.server 9999
   ```

   On device (proxy OFF):
   - Safari → `http://<your-mac-ip>:9999/mitmproxy-ca-cert.pem`
   - Settings → General → VPN & Device Management → Install profile
   - Settings → General → About → Certificate Trust Settings → Enable "mitmproxy"

3. **Configure proxy on device:**
   Settings → Wi-Fi → [network] → HTTP Proxy → Manual
   - Server: `<your-mac-ip>` (find via `ifconfig en0`)
   - Port: `8889`

4. **For iOS physical device — Manual signing:**

   Edit `platforms/ios-agent/device.env`:
   ```bash
   CODE_SIGN_STYLE=Manual
   PROVISIONING_PROFILE_SPECIFIER=<your-wildcard-profile-name>
   ```

   Download a wildcard provisioning profile from
   developer.apple.com → Profiles if you don't have one.

### Run with capture
```bash
# Start everything (mitmdump + driver):
make ready-ios-capture

# In another terminal:
atomyx run \
  --file script.yml \
  --platform ios --device <UDID> \
  --proxy mitmproxy:/tmp/atomyx-capture.jsonl
```

### How it works

```
Device app → HTTP proxy (mitmproxy on Mac:8889)
                ↓
         JSON-lines file ← Atomyx FileCapture reads
                ↓
         capture + assertApi commands validate
```

Apple domains (`*.apple.com`) are automatically bypassed
(`--ignore-hosts`) so code signing verification works through
the proxy.

### Example script with capture
```yaml
appId: com.example.app
name: Login API validation
---
- launchApp
- waitFor: "Login"
- capture: "POST /api/login as: login"
- assertApi:
    from: login
    status: 200
    body:
      $.token: not_empty
```

---

## Stop everything
```bash
make stop
```

---

## Troubleshooting

### "No devices found"
- Android: check `adb devices`, enable USB debugging
- iOS: check `idevice_id -l`, trust Mac on device

### "Developer cert not trusted"
- Turn proxy OFF → Settings → VPN & Device Management → Trust → proxy ON
- Only needed once per device per signing identity

### "capture timeout — 0 requests"
- Check proxy configured on device (Wi-Fi → HTTP Proxy → Manual)
- Check mitmproxy CA trusted on device
- Check `mitmdump` is running (`make mitm` or `make ready-*-capture`)

### iOS "ECONNRESET"
- Restart iproxy: `pkill iproxy && iproxy -u <UDID> 22087:22087 &`
- Or: `make stop && make ready-ios`
