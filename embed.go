// Package quickmock exposes the project's embedded assets.
//
// Go's embed directive can only reference files in the same directory or a
// subdirectory of the source file, so we host the embed declarations at the
// repo root and make them importable from cmd/server.
package quickmock

import "embed"

// LocalesFS holds the contents of /locales (en.json, ru.json, …).
//
//go:embed all:locales
var LocalesFS embed.FS

// WebFS holds the contents of /web (templates + static assets).
//
//go:embed all:web
var WebFS embed.FS

// MigrationsFS holds the contents of /migrations.
//
//go:embed all:migrations
var MigrationsFS embed.FS
