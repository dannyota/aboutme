package migrations

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func embeddedMigrationMap(t *testing.T, additions map[string]string) fstest.MapFS {
	t.Helper()
	result := fstest.MapFS{}
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := fs.ReadFile(FS, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		result[entry.Name()] = &fstest.MapFile{Data: data}
	}
	for name, content := range additions {
		result[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return result
}
