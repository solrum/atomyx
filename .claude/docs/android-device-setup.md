# Android device setup — how-to

## Purpose

For contributors or agents setting up or diagnosing a connected
Android test device. Read this when the user asks to "check proxy",
"install cert", "setup proxy", "disable screen lock", or "keep
screen on" for Android.

This does NOT cover iOS setup (see `ios.md`), ADB device pairing,
or the Atomyx Android agent (port 8765 via adb forward).

---

## Proxy

### Proxy tool

**mitmproxy**, listening on port **8889**.

Capture is written to `/tmp/atomyx-capture.jsonl` when started
via the CLI (`--proxy mitmproxy:/tmp/atomyx-capture.jsonl`).

### Step 1 — Verify current state (always run first)

```bash
# 1. Host IP (proxy must point here)
ipconfig getifaddr en0 || ipconfig getifaddr en1

# 2. Proxy currently set on device
adb shell settings get global http_proxy
adb shell settings get global global_http_proxy_host
adb shell settings get global global_http_proxy_port

# 3. mitmproxy running on host
lsof -i :8889 -n -P | grep LISTEN

# 4. CA cert installed on device (hash c8750f0d = mitmproxy default)
adb shell find /data/misc/user/ -name "c8750f0d*" 2>/dev/null
adb shell ls /system/etc/security/cacerts/ | grep c8750f0d
```

Expected healthy state:
- Device proxy = `<host-ip>:8889`
- mitmproxy LISTEN on `*:8889`
- Cert hash `c8750f0d.0` present in `/data/misc/user/0/cacerts-added/`

### Step 2 — Fix proxy IP / port

Run whenever the host IP changes or the port is wrong:

```bash
HOST_IP=$(ipconfig getifaddr en0 || ipconfig getifaddr en1)
adb shell settings put global http_proxy ${HOST_IP}:8889
adb shell settings put global global_http_proxy_host ${HOST_IP}
adb shell settings put global global_http_proxy_port 8889
```

### Step 3 — Start mitmproxy

```bash
mitmdump --listen-port 8889 --listen-host 0.0.0.0 -w /tmp/atomyx-capture.jsonl
```

Run in background when needed during a session:

```bash
mitmdump --listen-port 8889 --listen-host 0.0.0.0 -w /tmp/atomyx-capture.jsonl &
```

### Step 4 — Install CA cert (if missing)

Serve the cert from the host, then install on device via browser.

```bash
# On host — serve the cert directory
python3 -m http.server 9999 --directory ~/.mitmproxy &

# Get host IP to share with device
ipconfig getifaddr en0 || ipconfig getifaddr en1
```

On the device:
1. Open browser → `http://<host-ip>:9999/mitmproxy-ca-cert.pem`
2. Download the file.
3. Settings → Security → Install certificate → CA certificate → select the downloaded file.
4. Verify: re-run the check in Step 1 — hash `c8750f0d` must appear.

> User-installed certs cover browsers and apps that respect the
> user trust store. System-level trust (for apps pinning to the
> system store) requires a rooted device or a debug build.

### Troubleshooting — "No internet" after clearing proxy

**Symptom:** proxy cleared, ping works, but device shows "No internet"
and apps report no connectivity.

**Cause:** Android marks the network `PARTIAL_CONNECTIVITY` when
mitmproxy intercepts the HTTPS captive portal check
(`connectivitycheck.gstatic.com/generate_204`). The flag persists
even after the proxy is removed because Android caches the result.

**Diagnosis:**

```bash
# Confirm internet actually works despite the warning
adb shell curl -s -o /dev/null -w "%{http_code}" --max-time 5 \
  http://connectivitycheck.gstatic.com/generate_204
# → 204 means internet is fine; the problem is the cached flag only

# Check the active WiFi network capability flags
adb shell dumpsys connectivity 2>/dev/null \
  | grep "NetworkAgentInfo" | grep "WIFI CONNECTED" \
  | grep -oE "PARTIAL_CONNECTIVITY|VALIDATED"
```

**Fix** (testing device — captive portal detection not needed):

```bash
# Disable captive portal check, reconnect WiFi to clear the flag
adb shell settings put global captive_portal_mode 0
adb shell svc wifi disable && sleep 2 && adb shell svc wifi enable
```

After reconnecting, the network reports `VALIDATED` and the "No
internet" warning disappears. `captive_portal_mode 0` is kept
permanently on testing devices — use `ap reset` to apply in one step.

### Quick-reference: proxy healthy state

```bash
echo "=== Host IP ===" && (ipconfig getifaddr en0 || ipconfig getifaddr en1)
echo "=== Device proxy ===" && adb shell settings get global http_proxy
echo "=== mitmproxy ===" && (lsof -i :8889 -n -P | grep LISTEN || echo "NOT running")
echo "=== CA cert ===" && (adb shell find /data/misc/user/ -name "c8750f0d*" 2>/dev/null || echo "NOT installed")
```

---

## Screen lock / screen timeout

Android Settings UI caps screen timeout at 10 minutes. Use ADB to
bypass both limits.

### Verify current state

```bash
adb shell settings get system screen_off_timeout              # ms until screen off
adb shell settings get secure lock_screen_lock_after_timeout  # ms after screen off until lock
adb shell settings get global stay_on_while_plugged_in        # 7 = USB+AC+Wireless
```

### Disable auto screen-off and auto lock (testing mode)

```bash
# Keep screen on indefinitely (~24 days) while plugged in
adb shell settings put system screen_off_timeout 2147483647
# Delay lock indefinitely after screen off
adb shell settings put secure lock_screen_lock_after_timeout 2147483647
# Ensure stay-on covers all power sources (USB=1, AC=2, Wireless=4)
adb shell settings put global stay_on_while_plugged_in 7
```

Expected result: device connected via USB never turns off or locks.

### Restore defaults

```bash
adb shell settings put system screen_off_timeout 600000
adb shell settings put secure lock_screen_lock_after_timeout 5000
```
