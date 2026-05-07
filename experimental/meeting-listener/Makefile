.PHONY: run run-debug build build-debug clean test-fixture test-file

# --- Build artifact: the bundled .app -----------------------------------------
# macOS keys system-audio capture permission to a stable bundle identifier,
# so we always build the release binary into a minimal .app, ad-hoc
# codesigned with our entitlements. There is no separate "bare CLI" target —
# the bundle is the artifact.

APP := app/MeetingListener.app
APP_BIN := $(APP)/Contents/MacOS/MeetingListener
INFO_PLIST := app/Info.plist
ENTITLEMENTS := app/MeetingListener.entitlements

# Default: build the release .app and run it. First launch triggers TCC
# prompts for mic + system-audio recording; accept both. Subsequent runs
# reuse the granted permissions because the bundle id is stable.
run: build
	$(APP_BIN)

# Same, but with FluidAudio's chunk-level debug/info logs visible. Use when
# diagnosing VAD timing or encoder behavior.
run-debug: build-debug
	$(APP_BIN)

# Build the bundle without running it (CI / inspection / signing audit).
build:
	@swift build -c release
	@$(MAKE) -s _bundle BIN=.build/release/MeetingListener

build-debug:
	@swift build
	@$(MAKE) -s _bundle BIN=.build/debug/MeetingListener

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

test-file: test-fixture build
	$(APP_BIN) --file $(FIXTURE)
