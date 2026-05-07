.PHONY: run run-debug build build-debug clean test-fixture test-file reset-tcc

# --- Build artifact: the bundled .app -----------------------------------------
# macOS keys system-audio capture permission to a stable bundle identifier,
# so we always build the release binary into a minimal .app, ad-hoc
# codesigned with our entitlements. There is no separate "bare CLI" target —
# the bundle is the artifact.
#
# Bundle is "Pigeon.app", binary inside is "PigeonListener" — picked so
# `pkill PigeonListener` works to stop a detached run, and the entry under
# Privacy & Security shows as "Pigeon".

APP := app/Pigeon.app
APP_BIN := $(APP)/Contents/MacOS/PigeonListener
INFO_PLIST := app/Info.plist
ENTITLEMENTS := app/Pigeon.entitlements

# Default: build the release .app and run it.
#
# The launch goes through `open`, not direct exec. This matters: when you
# exec the binary inside a bundle directly, macOS attributes TCC requests to
# the parent shell (Terminal), so the system-audio prompt either never fires
# or gets attributed to Terminal's identity instead of Pigeon's, and the
# bundle never appears under "Screen & System Audio Recording". `open -W -n
# -a` makes LaunchServices broker the launch with the bundle's Info.plist as
# the identity, which is what TCC keys permission to.
#
# stdout/stderr are wired back to the controlling tty so log output stays
# visible in the terminal that ran make. Ctrl-C kills `open`; the bundled
# app keeps running detached, so quit it via `pkill PigeonListener` if you
# need a clean state. (Better Ctrl-C handling is on the polish list.)
run: build
	@open -W -n -a "$$(pwd)/$(APP)" --stdout=$$(tty) --stderr=$$(tty)

run-debug: build-debug
	@open -W -n -a "$$(pwd)/$(APP)" --stdout=$$(tty) --stderr=$$(tty)

# Build the bundle without running it (CI / inspection / signing audit).
build:
	@swift build -c release
	@$(MAKE) -s _bundle BIN=.build/release/PigeonListener

build-debug:
	@swift build
	@$(MAKE) -s _bundle BIN=.build/debug/PigeonListener

# Internal helper — assembles the bundle from whatever BIN you point it at.
_bundle:
	@mkdir -p $(APP)/Contents/MacOS
	@cp $(INFO_PLIST) $(APP)/Contents/Info.plist
	@cp $(BIN) $(APP_BIN)
	@codesign --force --sign - --entitlements $(ENTITLEMENTS) $(APP)
	@echo "Built $(APP)"

clean:
	swift package clean
	rm -rf .build
	rm -rf test/fixtures
	rm -rf $(APP)

# --- Deterministic file-mode smoke test --------------------------------------
# Generates an audio fixture from `say` with explicit 1.5 s silences between
# sentences, then replays it through the bundled binary. Useful when you
# suspect a code regression — if this fails, the pipeline is broken; if it
# passes, any remaining issue is environmental (mic noise, route, etc.).

FIXTURE := test/fixtures/utterances.aiff
FIXTURE_TEXT := "Hello there. [[slnc 1500]] How are you doing today? [[slnc 1500]] This is a test. [[slnc 1500]] Goodbye."

test-fixture:
	@mkdir -p test/fixtures
	@echo "Generating $(FIXTURE) via macOS say..."
	@say $(FIXTURE_TEXT) -o $(FIXTURE)
	@echo "Done."

# File replay is the one place where direct exec is fine — there's no live
# capture, no TCC permission needed, and we want stdout/stderr to behave
# normally for the diff-based tests.
test-file: test-fixture build
	$(APP_BIN) --file $(FIXTURE)

# If TCC has cached a denied (or otherwise stuck) state for the bundle, this
# clears it so the next `make run` triggers a fresh prompt. Also nudges
# LaunchServices to re-read the bundle's Info.plist — without that, the
# display name (CFBundleName / CFBundleDisplayName) stays cached at whatever
# it was on first launch, even after we rebuild with a new name.
LSREGISTER := /System/Library/Frameworks/CoreServices.framework/Versions/A/Frameworks/LaunchServices.framework/Versions/A/Support/lsregister

reset-tcc:
	@tccutil reset SystemAudioRecording com.anish749.pigeon.meeting-listener || true
	@tccutil reset Microphone com.anish749.pigeon.meeting-listener || true
	@$(LSREGISTER) -f "$$(pwd)/$(APP)" 2>/dev/null || true
	@echo "Cleared TCC state and re-registered $(APP) with LaunchServices."
