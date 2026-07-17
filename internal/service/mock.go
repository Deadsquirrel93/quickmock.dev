// Package service holds the business logic between handlers and storage.
package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Deadsquirrel93/quickmock.dev/internal/model"
	"github.com/Deadsquirrel93/quickmock.dev/internal/repository"
)

// Errors returned by the service layer. Handlers map these to HTTP status
// codes + i18n message keys; nothing else should switch on them.
var (
	ErrValidation       = errors.New("validation failed")
	ErrBodyTooLarge     = errors.New("body too large")
	ErrMockLimitReached = errors.New("mock limit reached")
	ErrNotFound         = errors.New("not found")
	ErrSpamBlocked      = errors.New("content blocked by spam filter")
	ErrTokenRequired    = errors.New("admin token required")
	ErrTokenInvalid     = errors.New("admin token invalid")
)

// ValidationError carries the offending field name. Handlers translate the
// field into a localized message.
type ValidationError struct {
	Field   string
	Message string // human-readable English fallback; not shown to users
}

func (e *ValidationError) Error() string { return e.Message }

// MockService is the only object handlers should call to manipulate mocks.
type MockService struct {
	repo       *repository.MockRepo
	logs       *repository.LogRepo
	stats      *StatsCache
	maxBody    int
	maxMocks   int
	defaultTTL time.Duration
	spam       *SpamFilter
}

func NewMockService(repo *repository.MockRepo, logs *repository.LogRepo, stats *StatsCache, maxBody, maxMocks int, defaultTTL time.Duration, spam *SpamFilter) *MockService {
	return &MockService{
		repo:       repo,
		logs:       logs,
		stats:      stats,
		maxBody:    maxBody,
		maxMocks:   maxMocks,
		defaultTTL: defaultTTL,
		spam:       spam,
	}
}

// Reserved response headers we will silently drop. Two categories:
//
//  1. Transport headers — setting them confuses the HTTP stack and gains
//     nothing for the user.
//  2. Origin-affecting security headers — these only ever apply to the
//     quickmock.dev origin, never to the system the mock is mimicking, so
//     letting a mock author set them turns a mock into an attack on our
//     own users (cookie fixation, CORS abuse, weakened CSP, etc.).
var reservedHeaders = map[string]struct{}{
	// Transport
	"content-length":    {},
	"transfer-encoding": {},
	"connection":        {},
	// Cookies under quickmock.dev origin
	"set-cookie":  {},
	"set-cookie2": {},
	// Security headers we own ourselves on /m/*
	"strict-transport-security":           {},
	"public-key-pins":                     {},
	"public-key-pins-report-only":         {},
	"content-security-policy":             {},
	"content-security-policy-report-only": {},
	"x-frame-options":                     {},
	"x-content-type-options":              {},
	"referrer-policy":                     {},
	"permissions-policy":                  {},
	"clear-site-data":                     {},
	// CORS — granting cross-origin reads of a mock attacker controls
	"access-control-allow-origin":      {},
	"access-control-allow-credentials": {},
	"access-control-allow-methods":     {},
	"access-control-allow-headers":     {},
	"access-control-expose-headers":    {},
	"access-control-max-age":           {},
	"cross-origin-opener-policy":       {},
	"cross-origin-embedder-policy":     {},
	"cross-origin-resource-policy":     {},
}

// IsReservedResponseHeader reports whether name is in reservedHeaders.
// Exposed so the mock router can apply the same filter on the serve path
// as defence in depth against legacy DB rows written before the filter
// was tightened.
func IsReservedResponseHeader(name string) bool {
	_, bad := reservedHeaders[strings.ToLower(strings.TrimSpace(name))]
	return bad
}

var headerNameRegexp = regexp.MustCompile(`^[!#$%&'*+\-.^_` + "`" + `|~0-9A-Za-z]+$`)

// pathSuffixRegexp validates the user-supplied URL label. Allowed chars are
// RFC 3986 unreserved (letters, digits, `-`, `.`, `_`, `~`) split by `/`.
// We reject leading/trailing slashes and empty segments before applying it
// — those are normalized away by normalizePathSuffix.
var pathSuffixRegexp = regexp.MustCompile(`^[A-Za-z0-9._~-]+(/[A-Za-z0-9._~-]+)*$`)

// PathSuffixMaxLen is the documented hard limit. 255 fits practical REST
// paths with plenty of headroom (a real `/api/v2/orgs/.../users/...` is
// usually < 100 chars).
const PathSuffixMaxLen = 255

// MaxSequenceSteps caps the EXTRA sequence steps per mock (the main
// response is step 1 on top of these). The create-form UI mirrors this cap.
const MaxSequenceSteps = 10

