# meeting-listener (prototype)

Captures microphone audio, streams it through Parakeet TDT (CoreML on the Apple
Neural Engine via [FluidAudio](https://github.com/FluidInference/FluidAudio)),
and prints transcripts to stdout as you speak.

This is iteration 1 of a larger experiment: validate the audio → Parakeet → text
pipeline end-to-end before adding the system-audio leg or wiring into pigeon.

## Build & run

```
cd experimental/meeting-listener
make run
```

`make run` is `swift run -c release MeetingListener`. Use the release config
rather than the default debug build — FluidAudio's logger mirrors every
`debug` and `info` line to stderr in debug builds, which clobbers the live
preview. In release builds only warnings and above print.

Other targets: `make build`, `make run-debug`, `make build-debug`, `make clean`.

First run downloads the Parakeet EOU model from HuggingFace (~120 MB) into
`~/Library/Application Support/FluidAudio/Models/`. Subsequent runs reuse the
cache.

Speak into the default input device. Output:

- **stderr** — single self-overwriting line of the partial transcript (live
  preview).
- **stdout** — `[MIC] <text>` per finalized utterance, after a ~1.3 s pause.

Press `Ctrl-C` to stop.

## Permissions

The CLI inherits microphone permission from its parent process. The first run
under a given Terminal will trigger macOS's TCC mic prompt — accept it, then
re-run.

## Requirements

- macOS 14.0+
- Apple Silicon (Parakeet runs on the Neural Engine)
- Swift 5.9+

## What's not in this prototype

- System audio capture (the "what others say" stream) — iteration 2
- Speaker diarization
- Pigeon channel integration
