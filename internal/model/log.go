package model

import "time"

// RequestLog mirrors one row of the `request_logs` table.
//
// The json tags are load-bearing: this struct is serialized straight to the
// caller by GET /api/mocks/:id/logs and the log export, and API.md has always
// documented those responses in snake_case like every other endpoint. Without
// the tags Go emits the field names verbatim, which silently contradicted the
// published contract.
type RequestLog struct {
	ID             string            `json:"id"`
	MockID         string            `json:"mock_id"`
	RequestMethod  string            `json:"request_method"`
	RequestHeaders map[string]string `json:"request_headers"`
	RequestBody    string            `json:"request_body"`
	RequestIP      string            `json:"request_ip"`
	CreatedAt      time.Time         `json:"created_at"`
}