// normalizePathSuffix strips leading/trailing slashes and collapses runs.
// Returns the cleaned value plus whether anything was left.
func normalizePathSuffix(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "/")
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	return s
}

// Create validates input, applies defaults, generates a slug, and inserts.
//
// creatorIP is required so we can enforce per-IP limits and rate-limit
// future requests. It is never shown in the UI.
func (s *MockService) Create(ctx context.Context, in model.MockInput, creatorIP string) (*model.Mock, error) {
	if err := s.validate(&in); err != nil {
		return nil, err
	}

	if s.spam.Blocked(&in, creatorIP) {
		return nil, ErrSpamBlocked
	}

	if creatorIP != "" {
		n, err := s.repo.CountActiveByCreatorIP(ctx, creatorIP)
		if err != nil {
			return nil, fmt.Errorf("count mocks per IP: %w", err)
		}
		if n >= s.maxMocks {
			return nil, ErrMockLimitReached
		}
	}

	slug, err := GenerateSlug(ctx, s.repo)
	if err != nil {
		return nil, err
	}

	ttl := in.TTL
	if ttl <= 0 {
		ttl = s.defaultTTL
	}
	expires := time.Now().Add(ttl)

	tokenPlain, tokenHash, err := GenerateAdminToken()
	if err != nil {
		return nil, fmt.Errorf("generate admin token: %w", err)
	}

	m := &model.Mock{
		Slug:               slug,
		Name:               strings.TrimSpace(in.Name),
		Method:             in.Method,
		ResponseBody:       in.ResponseBody,
		ResponseStatus:     in.ResponseStatus,
		ResponseHeaders:    cleanHeaders(in.ResponseHeaders),
		ResponseDelayMS:    in.ResponseDelayMS,
		ResponseDelayMaxMS: in.ResponseDelayMaxMS,
		ErrorRatePct:       in.ErrorRatePct,
		ErrorResponse:      in.ErrorResponse,
		SequenceSteps:      in.SequenceSteps,
		ContentType:        in.ContentType,
		PathSuffix:         in.PathSuffix,
		CORSEnabled:        in.CORSEnabled,
		ExpiresAt:          &expires,
		CreatorIP:          creatorIP,
		AdminTokenHash:     tokenHash,
	}
	if err := s.repo.Create(ctx, m); err != nil {
		return nil, fmt.Errorf("insert mock: %w", err)
	}
	m.AdminToken = tokenPlain
	if s.stats != nil {
		s.stats.BumpAsync(StatMocksCreated, 1)
	}
	return m, nil
}

// Get returns a mock by slug. ErrNotFound for unknown or expired.
func (s *MockService) Get(ctx context.Context, slug string) (*model.Mock, error) {
	m, err := s.repo.BySlug(ctx, slug)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrNotFound
	}
	return m, err
}

// authorize checks adminToken against m's stored hash. A mock with no hash
// is legacy (created before this feature): it stays slug-only and
// authorizes any caller, including an empty token, until it expires.
func authorize(m *model.Mock, adminToken string) error {
	if m.AdminTokenHash == "" {
		return nil
	}
	if adminToken == "" {
		return ErrTokenRequired
	}
	if !VerifyAdminToken(adminToken, m.AdminTokenHash) {
		return ErrTokenInvalid
	}
	return nil
}

// Update applies the input to an existing mock, replacing every field.
func (s *MockService) Update(ctx context.Context, slug string, in model.MockInput, creatorIP, adminToken string) (*model.Mock, error) {
	existing, err := s.Get(ctx, slug)
	if err != nil {
		return nil, err
	}
	if err := authorize(existing, adminToken); err != nil {
		return nil, err
	}
	if err := s.validate(&in); err != nil {
		return nil, err
	}
	if s.spam.Blocked(&in, creatorIP) {
		return nil, ErrSpamBlocked
	}
	existing.Name = strings.TrimSpace(in.Name)
	existing.Method = in.Method
	existing.ResponseBody = in.ResponseBody
	existing.ResponseStatus = in.ResponseStatus
	existing.ResponseHeaders = cleanHeaders(in.ResponseHeaders)
	existing.ResponseDelayMS = in.ResponseDelayMS
	existing.ResponseDelayMaxMS = in.ResponseDelayMaxMS
	existing.ErrorRatePct = in.ErrorRatePct
	existing.ErrorResponse = in.ErrorResponse
	existing.SequenceSteps = in.SequenceSteps
	existing.ContentType = in.ContentType
	existing.PathSuffix = in.PathSuffix
	existing.CORSEnabled = in.CORSEnabled
	if in.TTL > 0 {
		t := time.Now().Add(in.TTL)
		existing.ExpiresAt = &t
	}
	if err := s.repo.Update(ctx, existing); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return existing, nil
}

