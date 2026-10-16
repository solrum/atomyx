# Atomyx iOS driver: Makefile wrapper around xcodegen + xcodebuild.
#
# Simulator usage (zero config):
#   make setup                        # generate project + build
#   make test                         # unit tests
#   make serve                        # run driver (blocks)
#
# Physical-device usage:
#   cp device.env.example device.env  # one-time: copy + edit
#   make setup-device
#   make serve-device
#
# Command-line overrides still work and take precedence over the env
# file, so scripted callers (CI, multi-device setups) can bypass the
# file entirely:
#   make serve-device DEVICE_UDID=<udid> DEV_TEAM=<team> BUNDLE_ID=<id>

# Load per-contributor device config from `device.env` if it exists.
# Template is committed as `device.env.example`; real file is gitignored.
# The `-` prefix makes the include optional, so the simulator flow works
# without any device config.
#
# Restricted to device goals because device.env carries a contributor's
# personal signing identity (BUNDLE_ID, DEV_TEAM); loading it unconditionally
# would leak that identity into simulator builds, which need no signing.
DEVICE_GOALS := build-device install-device test-device serve-device setup-device check-device
ifneq ($(filter $(DEVICE_GOALS),$(MAKECMDGOALS)),)
-include device.env
endif

PROJECT   := AtomyxDriver.xcodeproj
SCHEME    := AtomyxDriver
DERIVED   := ./build
REPO_ROOT := $(abspath ../..)

# Stamp files marking that build-for-testing has produced a runnable
# test bundle for the simulator and device destinations respectively.
# Kept apart because the two destinations build into the same derived
# data path but are not interchangeable: a stamp only means its own
# destination is ready to serve. Each stamp depends on the Swift sources
# and the project definition, so editing either rebuilds the runner
# before it serves. A runner built before launch identities existed is
# refused when it answers a ping, and a stale stamp would keep serving it.
BUILD_STAMP        := $(DERIVED)/.build-for-testing
DEVICE_BUILD_STAMP := $(DERIVED)/.build-for-testing-device
SWIFT_SOURCES      := $(shell find Tests App -name '*.swift' 2>/dev/null)

# Every step that compiles into $(DERIVED) runs under an exclusive flock on
# $(DERIVED)/.build.lock, the same lock the Go launcher takes before its own
# build, so a make target and a launcher never build into the shared
# directory at the same time. perl supplies flock(2) because neither macOS
# nor stock Linux images share one lock utility, and macOS lockf uses a
# different locking family that does not exclude a flock holder. The lock
# is released when the build command exits.
LOCK_RUN = perl -MFcntl=:flock -e 'open(my $$f, ">>", shift) or die "build lock: $$!"; flock($$f, LOCK_EX) or die "build lock: $$!"; system(@ARGV); exit($$? >> 8)' $(DERIVED)/.build.lock

# --- Simulator defaults (auto-detected) -----------------------------
# Auto-detect the first booted simulator's UDID. The 36-char anchor
# in the grep pattern ensures we only match the UUID, not `(Booted)`.
UDID ?= $(shell xcrun simctl list devices booted 2>/dev/null \
                | grep -Eo '\(([A-F0-9-]{36})\)' | head -1 | tr -d '()')
DESTINATION ?= platform=iOS Simulator,id=$(UDID)

# --- Physical device config (loaded from device.env) -----------------
# These defaults make `make help` readable when device.env is absent.
# The `check-device` target will still fail with an actionable message if
# either DEVICE_UDID or DEV_TEAM is missing at target invocation time.
#
# BUNDLE_ID is overridable because some contributors may need a unique
# bundle id (Apple Developer team conflicts, multiple concurrent
# driver instances on one team, etc.). Default matches project.yml.
DEVICE_UDID ?=
DEV_TEAM    ?=
BUNDLE_ID   ?= dev.atomyx.driver.host
IPROXY_PORT ?= 22087
DEVICE_DESTINATION ?= platform=iOS,id=$(DEVICE_UDID)

