package identity

// Observer accepts identity signals from listeners and pollers. Each
// implementation is scoped to a single source (platform + account) and
// writes observations to that source's own people.jsonl file.
type Observer interface {
	Observe(sig Signal) error
	ObserveBatch(signals []Signal) error
}

// Resolver provides cross-source identity lookups. Implementations load
// multiple per-source identity files and merge them in memory using stable
// identifiers (email, Slack ID, phone).
type Resolver interface {
	LookupBySlackID(workspace, userID string) (*Person, error)
	SearchCandidates(query string) ([]Person, error)
	People() ([]Person, error)
}
