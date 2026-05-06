.PHONY: run build build-debug run-debug clean test-fixture test-file

# Default: build and run in release mode (silences FluidAudio debug/info logs).
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