# Runner launch environment. xcodebuild forwards every TEST_RUNNER_<NAME>
# variable to the test runner as <NAME>. ATOMYX_PORT overrides the port the
# runner listens on (default 22087), so a hand-started runner can sit next to
# a launcher-managed one. serve-device always reports DEVICE_UDID as the
# runner target identity.
ATOMYX_PORT ?=
# serve and serve-device run the products that build-for-testing wrote into
# $(DERIVED), from a derived data path of their own, so a hand-started runner
# never shares mutable state with a launcher-managed one or with a runner for
# another target. The xctestrun file is resolved when the recipe runs, after
# the build prerequisite has produced it.
RUN_DERIVED ?= $(DERIVED)/run-$(if $(DEVICE_UDID),$(DEVICE_UDID),$(UDID))
XCTESTRUN_SIM    = $(lastword $(sort $(wildcard $(DERIVED)/Build/Products/$(SCHEME)_iphonesimulator*.xctestrun)))
XCTESTRUN_DEVICE = $(lastword $(sort $(wildcard $(DERIVED)/Build/Products/$(SCHEME)_iphoneos*.xctestrun)))
SERVE_ENV := $(if $(ATOMYX_PORT),TEST_RUNNER_ATOMYX_PORT=$(ATOMYX_PORT))
SERVE_DEVICE_ENV := $(SERVE_ENV) TEST_RUNNER_ATOMYX_TARGET_UDID=$(DEVICE_UDID)

# Build-setting overrides passed to xcodebuild for device targets.
# Uses xcodebuild's space-separated KEY=VALUE syntax which overrides
# whatever the generated .xcodeproj declares.
#
# Signing mode: Manual with wildcard provisioning profile by default.
# Automatic signing requires network access to Apple servers on every
# build, which is problematic when a MITM proxy is active (the proxy's CA
# cert blocks Xcode's server trust). Manual signing with a wildcard
# profile works offline after initial profile download.
#
# To use Automatic signing instead, override:
#   make build-device CODE_SIGN_STYLE=Automatic
#
# NOTE: PRODUCT_BUNDLE_IDENTIFIER is deliberately NOT overridden here
# because xcodebuild applies command-line setting overrides to ALL
# targets in the invocation, so the host app AND test bundle would get the
# same bundle id, conflicting with the test target which needs a
# distinct suffix. Instead, BUNDLE_ID is baked into the generated
# .xcodeproj via XcodeGen's ${BUNDLE_ID} env var substitution in
# project.yml. Re-run `make generate` after changing BUNDLE_ID.
CODE_SIGN_STYLE    ?= Automatic
PROVISIONING_PROFILE_SPECIFIER ?=

SIGN_OVERRIDES := \
	CODE_SIGN_STYLE=$(CODE_SIGN_STYLE) \
	CODE_SIGNING_REQUIRED=YES \
	CODE_SIGNING_ALLOWED=YES \
	DEVELOPMENT_TEAM=$(DEV_TEAM)

# Only pass PROVISIONING_PROFILE_SPECIFIER when set, because an empty
# value confuses xcodebuild with Automatic signing.
ifneq ($(PROVISIONING_PROFILE_SPECIFIER),)
SIGN_OVERRIDES += "PROVISIONING_PROFILE_SPECIFIER=$(PROVISIONING_PROFILE_SPECIFIER)"
endif

.PHONY: help generate build test serve setup clean check-udid \
        build-device install-device test-device serve-device setup-device check-device
# $(PROJECT), $(BUILD_STAMP), and $(DEVICE_BUILD_STAMP) are real file
# targets, deliberately left out of .PHONY, so make only rebuilds them
# when their prerequisites are newer (see the targets themselves below).

.DEFAULT_GOAL := help

