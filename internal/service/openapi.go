package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

const MaxOpenAPISpecBytes = 1024 * 1024

// ParseOpenAPI converts an OpenAPI 3.x JSON or YAML document into the
// multi-route shape accepted by POST /api/mocks. It intentionally focuses on
// response examples/schemas: security and server definitions describe the
// upstream API, not the anonymous Quickmock workspace.
func ParseOpenAPI(raw string) ([]model.MockRoute, error) {
	if len(raw) == 0 || len(raw) > MaxOpenAPISpecBytes {
		return nil, errors.New("OpenAPI document is empty or too large")
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, fmt.Errorf("parse OpenAPI: %w", err)
	}
	version, _ := doc["openapi"].(string)
	if !strings.HasPrefix(version, "3.") {
		return nil, errors.New("only OpenAPI 3.x is supported")
	}
	paths, ok := stringMap(doc["paths"])
	if !ok || len(paths) == 0 {
		return nil, errors.New("OpenAPI paths are missing")
	}
	components, _ := stringMap(doc["components"])
	schemas, _ := stringMap(components["schemas"])

	pathNames := sortedKeys(paths)
	routes := make([]model.MockRoute, 0, len(pathNames))
	for _, path := range pathNames {
		pathItem, ok := stringMap(paths[path])
		if !ok {
			continue
		}
		for _, method := range []string{"get", "post", "put", "patch", "delete"} {
			op, ok := stringMap(pathItem[method])
			if !ok {
				continue
			}
			responses, ok := stringMap(op["responses"])
			if !ok || len(responses) == 0 {
				continue
			}
			statuses := sortedResponseStatuses(responses)
			baseStatus := statuses[0]
			for _, status := range statuses {
				if status >= 200 && status < 300 {
					baseStatus = status
					break
				}
			}
			baseBody, baseType := openAPIResponse(responses[strconv.Itoa(baseStatus)], schemas)
			name, _ := op["operationId"].(string)
			if name == "" {
				name, _ = op["summary"].(string)
			}
			route := model.MockRoute{
				Name: name, Method: model.Method(strings.ToUpper(method)), Path: path,
				ResponseStatus: baseStatus, ResponseBody: baseBody, ContentType: baseType,
			}
			for _, status := range statuses {
				if status == baseStatus {
					continue
				}
				body, contentType := openAPIResponse(responses[strconv.Itoa(status)], schemas)
				route.Variants = append(route.Variants, model.NamedVariant{
					Name: "status-" + strconv.Itoa(status), Status: status,
					Body: body, ContentType: contentType,
				})
			}
			routes = append(routes, route)
			if len(routes) >= MaxMockRoutes {
				return routes, nil
			}
		}
	}
	if len(routes) == 0 {
		return nil, errors.New("OpenAPI document has no supported operations")
	}
	return routes, nil
}

func openAPIResponse(value any, schemas map[string]any) (string, string) {
	response, _ := stringMap(value)
	content, _ := stringMap(response["content"])
	contentType := "application/json"
	media, ok := stringMap(content[contentType])
	if !ok {
		keys := sortedKeys(content)
		if len(keys) == 0 {
			return "", "text/plain; charset=utf-8"
		}
		contentType = keys[0]
		media, _ = stringMap(content[contentType])
	}
	var example any
	if media != nil {
		example = media["example"]
		if example == nil {
			if examples, ok := stringMap(media["examples"]); ok {
				for _, key := range sortedKeys(examples) {
					if item, ok := stringMap(examples[key]); ok {
						example = item["value"]
						break
					}
				}
			}
		}
		if example == nil {
			if schema, ok := stringMap(media["schema"]); ok {
				example = exampleFromSchema(schema, schemas, 0)
			}
		}
	}
	if text, ok := example.(string); ok && !strings.Contains(contentType, "json") {
		return text, contentType
	}
	if example == nil {
		example = map[string]any{}
	}
	b, _ := json.MarshalIndent(example, "", "  ")
	return string(b), contentType
}

func exampleFromSchema(schema map[string]any, schemas map[string]any, depth int) any {
	if depth > 8 {
		return nil
	}
	if example := schema["example"]; example != nil {
		return example
	}
	if ref, _ := schema["$ref"].(string); strings.HasPrefix(ref, "#/components/schemas/") {
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		if target, ok := stringMap(schemas[name]); ok {
			return exampleFromSchema(target, schemas, depth+1)
		}
	}
	typeName, _ := schema["type"].(string)
	switch typeName {
	case "object", "":
		out := map[string]any{}
		if properties, ok := stringMap(schema["properties"]); ok {
			for _, name := range sortedKeys(properties) {
				if child, ok := stringMap(properties[name]); ok {
					out[name] = exampleFromSchema(child, schemas, depth+1)
				}
			}
		}
		return out
	case "array":
		if items, ok := stringMap(schema["items"]); ok {
			return []any{exampleFromSchema(items, schemas, depth+1)}
		}
		return []any{}
	case "integer", "number":
		return 0
	case "boolean":
		return false
	default:
		if values, ok := schema["enum"].([]any); ok && len(values) > 0 {
			return values[0]
		}
		return "string"
	}
}

func stringMap(value any) (map[string]any, bool) {
	m, ok := value.(map[string]any)
	return m, ok
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedResponseStatuses(responses map[string]any) []int {
	var statuses []int
	for key := range responses {
		if status, err := strconv.Atoi(key); err == nil && status >= 100 && status <= 599 {
			statuses = append(statuses, status)
		}
	}
	if len(statuses) == 0 {
		return []int{200}
	}
	sort.Ints(statuses)
	return statuses
}
