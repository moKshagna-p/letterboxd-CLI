package appmeta

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	CanonicalName = "letterboxd-tui"
	LegacyName    = "film-heatmap"
)

func CommandName() string {
	name := strings.TrimSpace(filepath.Base(os.Args[0]))
	if name == "" {
		return CanonicalName
	}
	return name
}

func LookupEnv(primary, legacy string) string {
	if v := strings.TrimSpace(os.Getenv(primary)); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv(legacy))
}

func DefaultDBPath() string {
	return CanonicalName + ".db"
}
