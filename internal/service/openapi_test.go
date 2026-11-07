package service

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseOpenAPIJSONBuildsRoutesAndVariants(t *testing.T) {
	routes, err := ParseOpenAPI(`{
  "openapi":"3.0.3",
  "paths":{"/users/{id}":{"get":{"operationId":"getUser","responses":{
    "200":{"content":{"application/json":{"example":{"id":1,"name":"Ada"}}}},
    "404":{"content":{"application/json":{"example":{"error":"missing"}}}}
  }}}}
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Path != "/users/{id}" || routes[0].ResponseStatus != 200 {
		t.Fatalf("unexpected routes: %+v", routes)
	}
	if len(routes[0].Variants) != 1 || routes[0].Variants[0].Name != "status-404" {
		t.Fatalf("response variant missing: %+v", routes[0].Variants)
	}
}

func TestParseOpenAPIYAMLGeneratesSchemaExample(t *testing.T) {
	routes, err := ParseOpenAPI(`openapi: 3.1.0
paths:
  /health:
    get:
      responses:
        "200":
          content:
            application/json:
              schema:
                type: object
                properties:
                  ok: {type: boolean}
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].ResponseBody != "{\n  \"ok\": false\n}" {
		t.Fatalf("unexpected generated response: %+v", routes)
	}
}

// A schema whose properties all point back at itself fans out n^depth nodes;
// a 1 KB spec must not turn into a multi-megabyte (or OOM-sized) example.
func TestParseOpenAPISelfReferencingSchemaStaysSmall(t *testing.T) {
	var b strings.Builder
	b.WriteString("openapi: 3.0.0\npaths:\n  /x:\n    get:\n      responses:\n        '200':\n          content:\n            application/json:\n              schema: {$ref: '#/components/schemas/A'}\ncomponents:\n  schemas:\n    A:\n      type: object\n      properties:\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "        p%d: {$ref: '#/components/schemas/A'}\n", i)
	}
	routes, err := ParseOpenAPI(b.String())
	if err != nil {
		t.Fatal(err)
	}
	if n := len(routes[0].ResponseBody); n > 1<<20 {
		t.Fatalf("example body is %d bytes from a %d-byte spec", n, b.Len())
	}
	if !strings.Contains(routes[0].ResponseBody, `"p0"`) {
		t.Fatalf("example lost its top-level properties: %.200s", routes[0].ResponseBody)
	}
}
