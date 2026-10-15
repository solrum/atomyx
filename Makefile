# Atomyx top-level orchestration Makefile
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
GO_DIR      := $(REPO_ROOT)/core
CAPTURE_FILE := /tmp/atomyx-capture.jsonl
MITM_PORT   := 8889

# Port the Android control server listens on, reached through
# adb forward the same way platforms/android-agent/README.md documents.
ANDROID_AGENT_PORT := 8765

# Bounds the health probe below so a device that is attached but never
# answers fails fast instead of hanging the ready check.
ANDROID_HEALTH_TIMEOUT_S := 3

# The foreground service can still be starting up right after it is
# launched on the device, so the first probe can find no answer yet.
# These bound a retry loop around the probe: up to this many attempts,
# this many seconds apart, each still bounded by
# ANDROID_HEALTH_TIMEOUT_S, so a device that never becomes ready still
# fails in a predictable, bounded time instead of retrying forever.
ANDROID_HEALTH_ATTEMPTS      := 5
ANDROID_HEALTH_RETRY_INTERVAL_S := 1

# GO_VERSION_PKG is the package whose Version var build-core stamps via
# ldflags, so every Go binary's version surface reports exactly what
# this target built. GO_BUILD_VERSION prefers a tag reachable from the
# current commit, falls back to the short commit itself when the repo
# carries no tags yet, and falls back again to the package's own
# unstamped default when git is unavailable at all (for example a
# tarball build with no .git directory).
GO_VERSION_PKG    := dev.atomyx/internal/version
GO_BUILD_VERSION  := $(shell git -C $(REPO_ROOT) describe --tags --always --dirty 2>/dev/null || echo dev)

# GO_RELEASE_BINARIES lists the entry points release-core ships to
# operators, excluding the schema generators, which are developer
# tooling and not a distributed artifact.
GO_RELEASE_BINARIES := atomyx atomyx-mcp atomyx-runtime

# Apple domains to bypass in MITM proxy, because these need direct
# access for code signing verification (OCSP, provisioning).
APPLE_BYPASS := '(.*\.apple\.com|.*\.mzstatic\.com|.*\.icloud\.com)'

# Demo app fixture builds. demo-ios-device reads DEV_TEAM from the
# gitignored platforms/ios-agent/device.env, loaded only for that goal
# because the file carries a contributor's personal signing identity.
DEMO_DIR            := $(REPO_ROOT)/examples/atomyx-demo
DEMO_ANDROID_APK    := build/app/outputs/flutter-apk/app-debug.apk
DEMO_IOS_SIM_DD     := build/ios-sim
DEMO_IOS_SIM_APP    := $(DEMO_IOS_SIM_DD)/Build/Products/Debug-iphonesimulator/Runner.app
DEMO_IOS_DEVICE_DD  := build/ios-device
DEMO_IOS_DEVICE_APP := $(DEMO_IOS_DEVICE_DD)/Build/Products/Release-iphoneos/Runner.app
ifneq ($(filter demo-ios-device,$(MAKECMDGOALS)),)
-include $(IOS_DIR)/device.env
endif

.PHONY: help build build-core release-core check e2e hooks ready-android ready-ios ready-ios-sim \
        ready-ios-capture ready-android-capture \
        stop serve-ios serve-android mitm demo-android demo-ios-sim demo-ios-device

.DEFAULT_GOAL := help

help:
	@echo "Atomyx test orchestration"
	@echo ""
	@echo "SETUP (one-time):"
	@echo "  make build                      Build core binaries and the Studio frontend"
	@echo "  make build-core                 Build core binaries (CLI, MCP, runtime)"
	@echo "  make release-core               Build reproducible, checksummed core release binaries"
	@echo "  make check                      Run every local gate: core + repo-wide lints"
	@echo "  make hooks                      Install the commit size hook (core.hooksPath)"
	@echo "  make e2e PLATFORM=<p>           Run the device-gated end-to-end suite (iossim, android, or ios)"
	@echo "DEMO APP (fixture builds, each prints the output path):"
	@echo "  make demo-android               Debug APK"
	@echo "  make demo-ios-sim               Simulator build, no signing"
	@echo "  make demo-ios-device            Device build, team from platforms/ios-agent/device.env"
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
	@echo "  atomyx run \\"
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

# --- Build ---------------------------------------------------

build: build-core
	@echo "Building Studio..."
	@npm --prefix apps/studio run build
	@echo "Built: core + studio"

