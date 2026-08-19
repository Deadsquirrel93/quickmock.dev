package service

import (
	"testing"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

func TestValidateAdvancedResponseConfig(t *testing.T) {
	svc := &MockService{maxBody: 1024}
	in := model.MockInput{
		Method: model.MethodGET,
		Variants: []model.NamedVariant{{
			Name: " missing ", Body: `{"error":"missing"}`,
		}},
		Rules: []model.ResponseRule{{
			Name: "lookup", Variant: "missing",
			Conditions: []model.MatchCondition{{
				Source: "QUERY", Key: "id", Operator: "EQUALS", Value: "0",
			}},
		}},
	}
	if err := svc.validate(&in); err != nil {
		t.Fatalf("validate() = %v", err)
	}
	if in.Variants[0].Name != "missing" || in.Variants[0].Status != 200 {
		t.Fatalf("variant was not normalized: %+v", in.Variants[0])
	}
	if in.Rules[0].Conditions[0].Source != "query" || in.Rules[0].Conditions[0].Operator != "equals" {
		t.Fatalf("condition was not normalized: %+v", in.Rules[0].Conditions[0])
	}
}

func TestValidateAdvancedRejectsUnknownVariantAndDuplicateRoute(t *testing.T) {
	svc := &MockService{maxBody: 1024}
	cases := []model.MockInput{
		{
			Method: model.MethodGET,
			Rules: []model.ResponseRule{{Variant: "missing", Conditions: []model.MatchCondition{{
				Source: "method", Operator: "equals", Value: "GET",
			}}}},
		},
		{
			Method: model.MethodGET,
			Routes: []model.MockRoute{
				{Method: model.MethodGET, Path: "/users"},
				{Method: model.MethodGET, Path: "users/"},
			},
		},
	}
	for i := range cases {
		if err := svc.validate(&cases[i]); err == nil {
			t.Fatalf("case %d unexpectedly passed", i)
		}
	}
}

func TestValidateRouteDefaults(t *testing.T) {
	svc := &MockService{maxBody: 1024}
	in := model.MockInput{Method: model.MethodANY, Routes: []model.MockRoute{{
		Method: model.MethodPOST, Path: "users/{id}",
	}}}
	if err := svc.validate(&in); err != nil {
		t.Fatal(err)
	}
	got := in.Routes[0]
	if got.Path != "/users/{id}" || got.ResponseStatus != 200 || got.ContentType != "application/json" {
		t.Fatalf("unexpected normalized route: %+v", got)
	}
}
