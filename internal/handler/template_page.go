package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Deadsquirrel93/quickmock.dev/internal/i18n"
)

// templateFieldRow pairs a JSON path from MockTemplate.Fields with its
// localized explanation, so the template itself never concatenates locale
// keys.
type templateFieldRow struct {
	Path    string
	Meaning string
}

// TemplateCase renders GET /templates/:slug — one template detail page.
func (u *UI) TemplateCase(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	tpl, ok := TemplateBySlug(slug)
	if !ok {
		u.renderer.Render(w, r, "404", http.StatusNotFound, nil)
		return
	}
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

	u.renderer.Render(w, r, "templates_case", http.StatusOK, map[string]any{
		"Template":        tpl,
		"CreateCurl":      createCurl(u.baseURL, tpl.CreateBody),
		"CallCurl":        callCurl(u.baseURL, tpl.CallVerb, tpl.CallHeader, tpl.CallData),
		"Fields":          fields,
		"MetaTitle":       title + " — " + u.localz.T(lang, "app.name"),
		"MetaDescription": u.localz.T(lang, tpl.KeyPrefix+".summary"),
		"JSONLD":          TemplateCaseJSONLD(u.localz, lang, u.baseURL, tpl),
		"RelatedGuide":    tpl.RelatedGuide,
	})
}
