Run `./pigeon <command>` — the wrapper script auto-builds to `pigeon.bin` (gitignored) and executes.
Do not run `go build -o pigeon` directly; that overwrites the wrapper script.
`./pigeon help` gives everything you need to know about available commands and usage.

## Error Handling

Errors are the caller's decision — always propagate them, never hide them.

1. **Propagate, don't swallow.** If a function returns an error, the caller must handle or
   propagate it. Never `_ = someFunc()`. Logging inside a loop is still swallowing — the
   callee reports errors; the caller decides what to do.

2. **Collect errors in loops.** Partial failures are still failures. When processing multiple
   items (messages, channels, contacts), collect errors and return them:
   ```go
   var errs []error
   for _, msg := range msgs {
       if err := store.Write(msg); err != nil {
           errs = append(errs, err)
       }
   }
   return errors.Join(errs...)
   ```

3. **Wrap with context.** Every error return should say what failed and why — this codebase
   already does this well with `fmt.Errorf("verb noun: %w", err)`.

4. **Never hide errors behind defaults.** Don't return `&User{}`, `0`, or `[]Item{}` on error
   — the caller can't distinguish "no data" from "fetch failed". Always return the error.
   Exceptions: explicit `OrDefault` functions, or first-run cases where a missing file is
   expected (e.g. `loadCursors` returning empty map when cursor file doesn't exist yet).

5. **Skip useless nil checks.** Ranging over a nil slice is safe in Go — don't guard it.
   Only nil-check pointers and interfaces that can actually be nil.