# Build the core-runtime binaries (the atomyx CLI, the atomyx-mcp
# stdio server, the atomyx-runtime sidecar, and the schema generators)
# into core/bin/. This is the strategic runtime; the CLI and the MCP
# server in .mcp.json are linked to these binaries. core/Makefile is
# the source of truth for how the core builds; this target only
# delegates so the root stays a thin orchestrator.
build-core:
	@$(MAKE) -C $(GO_DIR) build GO_VERSION_PKG=$(GO_VERSION_PKG) GO_BUILD_VERSION=$(GO_BUILD_VERSION)

# Build the release binaries the way core/Makefile's release target
# ships them: with the local checkout's absolute path trimmed from the
# binary (-trimpath) and the C toolchain disabled (CGO_ENABLED=0,
# safe because no Go code in this module uses cgo), so a build of the
# same commit is byte-identical no matter which machine or directory
# produced it. Emits a SHA256SUMS file next to the binaries so an
# operator can verify a distributed artifact against the checksum
# recorded at release time.
release-core:
	@$(MAKE) -C $(GO_DIR) release GO_VERSION_PKG=$(GO_VERSION_PKG) GO_BUILD_VERSION=$(GO_BUILD_VERSION) GO_RELEASE_BINARIES="$(GO_RELEASE_BINARIES)"

# Every local gate the repository has: the core module's own gate
# (format, build, vet, layer-import, lint, comments, schema freshness,
# race tests, delegated to core/Makefile) plus the repo-wide prose and
# doc-path checks that cover the rest of the tree. Studio's checks run
# separately from apps/studio (see apps/studio/README.md), because they
# need their own npm install and are not part of this target.
check:
	@$(MAKE) -C $(GO_DIR) check
	bash scripts/check-style-symbols.sh
	bash scripts/check-ios-launcher-identifiers.sh
	bash scripts/check-agent-protocol.sh
	bash scripts/check-commit-size-hook.sh
	bash scripts/check-e2e-skip-gate.sh
	bash scripts/check-soak.sh
	bash scripts/check-nightly.sh
	bash scripts/check-e2e-trend.sh
	node scripts/lint-banned-phrases.mjs
	node scripts/lint-doc-paths.mjs

# Run the device-gated end-to-end suite for one platform and write the
# report under e2e-results/. PLATFORM is iossim, android, or ios, and
# scripts/e2e.sh checks the environment that platform needs. It is not
# part of check, because it needs a real device or simulator.
e2e:
	@bash scripts/e2e.sh "$(PLATFORM)"

# Point git at the versioned hooks in scripts/hooks (the commit size
# check). Run once per clone.
hooks:
	@git config core.hooksPath scripts/hooks
	@echo "OK: git hooks now read from scripts/hooks"

# --- Android ---------------------------------------------------

