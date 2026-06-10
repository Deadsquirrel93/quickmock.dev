package handler

import (
	"testing"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

func TestCreateMockRequestToInputFlaky(t *testing.T) {
	req := createMockRequest{
		Method:             "get",
		ResponseDelayMaxMS: 2000,
		ErrorRatePct:       25,
		ErrorResponse:      &model.ResponseStep{Status: 503, Body: "boom"},
		ResponseSequence: []model.ResponseStep{
			{Status: 500, Body: "first"},
			{Status: 201, Body: "second", Headers: map[string]string{"X-Step": "2"}},
		},
	}
	in := req.toInput()
	if in.ResponseDelayMaxMS != 2000 || in.ErrorRatePct != 25 {
		t.Fatalf("scalar fields lost: %+v", in)
	}
	if in.ErrorResponse == nil || in.ErrorResponse.Status != 503 {
		t.Fatalf("error response lost: %+v", in.ErrorResponse)
	}
	if len(in.SequenceSteps) != 2 || in.SequenceSteps[1].Headers["X-Step"] != "2" {
		t.Fatalf("sequence lost: %+v", in.SequenceSteps)
	}
}
