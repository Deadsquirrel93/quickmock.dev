package handler

import (
	"io/fs"
	"testing"
	"testing/fstest"

	quickmock "github.com/Deadsquirrel93/quickmock.dev"
)

func TestAssetVersionStableAndContentSensitive(t *testing.T) {
	base := fstest.MapFS{
		"static/css/style.css": {Data: []byte("body{}")},
		"static/js/app.js":     {Data: []byte("console.log(1)")},
		// A non-static sibling must not influence the version.
		"templates/base.html": {Data: []byte("<html>")},
	}

	v1, err := assetVersion(base)
	if err != nil {
		t.Fatalf("assetVersion: %v", err)
	}
	if v1 == "" {
		t.Fatal("version is empty")
	}

	// Deterministic: the same tree hashes to the same version.
	again, err := assetVersion(base)
	if err != nil {
		t.Fatalf("assetVersion (again): %v", err)
	}
	if again != v1 {
		t.Fatalf("version not stable: %q != %q", v1, again)
	}

	// Changing a static file's content changes the version.
	changed := fstest.MapFS{
		"static/css/style.css": {Data: []byte("body{color:red}")},
		"static/js/app.js":     {Data: []byte("console.log(1)")},
		"templates/base.html":  {Data: []byte("<html>")},
	}
	v2, err := assetVersion(changed)
	if err != nil {
		t.Fatalf("assetVersion (changed): %v", err)
	}
	if v2 == v1 {
		t.Fatalf("version did not change when CSS changed: %q", v1)
	}

	// Changing only a non-static file does NOT change the version.
	templateOnly := fstest.MapFS{
		"static/css/style.css": {Data: []byte("body{}")},
		"static/js/app.js":     {Data: []byte("console.log(1)")},
		"templates/base.html":  {Data: []byte("<html lang=en>")},
	}
	v3, err := assetVersion(templateOnly)
	if err != nil {
		t.Fatalf("assetVersion (template-only): %v", err)
	}
	if v3 != v1 {
		t.Fatalf("version changed on non-static edit: %q != %q", v3, v1)
	}
}

func TestAssetVersionFromEmbeddedFS(t *testing.T) {
	webSub, err := fs.Sub(quickmock.WebFS, "web")
	if err != nil {
		t.Fatal(err)
	}
	v, err := assetVersion(webSub)
	if err != nil {
		t.Fatalf("assetVersion: %v", err)
	}
	if len(v) != 12 {
		t.Fatalf("version length = %d, want 12 (%q)", len(v), v)
	}
}
