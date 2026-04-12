// Package lifecycle provides a structured supervisor for long-running
// per-account workers (Slack, WhatsApp, GWS, Linear, ...).
//
// The package has three responsibilities, split by concern:
//
//   - Listener: the work that runs for one account. Implementations only
//     need to block in Run until ctx is cancelled or an error occurs.
//   - Factory:  the recipe for building a Listener for a given Key. Called
//     fresh on every (re)start so implementations do not have to reason
//     about reusing state across crashes.
//   - Supervisor: owns the goroutine per Key, restarts crashed listeners
//     under a RestartPolicy, and reconciles the running set against a
//     desired set of Factories.
//
// Daemons do not spawn listener goroutines directly — they hand Factories
// to a Supervisor and call Reconcile. The Supervisor is the only place in
// the codebase that owns listener goroutines or chooses when to restart
// them, so recovery policy lives in exactly one file.
package lifecycle

import "context"

// Key uniquely identifies a supervised listener. Kind is the platform
// ("slack", "whatsapp", "gws", "linear"), ID is stable within that kind
// (team_id, account slug, email, workspace).
type Key struct {
	Kind string
	ID   string
}

// String returns "kind/id" for logs.
func (k Key) String() string {
	return k.Kind + "/" + k.ID
}

// Listener is a long-running worker for a single account.
//
// Run must block until ctx is cancelled or it encounters an unrecoverable
// error. The Supervisor treats any non-nil return as a crash and restarts
// the listener after a backoff. Returning nil while ctx is not yet done
// also counts as a crash — a well-behaved Run either returns ctx.Err() on
// cancellation or a descriptive error on failure.
//
// Implementations are responsible for acquiring and releasing their own
// resources inside Run (sockets, file locks, API registrations). The
// Supervisor will call Build() again for the next incarnation, so state
// must not leak between runs.
type Listener interface {
	Run(ctx context.Context) error
}

// Factory builds Listener instances on demand. It is the stable contract
// the Supervisor holds onto for a key's lifetime.
//
// Build is called on initial start and again on every restart. It must
// return a fresh Listener each time — supervisors never reuse a Listener
// across incarnations. A non-nil error from Build is treated the same as
// a crash: logged and retried under RestartPolicy.
type Factory interface {
	Key() Key
	Build(ctx context.Context) (Listener, error)
}

// ListenerFunc adapts a plain function into a Listener.
type ListenerFunc func(ctx context.Context) error

// Run satisfies Listener.
func (f ListenerFunc) Run(ctx context.Context) error { return f(ctx) }

// FactoryFunc adapts a plain function into a Factory without needing a
// dedicated struct. Useful for one-shot compositions in tests.
type FactoryFunc struct {
	K     Key
	BuildFn func(ctx context.Context) (Listener, error)
}

// Key satisfies Factory.
func (f FactoryFunc) Key() Key { return f.K }

// Build satisfies Factory.
func (f FactoryFunc) Build(ctx context.Context) (Listener, error) { return f.BuildFn(ctx) }