help:
	@echo "Atomyx iOS driver targets:"
	@echo ""
	@echo "Simulator (zero config, auto-detects booted sim):"
	@echo "  make setup       xcodegen + xcodebuild build-for-testing"
	@echo "  make generate    xcodegen only"
	@echo "  make build       xcodebuild build-for-testing"
	@echo "  make test        run unit tests (serve test auto-skipped)"
	@echo "  make serve       run in server mode (blocks terminal)"
	@echo "  make pointer-smoke  run pointer-gesture smoke (7 W3C patterns)"
	@echo "  make lint-isolation check private XCTest isolation (ADR-001)"
	@echo "  make clean       remove build/ and AtomyxDriver.xcodeproj"
	@echo ""
	@echo "Physical device (needs device.env or CLI overrides):"
	@echo "  cp device.env.example device.env   # one-time: copy + edit"
	@echo "  make setup-device                    # generate + build"
	@echo "  make build-device                    # build only (offline OK after first)"
	@echo "  make install-device                  # build + install app (needs USB, no serve)"
	@echo "  make serve-device                    # serve only (app must be installed)"
	@echo "  make test-device                     # run unit tests on device"
	@echo ""
	@echo "Config precedence (Make default): command line > device.env > built-in defaults"
	@echo ""
	@echo "Simulator variables:"
	@echo "  UDID=<uuid>               Simulator UDID (auto-detected)"
	@echo "  DESTINATION='...'         Full -destination override"
	@echo ""
	@echo "Device variables (from device.env or CLI):"
	@echo "  DEVICE_UDID=<udid>        Physical device UDID (idevice_id -l)"
	@echo "  DEV_TEAM=<team-id>        Apple Developer Team ID (10-char)"
	@echo "  BUNDLE_ID=<id>            Host app bundle id (default: dev.atomyx.driver.host)"
	@echo "  IPROXY_PORT=<port>        Tunnel port (default 22087)"
	@echo "  ATOMYX_PORT=<port>        Runner listen port for serve (default 22087)"
	@echo ""
	@echo "Prerequisites for device targets:"
	@echo "  brew install libimobiledevice  # iproxy tunneling"
	@echo "  Xcode, then Settings, then Accounts, then add Apple ID"
	@echo ""
	@echo "Resolved values (current run):"
	@echo "  UDID        = $(UDID)"
	@echo "  DESTINATION = $(DESTINATION)"
	@echo "  DEVICE_UDID = $(DEVICE_UDID)"
	@echo "  DEV_TEAM    = $(DEV_TEAM)"
	@echo "  BUNDLE_ID   = $(BUNDLE_ID)"

check-udid:
	@if [ -z "$(UDID)" ]; then \
		echo "ERROR: No booted simulator detected and no UDID provided."; \
		echo "Boot a simulator (Simulator.app or xcrun simctl boot <UDID>)"; \
		echo "or pass UDID explicitly: make <target> UDID=<uuid>"; \
		exit 1; \
	fi

# The generated Xcode project is not checked into git (see
# .gitignore) and is rebuilt from project.yml. Making it a real file
# target keyed on project.yml means a fresh clone generates it once,
# and an unchanged checkout skips xcodegen entirely, instead of
# regenerating on every build or serve invocation.
#
# BUNDLE_ID and CODE_SIGN_STYLE are baked into the generated
# .xcodeproj through this substitution. Changing either on the
# command line without also touching project.yml will not trigger a
# rebuild here, so run `make generate` to force one.
$(PROJECT): project.yml
	BUNDLE_ID=$(BUNDLE_ID) \
	CODE_SIGN_STYLE=$(CODE_SIGN_STYLE) \
	PROVISIONING_PROFILE_SPECIFIER="$(PROVISIONING_PROFILE_SPECIFIER)" \
	DEV_TEAM=$(DEV_TEAM) \
	xcodegen generate
	@touch -c $(PROJECT)

# Forces regeneration regardless of project.yml's timestamp, for
# example after changing BUNDLE_ID or CODE_SIGN_STYLE on the command
# line with project.yml itself unchanged.
generate:
	@rm -rf $(PROJECT)
	@$(MAKE) $(PROJECT)

