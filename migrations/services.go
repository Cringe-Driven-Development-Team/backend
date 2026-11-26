package migrations

import (
	"embed"
	"errors"
	"io/fs"
)

//go:embed identity/*.sql workspace/*.sql environments/*.sql
var serviceFiles embed.FS

// ServiceSource возвращает только миграции владельца, не исторические destructive Up монолита.
func ServiceSource(name string) (fs.FS, error) {
	switch name {
	case "identity", "workspace", "environments":
		return fs.Sub(serviceFiles, name)
	default:
		return nil, errors.New("unknown migration service")
	}
}
