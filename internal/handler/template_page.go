package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Deadsquirrel93/quickmock.dev/internal/i18n"
	mockmw "github.com/Deadsquirrel93/quickmock.dev/internal/middleware"
)

// templateFieldRow pairs a JSON path from MockTemplate.Fields with its
// localized explanation, so the template itself never concatenates locale
// keys.
type templateFieldRow struct {
	Path    string
	Meaning string
}

// templateCategorySection groups a category with its templates, in
// TemplateCategories order, for the /templates index.
type templateCategorySection struct {
	Category  TemplateCategory
	Templates []MockTemplate
}

// Templates renders GET /templates — the gallery index, grouped by category.
func (u *UI) Templates(w http.ResponseWriter, r *http.Request) {
	lang := i18n.LangFromContext(r.Context())
	if lang == "" {
		lang = u.localz.Fallback()
	}

	sections := make([]templateCategorySection, 0, len(TemplateCategories))
	for _, c := range TemplateCategories {
		sections = append(sections, templateCategorySection{
			Category:  c,
			Templates: TemplatesByCategory(c),
		})
	}

	u.renderer.Render(w, r, "templates_index", http.StatusOK, map[string]any{
		"Sections":        sections,
		"MetaTitle":       u.localz.T(lang, "templates.title") + " — " + u.localz.T(lang, "app.name"),
		"MetaDescription": u.localz.T(lang, "templates.meta_description"),
		"JSONLD":          TemplateIndexJSONLD(u.localz, lang, u.baseURL),
	})
}

// TemplateCase renders GET /templates/:slug — one template detail page.
func (u *UI) TemplateCase(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	tpl, ok := TemplateBySlug(slug)
	if !ok {
		u.renderer.Render(w, r, "404", http.StatusNotFound, nil)
		return
	}
	u.renderer.Render(w, r, "templates_case", http.StatusOK, u.templateCaseData(r, tpl))
}

// TemplateCreate handles POST /templates/:slug/create — the gallery's
// one-click "Create this mock" CTA. It builds the mock straight from the
// template registry (TemplateInput, the same source of truth the case page
// itself renders) and follows the same Post/Redirect/Get flow as CreateForm
// (ui.go): redirect to the new mock's management page on success, or
// re-render this same case page with an error banner on failure.
func (u *UI) TemplateCreate(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	tpl, ok := TemplateBySlug(slug)
	if !ok {
		u.renderer.Render(w, r, "404", http.StatusNotFound, nil)
		return
	}
	in, _ := TemplateInput(slug)
	ip := mockmw.IPFromContext(r.Context())
	m, err := u.svc.Create(r.Context(), in, ip)
	if err != nil {
		data := u.templateCaseData(r, tpl)
		data["Error"] = errorKey(err)
		u.renderer.Render(w, r, "templates_case", http.StatusOK, data)
		return
	}
	http.Redirect(w, r, mockRedirectLocation(m), http.StatusSeeOther)
}

// templateCaseData builds the render data for the templates_case template,
// shared by TemplateCase and TemplateCreate's error path so a failed
// one-click create re-renders the exact same case page it was submitted
// from, instead of drifting out of sync with it.
func (u *UI) templateCaseData(r *http.Request, tpl MockTemplate) map[string]any {
	lang := i18n.LangFromContext(r.Context())
	if lang == "" {
		lang = u.localz.Fallback()
	}
	title := u.localz.T(lang, tpl.KeyPrefix+".title")

	fields := make([]templateFieldRow, 0, len(tpl.Fields))
	for _, path := range tpl.Fields {
		fields = append(fields, templateFieldRow{
			Path:    path,
			Meaning: u.localz.T(lang, tpl.KeyPrefix+".field."+path),
		})
	}

	// The payload section shows the response body the mock actually
	// returns — the Fields table's JSON paths (e.g. data.object.amount)
	// address this body, not the POST /api/mocks envelope shown below in
	// the "Create" curl example.
	payload := tpl.CreateBody
	pathSuffix := ""
	if in, ok := TemplateInput(tpl.Slug); ok {
		payload = prettyPayload(in.ResponseBody)
		pathSuffix = in.PathSuffix
	}

	return map[string]any{
		"Template":        tpl,
		"Payload":         payload,
		"CreateCurl":      createCurl(u.baseURL, tpl.CreateBody),
		"CallCurl":        callCurl(u.baseURL, tpl.CallVerb, tpl.CallHeader, tpl.CallData, pathSuffix),
		"Fields":          fields,
		"MetaTitle":       title + " — " + u.localz.T(lang, "app.name"),
		"MetaDescription": u.localz.T(lang, tpl.KeyPrefix+".summary"),
		"JSONLD":          TemplateCaseJSONLD(u.localz, lang, u.baseURL, tpl),
		"RelatedGuide":    tpl.RelatedGuide,
		"MaxBodyKB":       u.maxBody / 1024,
		"MaxMocks":        u.maxMocks,
	}
}

// payloadTokenRe matches a {{...}} templating token with no nested braces —
// the same {{faker.*}}, {{now.*}}, {{seq}}, {{request.*}} tokens
// service.RenderResponseBody substitutes on the serving path.
var payloadTokenRe = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// prettyPayload pretty-prints a mock's response body for display, tolerating
// the templating tokens that make it invalid JSON on its own (e.g.
// "created":{{now.unix}} or "email":"{{faker.email}}"). It follows the same
// strip-format-restore approach as the client-side stripTokens helper in
// web/templates/index.html: every token is swapped for a unique placeholder
// before formatting and put back afterwards, so the token's own text is
// never touched by the JSON formatter.
//
// A token already sitting inside quotes ("{{faker.email}}") keeps those
// quotes — only its interior is replaced with a bare placeholder. A token
// standing in for a whole value ({{now.unix}}) has no quotes of its own, so
// the placeholder is wrapped in quotes to stay valid JSON, and those added
// quotes are stripped back off when the original token text is restored.
//
// If the stripped body still isn't valid JSON, body is returned unchanged —
// never an error, never an empty string.
func prettyPayload(body string) string {
	locs := payloadTokenRe.FindAllStringIndex(body, -1)
	type token struct {
		placeholder, original string
		quoted                bool
	}
	tokens := make([]token, 0, len(locs))

	var stripped strings.Builder
	last := 0
	for i, loc := range locs {
		start, end := loc[0], loc[1]
		stripped.WriteString(body[last:start])
		quoted := start > 0 && body[start-1] == '"' && end < len(body) && body[end] == '"'
		placeholder := fmt.Sprintf("__QM_TOKEN_%d__", i)
		if quoted {
			stripped.WriteString(placeholder)
		} else {
			stripped.WriteString(`"` + placeholder + `"`)
		}
		tokens = append(tokens, token{placeholder: placeholder, original: body[start:end], quoted: quoted})
		last = end
	}
	stripped.WriteString(body[last:])

	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(stripped.String()), "", "  "); err != nil {
		return body
	}
	out := buf.String()
	for _, tk := range tokens {
		if tk.quoted {
			out = strings.ReplaceAll(out, tk.placeholder, tk.original)
		} else {
			out = strings.ReplaceAll(out, `"`+tk.placeholder+`"`, tk.original)
		}
	}
	return out
}
