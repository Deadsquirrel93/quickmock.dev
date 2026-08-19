package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Deadsquirrel93/quickmock.dev/internal/i18n"
)

func (u *UI) Docs(w http.ResponseWriter, r *http.Request) {
	lang := i18n.LangFromContext(r.Context())
	if lang == "" {
		lang = u.localz.Fallback()
	}
	u.renderer.Render(w, r, "docs", http.StatusOK, map[string]any{
		"MetaTitle":       u.localz.T(lang, "docs.meta_title"),
		"MetaDescription": u.localz.T(lang, "docs.meta_description"),
	})
}

// OpenAPISpec serves a deliberately hand-owned contract for Quickmock's
// public API. Keeping it in Go makes the docs deploy atomically with the
// handlers and avoids a second generated artifact drifting out of date.
func OpenAPISpec(baseURL string) http.HandlerFunc {
	base := strings.TrimRight(baseURL, "/")
	spec := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title": "Quickmock API", "version": LastUpdated,
			"description": "Create expiring HTTP mock endpoints and multi-route workspaces without an account.",
		},
		"servers": []map[string]string{{"url": base}},
		"paths": map[string]any{
			"/api/mocks": map[string]any{"post": map[string]any{
				"summary":     "Create a mock or multi-route workspace",
				"requestBody": jsonBodyRef("#/components/schemas/MockInput"),
				"responses":   map[string]any{"201": jsonResponseRef("#/components/schemas/Mock")},
			}},
			"/api/mocks/{slug}": map[string]any{
				"get":    map[string]any{"summary": "Get a mock configuration", "parameters": slugParameter(), "responses": map[string]any{"200": jsonResponseRef("#/components/schemas/Mock")}},
				"put":    map[string]any{"summary": "Replace a mock configuration", "security": []map[string][]string{{"adminToken": {}}}, "parameters": slugParameter(), "requestBody": jsonBodyRef("#/components/schemas/MockInput"), "responses": map[string]any{"200": jsonResponseRef("#/components/schemas/Mock")}},
				"delete": map[string]any{"summary": "Delete a mock", "security": []map[string][]string{{"adminToken": {}}}, "parameters": slugParameter(), "responses": map[string]any{"204": map[string]any{"description": "Deleted"}}},
			},
			"/api/mocks/{slug}/logs": map[string]any{
				"get":    map[string]any{"summary": "List captured requests", "security": []map[string][]string{{"adminToken": {}}}, "parameters": slugParameter(), "responses": map[string]any{"200": map[string]any{"description": "Captured requests"}}},
				"delete": map[string]any{"summary": "Clear captured requests", "security": []map[string][]string{{"adminToken": {}}}, "parameters": slugParameter(), "responses": map[string]any{"204": map[string]any{"description": "Cleared"}}},
			},
			"/api/parse-openapi": map[string]any{"post": map[string]any{
				"summary":     "Convert an OpenAPI 3.x JSON/YAML document to Quickmock routes",
				"requestBody": map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object", "required": []string{"spec"}, "properties": map[string]any{"spec": map[string]any{"type": "string"}}}}}},
				"responses":   map[string]any{"200": map[string]any{"description": "Routes generated"}},
			}},
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{"adminToken": map[string]any{"type": "http", "scheme": "bearer"}},
			"schemas": map[string]any{
				"MockInput": mockInputSchema(),
				"Mock":      map[string]any{"allOf": []any{map[string]any{"$ref": "#/components/schemas/MockInput"}, map[string]any{"type": "object", "properties": map[string]any{"slug": map[string]any{"type": "string"}, "url": map[string]any{"type": "string", "format": "uri"}, "admin_token": map[string]any{"type": "string", "description": "Returned once on create"}}}}},
			},
		},
	}
	body, _ := json.MarshalIndent(spec, "", "  ")
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(body)
	}
}

func slugParameter() []map[string]any {
	return []map[string]any{{"name": "slug", "in": "path", "required": true, "schema": map[string]any{"type": "string"}}}
}
func jsonBodyRef(ref string) map[string]any {
	return map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": ref}}}}
}
func jsonResponseRef(ref string) map[string]any {
	return map[string]any{"description": "OK", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": ref}}}}
}

func mockInputSchema() map[string]any {
	return map[string]any{
		"type": "object", "required": []string{"method"},
		"properties": map[string]any{
			"name":              map[string]any{"type": "string", "maxLength": 100},
			"method":            map[string]any{"type": "string", "enum": []string{"GET", "POST", "PUT", "PATCH", "DELETE", "ANY"}},
			"response_status":   map[string]any{"type": "integer", "minimum": 100, "maximum": 599, "default": 200},
			"response_body":     map[string]any{"type": "string", "maxLength": 524288},
			"response_headers":  map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
			"content_type":      map[string]any{"type": "string"},
			"response_delay_ms": map[string]any{"type": "integer", "minimum": 0, "maximum": 30000},
			"response_variants": map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"response_rules":    map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"routes":            map[string]any{"type": "array", "maxItems": 50, "items": map[string]any{"type": "object"}},
			"logs_public":       map[string]any{"type": "boolean", "default": false},
			"capture_body":      map[string]any{"type": "boolean", "default": true},
			"capture_ip":        map[string]any{"type": "boolean", "default": true},
			"ttl_seconds":       map[string]any{"type": "integer", "maximum": 2592000},
		},
	}
}
