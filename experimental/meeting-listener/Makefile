.PHONY: run build build-debug run-debug clean

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
