# Atomyx iOS driver — Makefile wrapper around xcodegen + xcodebuild.
#
# Simulator usage (zero config):
#   make setup                        # generate project + build
#   make test                         # unit tests
#   make serve                        # run driver (blocks)
#   make smoke                        # host-side integration test
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
# The `-` prefix makes the include optional — simulator flow works
# without any device config.
-include device.env

PROJECT   := AtomyxDriver.xcodeproj
SCHEME    := AtomyxDriver
DERIVED   := ./build
REPO_ROOT := $(abspath ../..)

# ── Simulator defaults (auto-detected) ─────────────────────────────
# Auto-detect the first booted simulator's UDID. The 36-char anchor
# in the grep pattern ensures we only match the UUID, not `(Booted)`.
UDID ?= $(shell xcrun simctl list devices booted 2>/dev/null \
                | grep -Eo '\(([A-F0-9-]{36})\)' | head -1 | tr -d '()')
DESTINATION ?= platform=iOS Simulator,id=$(UDID)

# ── Physical device config (loaded from device.env) ─────────────────
# These defaults make `make help` readable when device.env is absent —
# `check-device` target will still fail with an actionable message if
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

# Build-setting overrides passed to xcodebuild for device targets.
# Uses xcodebuild's space-separated KEY=VALUE syntax which overrides
# whatever the generated .xcodeproj declares.
#
# NOTE: PRODUCT_BUNDLE_IDENTIFIER is deliberately NOT overridden here
# because xcodebuild applies command-line setting overrides to ALL
# targets in the invocation — host app AND test bundle would get the
# same bundle id, conflicting with the test target which needs a
# distinct suffix. Instead, BUNDLE_ID is baked into the generated
# .xcodeproj via XcodeGen's ${BUNDLE_ID} env var substitution in
# project.yml. Re-run `make generate` after changing BUNDLE_ID.
SIGN_OVERRIDES := \
	CODE_SIGN_STYLE=Automatic \
	CODE_SIGNING_REQUIRED=YES \
	CODE_SIGNING_ALLOWED=YES \
	DEVELOPMENT_TEAM=$(DEV_TEAM)

.PHONY: help generate build test serve smoke setup clean check-udid \
        build-device test-device serve-device setup-device check-device

.DEFAULT_GOAL := help

help:
	@echo "Atomyx iOS driver targets:"
	@echo ""
	@echo "Simulator (zero config — auto-detects booted sim):"
	@echo "  make setup       xcodegen + xcodebuild build-for-testing"
	@echo "  make generate    xcodegen only"
	@echo "  make build       xcodebuild build-for-testing"
	@echo "  make test        run unit tests (serve test auto-skipped)"
	@echo "  make serve       run in server mode (blocks terminal)"
	@echo "  make smoke       run host-side smoke test (from repo root)"
	@echo "  make clean       remove build/ and AtomyxDriver.xcodeproj"
	@echo ""
	@echo "Physical device (needs device.env or CLI overrides):"
	@echo "  cp device.env.example device.env   # one-time: copy + edit"
	@echo "  make setup-device                    # reads device.env"
	@echo "  make build-device"
	@echo "  make test-device"
	@echo "  make serve-device"
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
	@echo ""
	@echo "Prerequisites for device targets:"
	@echo "  brew install libimobiledevice  # iproxy tunneling"
	@echo "  Xcode → Settings → Accounts → add Apple ID"
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

generate:
	# Export BUNDLE_ID into the shell env for XcodeGen ${BUNDLE_ID}
	# substitution in project.yml. Default is baked in as a make
	# variable above — always non-empty by the time this runs.
	BUNDLE_ID=$(BUNDLE_ID) xcodegen generate

build: generate check-udid
	xcodebuild build-for-testing \
		-project $(PROJECT) \
		-scheme $(SCHEME) \
		-destination '$(DESTINATION)' \
		-derivedDataPath $(DERIVED)

test: check-udid
	xcodebuild test \
		-project $(PROJECT) \
		-scheme $(SCHEME) \
		-destination '$(DESTINATION)' \
		-derivedDataPath $(DERIVED) \
		-only-testing:AtomyxDriverUITests/CommandHandlerUnitTests

serve: check-udid
	xcodebuild test-without-building \
		-project $(PROJECT) \
		-scheme $(SCHEME) \
		-destination '$(DESTINATION)' \
		-derivedDataPath $(DERIVED) \
		-only-testing:AtomyxDriverUITests/AtomyxDriverUITests/testServeCommands

smoke:
	cd $(REPO_ROOT) && npm run build && node scripts/ios-driver-smoke.mjs

setup: generate build

# ── Physical device targets ──────────────────────────────────────────
#
# Usage:
#   make setup-device DEVICE_UDID=00008101-... DEV_TEAM=ABCD123456
#   make serve-device DEVICE_UDID=00008101-... DEV_TEAM=ABCD123456
#
# Prerequisites (one-time, per host):
#   1. `brew install libimobiledevice` — for iproxy tunneling
#   2. Xcode → Settings → Accounts → add Apple ID
#   3. Trust the device on the Mac (pair via USB)
#   4. Trust the Mac on the device (iOS prompt on first connect)
#
# Driver code is identical to simulator — the only difference is
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
		echo "Discover UDID:  idevice_id -l   OR   Xcode → Window → Devices"; \
		echo "Team ID:        developer.apple.com/account → Membership"; \
		exit 1; \
	fi

build-device: generate check-device
	xcodebuild build-for-testing \
		-project $(PROJECT) \
		-scheme $(SCHEME) \
		-destination '$(DEVICE_DESTINATION)' \
		-derivedDataPath $(DERIVED) \
		$(SIGN_OVERRIDES)

test-device: check-device
	xcodebuild test \
		-project $(PROJECT) \
		-scheme $(SCHEME) \
		-destination '$(DEVICE_DESTINATION)' \
		-derivedDataPath $(DERIVED) \
		-only-testing:AtomyxDriverUITests/CommandHandlerUnitTests \
		$(SIGN_OVERRIDES)

serve-device: check-device
	xcodebuild test-without-building \
		-project $(PROJECT) \
		-scheme $(SCHEME) \
		-destination '$(DEVICE_DESTINATION)' \
		-derivedDataPath $(DERIVED) \
		-only-testing:AtomyxDriverUITests/AtomyxDriverUITests/testServeCommands \
		$(SIGN_OVERRIDES)

setup-device: generate build-device

clean:
	rm -rf $(DERIVED) $(PROJECT)