build: check-udid $(PROJECT)
	@mkdir -p $(DERIVED)
	$(LOCK_RUN) xcodebuild build-for-testing \
		-project $(PROJECT) \
		-scheme $(SCHEME) \
		-destination '$(DESTINATION)' \
		-derivedDataPath $(DERIVED)

test: check-udid $(PROJECT)
	@mkdir -p $(DERIVED)
	$(LOCK_RUN) xcodebuild test \
		-project $(PROJECT) \
		-scheme $(SCHEME) \
		-destination '$(DESTINATION)' \
		-derivedDataPath $(DERIVED) \
		-only-testing:AtomyxDriverAgent/CommandHandlerUnitTests \
		-only-testing:AtomyxDriverAgent/DeviceOrientationTests

# build-for-testing output for the simulator destination. A fresh
# clone has neither $(PROJECT) nor this stamp, so serve's prerequisite
# chain below builds both automatically; an unchanged checkout skips
# straight to serving.
$(BUILD_STAMP): $(PROJECT) $(SWIFT_SOURCES)
	@mkdir -p $(DERIVED)
	$(LOCK_RUN) xcodebuild build-for-testing \
		-project $(PROJECT) \
		-scheme $(SCHEME) \
		-destination '$(DESTINATION)' \
		-derivedDataPath $(DERIVED)
	@mkdir -p $(DERIVED)
	@touch $(BUILD_STAMP)

serve: check-udid $(BUILD_STAMP)
	@rm -rf $(RUN_DERIVED)/serve.xcresult
	$(SERVE_ENV) xcodebuild test-without-building \
		-xctestrun $(XCTESTRUN_SIM) \
		-destination '$(DESTINATION)' \
		-derivedDataPath $(RUN_DERIVED) \
		-resultBundlePath $(RUN_DERIVED)/serve.xcresult \
		-only-testing:AtomyxDriverAgent/AtomyxDriverAgent/testServeCommands

# Pointer-gesture smoke: runs 7 W3C patterns against a live
# driver. Expects `make serve` (or `make serve-device`) already
# running in another terminal; optionally set POINTER_MODE to
# tighten expectations for CI (private = all 7 pass; public =
# 5 pass + 2 skipped).
pointer-smoke:
	cd $(REPO_ROOT) && npm run build && node scripts/ios-pointer-smoke.mjs

# Private XCTest symbols (XCSynthesizedEventRecord, XCPointerEventPath,
# XCTRunnerDaemonSession, and related classes reached through
# NSClassFromString) may be accessed from exactly one file,
# Tests/Bridge/EventRecordSynthesizer.swift, so an Xcode update that
# breaks the private surface has a single call site to fix. Any other
# match is an isolation violation and a review blocker.
lint-isolation:
	@violations=$$(grep -rl 'NSClassFromString("XCSynth\|NSClassFromString("XCPointer\|NSClassFromString("XCTRunner' Tests/ | grep -v 'Bridge/EventRecordSynthesizer.swift' || true); \
	if [ -n "$$violations" ]; then \
		echo "ERROR: private XCTest symbol access outside Tests/Bridge/EventRecordSynthesizer.swift:"; \
		echo "$$violations"; \
		exit 1; \
	fi; \
	echo "isolation check: OK"

setup: build

# --- Physical device targets -----------------------------------------
#
# Usage:
#   make setup-device DEVICE_UDID=00008101-... DEV_TEAM=ABCD123456
#   make serve-device DEVICE_UDID=00008101-... DEV_TEAM=ABCD123456
#
# Prerequisites (one-time, per host):
#   1. `brew install libimobiledevice`, for iproxy tunneling
#   2. Xcode, then Settings, then Accounts, then add Apple ID
#   3. Trust the device on the Mac (pair via USB)
#   4. Trust the Mac on the device (iOS prompt on first connect)
#
# Driver code is identical to simulator. The only difference is
# (a) code signing passthrough to xcodebuild, and (b) the adapter
# spawns iproxy on connect when targeting a physical device (see
# `IosXctestController.startIproxy`).

