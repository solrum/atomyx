# Atomyx — top-level orchestration Makefile
#
# One command to get ready for testing:
#
#   make ready-android                    # Android device/emulator
#   make ready-ios                        # iOS physical device (needs device.env)
#   make ready-ios-sim                    # iOS simulator
#
# Then run scripts:
#   atomyx driver run --file examples/test-settings.yml --platform ios --device <UDID>
#
# With API capture:
#   make ready-ios-capture                # iOS + mitmproxy
#   atomyx driver run --file script.yml --platform ios --device <UDID> --proxy mitmproxy:/tmp/atomyx-capture.jsonl

REPO_ROOT   := $(abspath .)
IOS_DIR     := $(REPO_ROOT)/platforms/ios-agent
ANDROID_DIR := $(REPO_ROOT)/platforms/android-agent
CLI_DIR     := $(REPO_ROOT)/packages/cli
CAPTURE_FILE := /tmp/atomyx-capture.jsonl
MITM_PORT   := 8889

# Apple domains to bypass in MITM proxy — these need direct
# access for code signing verification (OCSP, provisioning).
APPLE_BYPASS := '(.*\.apple\.com|.*\.mzstatic\.com|.*\.icloud\.com)'

.PHONY: help build ready-android ready-ios ready-ios-sim \
        ready-ios-capture ready-android-capture \
        stop serve-ios serve-android mitm

.DEFAULT_GOAL := help

help:
	@echo "Atomyx — test orchestration"
	@echo ""
	@echo "SETUP (one-time):"
	@echo "  make build                      Build all TS packages"
	@echo ""
	@echo "READY (start driver, ready to run scripts):"
	@echo "  make ready-android              Android device/emulator"
	@echo "  make ready-ios                  iOS physical device"
	@echo "  make ready-ios-sim              iOS simulator"
	@echo ""
	@echo "READY + API CAPTURE (with mitmproxy):"
	@echo "  make ready-ios-capture          iOS device + mitmproxy"
	@echo "  make ready-android-capture      Android + mitmproxy"
	@echo ""
	@echo "RUN SCRIPTS:"
	@echo "  node packages/cli/dist/main.js run \\"
	@echo "    --file <script.yml> --platform <ios|android> --device <id>"
	@echo ""
	@echo "  With capture:"
	@echo "    ... --proxy mitmproxy:$(CAPTURE_FILE)"
	@echo ""
	@echo "STOP:"
	@echo "  make stop                       Stop all Atomyx processes"
	@echo ""
	@echo "PREREQUISITES:"
	@echo "  Android: adb + Atomyx agent APK installed"
	@echo "  iOS device: cd platforms/ios-agent && cp device.env.example device.env"
	@echo "  iOS sim: Xcode + booted simulator"
	@echo "  Capture: brew install mitmproxy"

# ── Build ────────────────────────────────────────────────────

build:
	@echo "Building all packages..."
	@for d in shared packages/core packages/driver packages/driver-wire \
	          packages/android-driver packages/ios-driver \
	          packages/script packages/mcp \
	          packages/cli; do \
		echo "  $$d"; \
		(cd "$$d" && npx tsc) || exit 1; \
	done
	@echo "✓ All packages built"

# ── Android ──────────────────────────────────────────────────

ready-android:
	@echo "Starting Android driver..."
	@echo "Checking adb..."
	@adb devices -l | grep -v "List" | grep -v "^$$" || \
		(echo "ERROR: No Android devices. Connect device or start emulator." && exit 1)
	@echo "✓ Android ready. Run scripts with:"
	@echo "  node packages/cli/dist/main.js run \\"
	@echo "    --file <script.yml> --platform android --device <serial>"

