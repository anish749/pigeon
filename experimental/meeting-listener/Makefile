.PHONY: run build build-debug run-debug clean test-fixture test-file app run-app

# Default: build and run in release mode (silences FluidAudio debug/info logs).
# This runs the bare CLI binary — TCC permissions for system audio are not
# stable here. Use `make run-app` for that.
run:
	swift run -c release MeetingListener

build:
	swift build -c release

# Debug variants — useful when you want to see FluidAudio's chunk-level logs.
run-debug:
	swift run MeetingListener

build-debug:
	swift build

clean:
	swift package clean
	rm -rf .build
	rm -rf test/fixtures
	rm -rf app/MeetingListener.app

# --- App bundle for stable TCC permissions -----------------------------------
# macOS keys system-audio capture permission to a stable bundle identifier.
# A bare `swift run` binary doesn't have one, so the kernel re-prompts (or
# silently denies) on every rebuild. Wrap the release binary in a minimal
# .app and ad-hoc codesign it with our entitlements file; permission grants
# then persist as long as the bundle id stays the same.

APP := app/MeetingListener.app
APP_BIN := $(APP)/Contents/MacOS/MeetingListener
INFO_PLIST := app/Info.plist
ENTITLEMENTS := app/MeetingListener.entitlements

app: build
	@mkdir -p $(APP)/Contents/MacOS
	@cp $(INFO_PLIST) $(APP)/Contents/Info.plist
	@cp .build/release/MeetingListener $(APP_BIN)
	@codesign --force --sign - --entitlements $(ENTITLEMENTS) $(APP) 2>&1
	@echo "Built $(APP)"

run-app: app
	$(APP_BIN)

# --- Deterministic file-mode smoke test --------------------------------------
# Generates an audio fixture from `say` with explicit 1.5 s silences between
# sentences, then replays it through the same ASRSession used at runtime.
# Use this when you suspect EOU isn't firing on the live mic — if EOU works
# here but not on mic, the issue is mic noise / acoustic environment, not the
# pipeline.

FIXTURE := test/fixtures/utterances.aiff
FIXTURE_TEXT := "Hello there. [[slnc 1500]] How are you doing today? [[slnc 1500]] This is a test. [[slnc 1500]] Goodbye."

test-fixture:
	@mkdir -p test/fixtures
	@echo "Generating $(FIXTURE) via macOS say..."
	@say $(FIXTURE_TEXT) -o $(FIXTURE)
	@echo "Done."

test-file: test-fixture build
	swift run -c release MeetingListener --file $(FIXTURE)