# adb operations below fail with "more than one device/emulator" when
# several devices or emulators are attached. Set ANDROID_SERIAL in the
# environment to pick one; adb devices lists the serials.
ready-android:
	@echo "Starting Android driver..."
	@echo "Checking adb..."
	@adb devices -l | grep -v "List" | grep -v "^$$" || \
		(echo "ERROR: No Android devices. Connect device or start emulator." && exit 1)
	@echo "Checking Android agent..."
	@adb forward tcp:$(ANDROID_AGENT_PORT) tcp:$(ANDROID_AGENT_PORT) >/dev/null
	@attempt=1; \
		while true; do \
			health=$$(curl -s -w '\n%{http_code}' --max-time $(ANDROID_HEALTH_TIMEOUT_S) \
				http://127.0.0.1:$(ANDROID_AGENT_PORT)/health); \
			curl_rc=$$?; \
			status=$$(printf '%s' "$$health" | tail -n1); \
			body=$$(printf '%s' "$$health" | sed '$$d'); \
			if [ $$curl_rc -eq 0 ] && [ "$$status" = "200" ]; then \
				break; \
			fi; \
			if [ $$attempt -ge $(ANDROID_HEALTH_ATTEMPTS) ]; then \
				if [ $$curl_rc -ne 0 ] || [ "$$status" = "000" ]; then \
					echo "ERROR: the Android agent did not answer on port $(ANDROID_AGENT_PORT)."; \
					echo "Enable the accessibility service and start the foreground service on the device, then retry."; \
				else \
					reason=$$(printf '%s' "$$body" | sed -n 's/.*"reason":"\([^"]*\)".*/\1/p'); \
					if [ -z "$$reason" ]; then reason="$$body"; fi; \
					echo "ERROR: the Android agent is not ready: $$reason"; \
					echo "Enable the accessibility service and start the foreground service on the device, then retry."; \
				fi; \
				exit 1; \
			fi; \
			attempt=$$((attempt + 1)); \
			sleep $(ANDROID_HEALTH_RETRY_INTERVAL_S); \
		done
	@echo "OK: Android ready. Run scripts with:"
	@echo "  atomyx run \\"
	@echo "    --file <script.yml> --platform android --device <serial>"

ready-android-capture: mitm ready-android
	@echo ""
	@echo "OK: Android and capture ready. Run with:"
	@echo "  ... --proxy mitmproxy:$(CAPTURE_FILE)"
	@echo ""
	@echo "NOTE: Configure proxy on Android device:"
	@echo "  Settings, then Wi-Fi, then [network], then Proxy, then Manual"
	@echo "  Host: $$(ifconfig en0 | grep 'inet ' | awk '{print $$2}')  Port: $(MITM_PORT)"

# --- iOS Device ---------------------------------------------

ready-ios: serve-ios
	@echo ""
	@echo "OK: iOS device ready. Run scripts with:"
	@echo "  atomyx run \\"
	@echo "    --file <script.yml> --platform ios --device <UDID>"

ready-ios-capture: mitm serve-ios
	@echo ""
	@echo "OK: iOS device and capture ready. Run with:"
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
	@# Start driver (foreground, so it blocks the terminal)
	cd $(IOS_DIR) && make serve-device

# --- Demo app fixtures -----------------------------------------

demo-android:
	cd $(DEMO_DIR) && flutter build apk --debug
	@echo "Output: $(DEMO_DIR)/$(DEMO_ANDROID_APK)"

# Builds for the host architecture only, because the Xcode 27 lipo
# rejects the multi-architecture verify call Flutter 3.38 makes when it
# thins the engine framework for a universal simulator build.
demo-ios-sim:
	cd $(DEMO_DIR) && flutter build ios --simulator --debug --config-only
	@cd $(DEMO_DIR) && xcodebuild -workspace ios/Runner.xcworkspace -scheme Runner \
		-configuration Debug -destination generic/platform=iOS\ Simulator \
		-derivedDataPath $(DEMO_IOS_SIM_DD) ARCHS=$$(uname -m) ONLY_ACTIVE_ARCH=YES \
		CODE_SIGNING_ALLOWED=NO CODE_SIGNING_REQUIRED=NO build
	@echo "Output: $(DEMO_DIR)/$(DEMO_IOS_SIM_APP)"

# Release configuration, because a debug Flutter app cannot launch from
# the home screen on a device. Flutter has no team flag, so Flutter
# prepares the project and xcodebuild signs with DEV_TEAM.
demo-ios-device:
	@if [ -z "$(DEV_TEAM)" ]; then \
		echo "ERROR: DEV_TEAM is not set. Create platforms/ios-agent/device.env"; \
		echo "from device.env.example, or pass DEV_TEAM=<team> on the command line."; \
		exit 1; \
	fi
	cd $(DEMO_DIR) && flutter build ios --release --config-only
	@cd $(DEMO_DIR) && xcodebuild -workspace ios/Runner.xcworkspace -scheme Runner \
		-configuration Release -destination generic/platform=iOS \
		-derivedDataPath $(DEMO_IOS_DEVICE_DD) -allowProvisioningUpdates \
		DEVELOPMENT_TEAM=$(DEV_TEAM) CODE_SIGN_STYLE=Automatic build
	@echo "Output: $(DEMO_DIR)/$(DEMO_IOS_DEVICE_APP)"

# --- iOS Simulator ---------------------------------------------

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

# --- mitmproxy ---------------------------------------------------

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
	@echo "OK: mitmproxy running on :$(MITM_PORT)"
	@echo "  Capture file: $(CAPTURE_FILE)"
	@echo "  Apple domains bypassed (signing verification OK)"

# --- Stop ---------------------------------------------------

stop:
	@echo "Stopping Atomyx processes..."
	@pkill -f "xcodebuild.*AtomyxDriver" 2>/dev/null && echo "  OK: xcodebuild stopped" || true
	@pkill -f "iproxy" 2>/dev/null && echo "  OK: iproxy stopped" || true
	@pkill -f "mitmdump.*$(MITM_PORT)" 2>/dev/null && echo "  OK: mitmdump stopped" || true
	@echo "Done"
