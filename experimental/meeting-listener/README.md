# meeting-listener (prototype)

Captures both halves of a meeting — your microphone *and* the system audio mix
(Zoom, Meet, Slack, music) — streams each through Parakeet TDT
([FluidAudio](https://github.com/FluidInference/FluidAudio) CoreML on the
Apple Neural Engine), and prints labeled transcripts to stdout as the
conversation happens.

By default both sources run simultaneously. Each finalized utterance lands on
its own line, prefixed `[MIC] …` (you) or `[SYS] …` (everyone else).

## Build & run

For day-to-day use, run the bundled app — system-audio capture needs the
stable bundle identifier macOS keys TCC permission to:

```
cd experimental/meeting-listener
make run-app
```

`make run-app` builds a release binary, wraps it in a minimal `.app` at
`app/MeetingListener.app/` (ad-hoc codesigned with our entitlements), and
launches it. The first run triggers two macOS permission prompts — one for
the mic, one for system audio recording. Accept both. Subsequent runs reuse
the granted permission as long as the bundle identifier
(`com.anish749.pigeon.meeting-listener`) doesn't change.

For iteration on code that doesn't depend on system audio:

```
make run             # plain CLI, mic + system; system audio TCC less stable
make run-debug       # same, with FluidAudio's chunk-level debug logs
make test-file       # deterministic file replay (no mic, no system audio)
```

First run downloads the Parakeet EOU model (~120 MB) and the Silero VAD model
(~1.6 MB) from HuggingFace into `~/Library/Application Support/FluidAudio/Models/`.
Subsequent runs reuse the cache.

## Output shape

Each pipeline writes two channels to the terminal:

- **stderr** — single self-overwriting line of the partial transcript (live
  preview, sized to terminal width).
- **stdout** — `[MIC] <text>` or `[SYS] <text>` per finalized utterance,
  committed when the VAD reports `speechEnd`.

Press `Ctrl-C` to stop. In-flight utterances are flushed before exit.

## CLI flags

| Flag | Default | Effect |
|---|---|---|
| `--vad-threshold <float>` | `0.5` | Silero `defaultThreshold`. Lower → catches softer / shorter words; higher → fewer false-positives on noise. |
| `--pre-buffer-ms <int>` | `300` | Audio retained before VAD's `speechStart` so the onset isn't clipped. |
| `--trailing-silence-ms <int>` | `500` | Silence fed to Parakeet before `finish()` so the streaming encoder can resolve the last word. |
| `--file <path>` | — | Replay an audio file through one ASR session instead of running mic + system. Useful for deterministic regression tests. |

All defaults live in `Sources/MeetingListener/Config.swift`.

## Permissions

The bundled `.app` declares two TCC strings in `app/Info.plist`:

- `NSMicrophoneUsageDescription` — for the input mic.
- `NSAudioCaptureUsageDescription` — for the system-audio process tap.

Plus the entitlement `com.apple.security.device.audio-input` in
`app/MeetingListener.entitlements`.

If you ever revoke and need to re-grant, find them in:

- System Settings → Privacy & Security → Microphone
- System Settings → Privacy & Security → Audio Recording (or "System Recording")

## Requirements

- macOS 14.4+ (Core Audio Process Tap)
- Apple Silicon (Parakeet on the Neural Engine)
- Swift 5.9+

## What's not yet here

- Speaker diarization for `[SYS]` (next PR — `[SYS:spk0]`, `[SYS:spk1]`, …)
- Pigeon channel integration