// Delete removes a mock and its logs.
func (s *MockService) Delete(ctx context.Context, slug, adminToken string) error {
	m, err := s.Get(ctx, slug)
	if err != nil {
		return err
	}
	if err := authorize(m, adminToken); err != nil {
		return err
	}
	if err := s.repo.DeleteBySlug(ctx, slug); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// ClearLogs purges the request_logs for a mock and resets the counter.
func (s *MockService) ClearLogs(ctx context.Context, slug, adminToken string) error {
	m, err := s.Get(ctx, slug)
	if err != nil {
		return err
	}
	if err := authorize(m, adminToken); err != nil {
		return err
	}
	return s.logs.DeleteByMockID(ctx, m.ID)
}

func (s *MockService) validate(in *model.MockInput) error {
	if in.Method == "" {
		return &ValidationError{Field: "method", Message: "method is required"}
	}
	if !model.ValidMethod(string(in.Method)) {
		return &ValidationError{Field: "method", Message: "unknown method"}
	}
	if in.ResponseStatus == 0 {
		in.ResponseStatus = 200
	}
	if in.ResponseStatus < 100 || in.ResponseStatus > 599 {
		return &ValidationError{Field: "response_status", Message: "status out of range"}
	}
	if in.ResponseDelayMS < 0 || in.ResponseDelayMS > 30000 {
		return &ValidationError{Field: "response_delay_ms", Message: "delay out of range"}
	}
	if len(in.ResponseBody) > s.maxBody {
		return ErrBodyTooLarge
	}
	for name := range in.ResponseHeaders {
		if !headerNameRegexp.MatchString(name) {
			return &ValidationError{Field: "response_headers", Message: "invalid header name: " + name}
		}
	}
	if in.ContentType == "" {
		in.ContentType = "text/plain; charset=utf-8"
	}
	if len(in.Name) > 100 {
		in.Name = in.Name[:100]
	}
	if in.PathSuffix != "" {
		in.PathSuffix = normalizePathSuffix(in.PathSuffix)
		if in.PathSuffix == "" {
			// User typed only slashes/whitespace — treat as unset.
		} else if len(in.PathSuffix) > PathSuffixMaxLen {
			return &ValidationError{Field: "path_suffix", Message: "path is too long"}
		} else if !pathSuffixRegexp.MatchString(in.PathSuffix) {
			return &ValidationError{Field: "path_suffix", Message: "path contains invalid characters"}
		}
	}
	if in.ResponseDelayMaxMS != 0 &&
		(in.ResponseDelayMaxMS < 0 || in.ResponseDelayMaxMS < in.ResponseDelayMS || in.ResponseDelayMaxMS > 30000) {
		return &ValidationError{Field: "response_delay_max_ms", Message: "delay max out of range"}
	}
	if in.ErrorRatePct < 0 || in.ErrorRatePct > 100 {
		return &ValidationError{Field: "error_rate_pct", Message: "error rate out of range"}
	}
	if in.ErrorRatePct == 0 {
		// Normalise: an alternate response without a rate is dead config.
		in.ErrorResponse = nil
	} else {
		if in.ErrorResponse == nil {
			return &ValidationError{Field: "error_response", Message: "error response required when error rate is set"}
		}
		if err := s.validateStep(in.ErrorResponse, "error_response", 500); err != nil {
			return err
		}
		// The error response inherits the mock's headers and content-type.
		in.ErrorResponse.Headers = nil
	}
	if len(in.SequenceSteps) > MaxSequenceSteps {
		return &ValidationError{Field: "response_sequence", Message: "too many sequence steps"}
	}
	for i := range in.SequenceSteps {
		st := &in.SequenceSteps[i]
		if err := s.validateStep(st, "response_sequence", 200); err != nil {
			return err
		}
		st.Headers = cleanHeaders(st.Headers)
		if len(st.Headers) == 0 {
			st.Headers = nil
		}
		for name := range st.Headers {
			if !headerNameRegexp.MatchString(name) {
				return &ValidationError{Field: "response_sequence", Message: "invalid step header name: " + name}
			}
		}
	}
	return nil
}

// validateStep applies the shared rules for one alternate response. A zero
// status gets the variant's default (500 for the error response, 200 for a
// sequence step).
func (s *MockService) validateStep(st *model.ResponseStep, field string, defaultStatus int) error {
	if st.Status == 0 {
		st.Status = defaultStatus
	}
	if st.Status < 100 || st.Status > 599 {
		return &ValidationError{Field: field, Message: "step status out of range"}
	}
	if len(st.Body) > s.maxBody {
		return ErrBodyTooLarge
	}
	return nil
}

func cleanHeaders(h map[string]string) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" {
			continue
		}
		if _, reserved := reservedHeaders[strings.ToLower(k)]; reserved {
			continue
		}
		out[k] = v
	}
	return out
}
