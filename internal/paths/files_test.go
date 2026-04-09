package paths

import "testing"

func TestMetaFilePath(t *testing.T) {
	mf := MetaFile("/data/slack/acme/eng/.meta.json")
	if got := mf.Path(); got != "/data/slack/acme/eng/.meta.json" {
		t.Errorf("Path() = %q", got)
	}
}
