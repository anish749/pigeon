package identity

import (
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/read"
)

// GlobPeopleFiles returns paths to all people.jsonl identity files under dir,
// discovered via paths.PeopleFileGlob.
// Returns nil (not an error) when no identity data exists yet.
func GlobPeopleFiles(dir string) ([]paths.PeopleFile, error) {
	files, err := read.GlobFiles(dir, []string{paths.PeopleFileGlob})
	if err != nil {
		return nil, err
	}
	out := make([]paths.PeopleFile, len(files))
	for i, f := range files {
		out[i] = paths.PeopleFile(f)
	}
	return out, nil
}
