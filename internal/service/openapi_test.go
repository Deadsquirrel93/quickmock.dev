package service

import "testing"

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
