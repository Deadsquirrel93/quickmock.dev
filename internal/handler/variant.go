package handler

import (
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
)

// servedResponse is the resolved response for one hit of a mock after the
// flaky logic (error rate, sequence) picked a variant.
type servedResponse struct {
	Variant string // X-Mockapi-Variant value: "default", "error", "seq-<i>/<n>"
	Status  int
	Body    string
	Headers map[string]string // step headers laid over the mock's own; nil otherwise
}

// pickVariant decides which response a hit gets. roll is rand[0,100).
// nextPos yields the 0-based shared sequence position and is only called
// when the sequence actually serves — an error hit must not consume a
// position, and plain mocks must not touch Redis at all.
// Precedence: error roll > sequence > default.
func pickVariant(m *model.Mock, roll int, nextPos func() uint64) servedResponse {
	if m.ErrorRatePct > 0 && m.ErrorResponse != nil && roll < m.ErrorRatePct {
		return servedResponse{Variant: "error", Status: m.ErrorResponse.Status, Body: m.ErrorResponse.Body}
	}
	if n := len(m.SequenceSteps); n > 0 {
		cycle := n + 1 // the main response is step 1
		idx := int(nextPos() % uint64(cycle))
		if idx == 0 {
			return servedResponse{
				Variant: fmt.Sprintf("seq-1/%d", cycle),
				Status:  m.ResponseStatus,
				Body:    m.ResponseBody,
			}
		}
		st := m.SequenceSteps[idx-1]
		return servedResponse{
			Variant: fmt.Sprintf("seq-%d/%d", idx+1, cycle),
			Status:  st.Status,
			Body:    st.Body,
			Headers: st.Headers,
		}
	}
	return servedResponse{Variant: "default", Status: m.ResponseStatus, Body: m.ResponseBody}
}

// effectiveDelay is the sleep for this hit: the fixed delay when maxMS is
// unset, a uniform random duration in [minMS, maxMS] otherwise.
func effectiveDelay(minMS, maxMS int) time.Duration {
	if maxMS > minMS {
		minMS += rand.IntN(maxMS - minMS + 1)
	}
	return time.Duration(minMS) * time.Millisecond
}