check-device:
	@missing=""; \
	if [ -z "$(DEVICE_UDID)" ]; then missing="$$missing DEVICE_UDID"; fi; \
	if [ -z "$(DEV_TEAM)" ]; then missing="$$missing DEV_TEAM"; fi; \
	if [ -n "$$missing" ]; then \
		echo "ERROR: missing required variables:$$missing"; \
		echo ""; \
		echo "Set them in device.env (recommended):"; \
		echo "  cp device.env.example device.env"; \
		echo "  \$$EDITOR device.env"; \
		echo ""; \
		echo "Or override on the command line:"; \
		echo "  make $(MAKECMDGOALS) DEVICE_UDID=<udid> DEV_TEAM=<team>"; \
		echo ""; \
		echo "Discover UDID:  idevice_id -l   OR   Xcode, then Window, then Devices"; \
		echo "Team ID:        developer.apple.com/account, then Membership"; \
		exit 1; \
	fi

build-device: check-device $(PROJECT)
	@mkdir -p $(DERIVED)
	$(LOCK_RUN) xcodebuild build-for-testing \
		-project $(PROJECT) \
		-scheme $(SCHEME) \
		-destination '$(DEVICE_DESTINATION)' \
		-derivedDataPath $(DERIVED) \
		$(SIGN_OVERRIDES)

test-device: check-device $(PROJECT)
	@mkdir -p $(DERIVED)
	$(LOCK_RUN) xcodebuild test \
		-project $(PROJECT) \
		-scheme $(SCHEME) \
		-destination '$(DEVICE_DESTINATION)' \
		-derivedDataPath $(DERIVED) \
		-only-testing:AtomyxDriverAgent/CommandHandlerUnitTests \
		-only-testing:AtomyxDriverAgent/DeviceOrientationTests \
		$(SIGN_OVERRIDES)

install-device: build-device
	@echo "Installing on device $(DEVICE_UDID)..."
	xcrun devicectl device install app \
		--device $(DEVICE_UDID) \
		$(DERIVED)/Build/Products/Debug-iphoneos/AtomyxHostApp.app
	xcrun devicectl device install app \
		--device $(DEVICE_UDID) \
		$(DERIVED)/Build/Products/Debug-iphoneos/AtomyxDriverAgent-Runner.app
	@echo ""
	@echo "Installed. If the device prompts to trust the developer certificate:"
	@echo "  Settings, then General, then VPN and Device Management, then Trust"
	@echo ""
	@echo "Next: make serve-device"

# build-for-testing output for the device destination, kept apart
# from $(BUILD_STAMP) because it carries signing overrides the
# simulator build does not use. check-device is deliberately not a
# prerequisite here (unlike serve-device below): a phony prerequisite
# on a file target defeats staleness tracking, forcing a rebuild on
# every invocation regardless of whether the stamp is current.
$(DEVICE_BUILD_STAMP): $(PROJECT) $(SWIFT_SOURCES)
	@mkdir -p $(DERIVED)
	$(LOCK_RUN) xcodebuild build-for-testing \
		-project $(PROJECT) \
		-scheme $(SCHEME) \
		-destination '$(DEVICE_DESTINATION)' \
		-derivedDataPath $(DERIVED) \
		$(SIGN_OVERRIDES)
	@mkdir -p $(DERIVED)
	@touch $(DEVICE_BUILD_STAMP)

serve-device: check-device $(DEVICE_BUILD_STAMP)
	@rm -rf $(RUN_DERIVED)/serve.xcresult
	$(SERVE_DEVICE_ENV) xcodebuild test-without-building \
		-xctestrun $(XCTESTRUN_DEVICE) \
		-destination '$(DEVICE_DESTINATION)' \
		-derivedDataPath $(RUN_DERIVED) \
		-resultBundlePath $(RUN_DERIVED)/serve.xcresult \
		-only-testing:AtomyxDriverAgent/AtomyxDriverAgent/testServeCommands

setup-device: build-device

clean:
	rm -rf $(DERIVED) $(PROJECT)
