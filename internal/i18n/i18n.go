// Package i18n implements server-side localization for Mock API.
//
// Locale files live in /locales/<code>.json and are loaded once at startup
// (typically via embed.FS). Translations are flat key→string maps where keys
// use dotted namespaces (e.g. "mock.create.title"). Values are Go fmt-style
// format strings.
//
// Lookup falls back from the requested language to the configured default
// language, and finally to the literal key — so a UI page never crashes on a
// missing translation.
package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// Localizer holds loaded message catalogs and resolves translations.
//
// LoadFS runs once at startup; after that a Localizer is read-only and safe
// for concurrent use.
type Localizer struct {
	messages  map[string]map[string]string // lang → key → message
	supported []string                     // ordered list, for UI dropdown
	fallback  string                       // language used when key missing
}

// New creates an empty Localizer with the given fallback language.
//
// Call one of the Load* methods before using T.
func New(fallback string) *Localizer {
	if fallback == "" {
		fallback = "en"
	}
	return &Localizer{
		messages: make(map[string]map[string]string),
		fallback: fallback,
	}
}

// LoadFS loads every <lang>.json file from dir inside fsys. The language code
// is the filename without extension. Returns an error if no files are loaded
// or if the fallback language isn't among them.
func (l *Localizer) LoadFS(fsys fs.FS, dir string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("i18n: read dir %q: %w", dir, err)
	}

	loaded := make(map[string]map[string]string)
	var order []string

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		lang := strings.TrimSuffix(e.Name(), ".json")

		data, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("i18n: read %q: %w", e.Name(), err)
		}
		var msgs map[string]string
		if err := json.Unmarshal(data, &msgs); err != nil {
			return fmt.Errorf("i18n: parse %q: %w", e.Name(), err)
		}
		loaded[lang] = msgs
		order = append(order, lang)
	}

	if len(loaded) == 0 {
		return fmt.Errorf("i18n: no locale files found in %q", dir)
	}
	if _, ok := loaded[l.fallback]; !ok {
		return fmt.Errorf("i18n: fallback language %q missing from loaded locales", l.fallback)
	}

	l.messages = loaded
	l.supported = order

	return nil
}

// Supported returns the list of language codes loaded into the Localizer,
// in the order the files were read. Callers must not modify it.
func (l *Localizer) Supported() []string { return l.supported }

// IsSupported reports whether lang has a loaded catalog.
func (l *Localizer) IsSupported(lang string) bool {
	_, ok := l.messages[lang]
	return ok
}

// Fallback returns the fallback language code.
func (l *Localizer) Fallback() string { return l.fallback }

// T resolves key for lang and formats it with args.
//
// Lookup order:
//  1. messages[lang][key]
//  2. messages[fallback][key]
//  3. key itself (so the page renders something instead of crashing)
//
// If args are provided, the message is treated as a fmt.Sprintf format string.
func (l *Localizer) T(lang, key string, args ...any) string {
	msg, ok := l.messages[lang][key]
	if !ok {
		msg, ok = l.messages[l.fallback][key]
	}

	if !ok {
		return key
	}
	if len(args) == 0 || !strings.Contains(msg, "%") {
		// No format verbs to fill in — handing extra args to fmt.Sprintf
		// produces "%!(EXTRA …)" noise. Templates pass args defensively
		// (e.g. an error key that may or may not have placeholders), so
		// silently ignoring them here is the right call.
		return msg
	}
	return fmt.Sprintf(msg, args...)
}
