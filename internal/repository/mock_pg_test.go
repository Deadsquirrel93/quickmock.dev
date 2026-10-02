package repository

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

func TestMockRepoRoundTripsJSONColumns(t *testing.T) {
	pool := testPool(t)
	repo := NewMockRepo(pool)
	ctx := context.Background()
	exp := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)

	full := &model.Mock{
		Slug: "pgfull" + time.Now().Format("150405.000000"), Name: "full", Method: model.MethodPOST,
		ResponseBody: `{"ok":true}`, ResponseStatus: 201, ContentType: "application/json",
		ResponseHeaders: map[string]string{"X-A": "1"},
		ErrorRatePct:    10, ErrorResponse: &model.ResponseStep{Status: 503, Body: "down"},
		SequenceSteps: []model.ResponseStep{{Status: 500, Body: "e", Headers: map[string]string{"X-S": "2"}}},
		Variants:      []model.NamedVariant{{Name: "v1", Status: 202, Body: "v"}},
		Rules:         []model.ResponseRule{{Name: "r1", Variant: "v1", Conditions: []model.MatchCondition{{Source: "query", Key: "k", Operator: "equals", Value: "x"}}}},
		Routes:        []model.MockRoute{{Method: model.MethodGET, Path: "/users/{id}", ResponseStatus: 200, ResponseBody: "u"}},
		PathSuffix:    "a/b", ExpiresAt: &exp, CreatorIP: "203.0.113.1", AdminTokenHash: "abc",
		CORSEnabled: true, LogsPublic: true, CaptureBody: true, CaptureIP: true,
	}
	if err := repo.Create(ctx, full); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.DeleteBySlug(context.Background(), full.Slug) })
	got, err := repo.BySlug(ctx, full.Slug)
	if err != nil {
		t.Fatal(err)
	}
	for name, pair := range map[string][2]any{
		"ResponseHeaders": {got.ResponseHeaders, full.ResponseHeaders},
		"ErrorResponse":   {got.ErrorResponse, full.ErrorResponse},
		"SequenceSteps":   {got.SequenceSteps, full.SequenceSteps},
		"Variants":        {got.Variants, full.Variants},
		"Rules":           {got.Rules, full.Rules},
		"Routes":          {got.Routes, full.Routes},
		"PathSuffix":      {got.PathSuffix, full.PathSuffix},
		"Name":            {got.Name, full.Name},
		"Method":          {got.Method, full.Method},
		"AdminTokenHash":  {got.AdminTokenHash, full.AdminTokenHash},
	} {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			t.Errorf("%s = %#v, want %#v", name, pair[0], pair[1])
		}
	}

	// Clearing the optional config writes SQL NULL, and reads back as nil.
	full.ErrorResponse, full.SequenceSteps, full.Variants, full.Rules, full.Routes = nil, nil, nil, nil, nil
	full.PathSuffix = ""
	if err := repo.Update(ctx, full); err != nil {
		t.Fatal(err)
	}
	assertNullConfig(t, pool, full.Slug)
	got, err = repo.BySlug(ctx, full.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if got.ErrorResponse != nil || got.SequenceSteps != nil || got.Variants != nil || got.Rules != nil || got.Routes != nil || got.PathSuffix != "" {
		t.Errorf("cleared config read back non-empty: %+v", got)
	}
}

func TestMockRepoPlainMockKeepsNullColumns(t *testing.T) {
	pool := testPool(t)
	repo := NewMockRepo(pool)
	ctx := context.Background()
	exp := time.Now().Add(time.Hour)

	plain := &model.Mock{
		Slug: "pgplain" + time.Now().Format("150405.000000"), Method: model.MethodGET,
		ResponseStatus: 200, ContentType: "text/plain", ResponseHeaders: map[string]string{},
		ExpiresAt: &exp, CreatorIP: "203.0.113.2",
	}
	if err := repo.Create(ctx, plain); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.DeleteBySlug(context.Background(), plain.Slug) })
	assertNullConfig(t, pool, plain.Slug)
	got, err := repo.BySlug(ctx, plain.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if got.ResponseHeaders == nil || len(got.ResponseHeaders) != 0 {
		t.Errorf("ResponseHeaders = %#v, want empty non-nil map", got.ResponseHeaders)
	}
	if got.Name != "" || got.AdminTokenHash != "" || got.ErrorResponse != nil || got.Routes != nil {
		t.Errorf("plain mock read back with config: %+v", got)
	}
	if _, err := repo.BySlug(ctx, "no-such-slug"); err != ErrNotFound {
		t.Errorf("BySlug(missing) err = %v, want ErrNotFound", err)
	}
}

func assertNullConfig(t *testing.T, pool *pgxpool.Pool, slug string) {
	t.Helper()
	var allNull bool
	err := pool.QueryRow(context.Background(), `
		SELECT error_response IS NULL AND response_sequence IS NULL AND response_variants IS NULL
		       AND response_rules IS NULL AND routes IS NULL AND path_suffix IS NULL
		FROM mocks WHERE slug = $1`, slug).Scan(&allNull)
	if err != nil {
		t.Fatal(err)
	}
	if !allNull {
		t.Error("optional config columns should be SQL NULL")
	}
}
