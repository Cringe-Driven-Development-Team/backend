package contracttests

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestServiceSQLBoundaries закрепляет владение таблицами вне test fixtures.
func TestServiceSQLBoundaries(t *testing.T) {
	for _, scope := range []struct{ paths, forbidden []string }{
		{[]string{"notebook/adapters/postgres", "runtimesession/adapters/postgres"},
			[]string{"identity.users", "environments.runtime_", "internal/user/adapters/postgres"}},
		{[]string{"runtime/adapters/postgres"},
			[]string{"identity.users", "workspace.runtime_", "workspace.notebooks", "internal/user/adapters/postgres"}},
		{[]string{"user/adapters/postgres"}, []string{"workspace.notebooks", "environments.runtime_"}},
	} {
		for _, path := range scope.paths {
			err := filepath.WalkDir(filepath.Join("..", "..", "internal", path), func(file string, entry fs.DirEntry, err error) error {
				if err != nil || entry.IsDir() || !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
					return err
				}
				data, err := os.ReadFile(file)
				if err != nil {
					return err
				}
				for _, pattern := range scope.forbidden {
					if strings.Contains(string(data), pattern) {
						t.Errorf("%s references another service: %s", file, pattern)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
