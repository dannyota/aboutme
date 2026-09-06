package migrations

import (
	"testing"
	"testing/fstest"
)

func enforcementFixtureFS(t *testing.T) fstest.MapFS {
	t.Helper()
	result := fstest.MapFS{}
	for _, source := range migrationSourcesFromFSForTest(t, 14) {
		result[source.name] = &fstest.MapFile{Data: source.data}
	}
	return result
}

func enforcementFixtureWith(t *testing.T, additions map[string]string) fstest.MapFS {
	t.Helper()
	result := enforcementFixtureFS(t)
	for name, content := range additions {
		result[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return result
}
