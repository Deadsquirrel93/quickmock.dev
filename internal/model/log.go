package model

import "time"

// RequestLog mirrors one row of the `request_logs` table.
type RequestLog struct {
	ID             string
	MockID         string
	RequestMethod  string
	RequestHeaders map[string]string
	RequestBody    string
	RequestIP      string
	CreatedAt      time.Time
}