ready-android-capture: mitm ready-android
	@echo ""
	@echo "✓ Android + capture ready. Run with:"
	@echo "  ... --proxy mitmproxy:$(CAPTURE_FILE)"
	@echo ""
	@echo "NOTE: Configure proxy on Android device:"
	@echo "  Settings → Wi-Fi → [network] → Proxy → Manual"
	@echo "  Host: $$(ifconfig en0 | grep 'inet ' | awk '{print $$2}')  Port: $(MITM_PORT)"

# ── iOS Device ───────────────────────────────────────────────

ready-ios: serve-ios
	@echo ""
	@echo "✓ iOS device ready. Run scripts with:"
	@echo "  node packages/cli/dist/main.js run \\"
	@echo "    --file <script.yml> --platform ios --device <UDID>"

ready-ios-capture: mitm serve-ios
	@echo ""
	@echo "✓ iOS device + capture ready. Run with:"
	@echo "  ... --proxy mitmproxy:$(CAPTURE_FILE)"

serve-ios:
	@echo "Starting iOS device driver..."
	@test -f $(IOS_DIR)/device.env || \
		(echo "ERROR: platforms/ios-agent/device.env not found." && \
		 echo "  cp platforms/ios-agent/device.env.example platforms/ios-agent/device.env" && \
		 echo "  Edit with your DEVICE_UDID and DEV_TEAM." && exit 1)
	@# Extract DEVICE_UDID from device.env
	$(eval DEVICE_UDID := $(shell grep '^DEVICE_UDID=' $(IOS_DIR)/device.env | cut -d= -f2))
	@# Kill stale processes
	@pkill -f "iproxy.*22087" 2>/dev/null || true
	@# Start iproxy in background
	@iproxy -u $(DEVICE_UDID) 22087:22087 &
	@sleep 1
	@echo "iproxy tunnel started for $(DEVICE_UDID)"
	@# Start driver (foreground — blocks terminal)
	cd $(IOS_DIR) && make serve-device

# ── iOS Simulator ────────────────────────────────────────────

ready-ios-sim:
	@echo "Starting iOS simulator driver..."
	@UDID=$$(xcrun simctl list devices booted 2>/dev/null \
		| grep -Eo '\(([A-F0-9-]{36})\)' | head -1 | tr -d '()'); \
	if [ -z "$$UDID" ]; then \
		echo "ERROR: No booted simulator. Boot one first:"; \
		echo "  xcrun simctl boot <UDID>"; \
		exit 1; \
	fi; \
	echo "Simulator: $$UDID"; \
	cd $(IOS_DIR) && make serve

# ── mitmproxy ────────────────────────────────────────────────

mitm:
	@which mitmdump > /dev/null 2>&1 || \
		(echo "ERROR: mitmproxy not installed. Run: brew install mitmproxy" && exit 1)
	@# Kill existing
	@pkill -f "mitmdump.*$(MITM_PORT)" 2>/dev/null || true
	@sleep 1
	@# Clear capture file
	@> $(CAPTURE_FILE)
	@# Start with Apple domain bypass
	mitmdump \
		-s $(REPO_ROOT)/scripts/mitm-capture-addon.py \
		--set capture_file=$(CAPTURE_FILE) \
		-p $(MITM_PORT) \
		--ssl-insecure \
		--ignore-hosts $(APPLE_BYPASS) \
		-q &
	@sleep 2
	@echo "✓ mitmproxy running on :$(MITM_PORT)"
	@echo "  Capture file: $(CAPTURE_FILE)"
	@echo "  Apple domains bypassed (signing verification OK)"

# ── Stop ─────────────────────────────────────────────────────

stop:
	@echo "Stopping Atomyx processes..."
	@pkill -f "xcodebuild.*AtomyxDriver" 2>/dev/null && echo "  ✓ xcodebuild stopped" || true
	@pkill -f "iproxy" 2>/dev/null && echo "  ✓ iproxy stopped" || true
	@pkill -f "mitmdump.*$(MITM_PORT)" 2>/dev/null && echo "  ✓ mitmdump stopped" || true
	@echo "Done"
