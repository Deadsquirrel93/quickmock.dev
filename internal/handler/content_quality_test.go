package handler

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// htmlTagRE strips markup so word/character floors measure the reader-facing
// text, not the <p>/<ul>/<code>/<a> tags that carry it.
var htmlTagRE = regexp.MustCompile(`<[^>]*>`)

// cjkCharsPerWord converts a run of CJK characters into a comparable word
// count. Chinese averages roughly two characters per word; the figure only
// has to be in the right neighbourhood, since it feeds a floor rather than a
// report.
const cjkCharsPerWord = 2

// wordCount counts whitespace-separated tokens in s after HTML tags are
// stripped, then adds an estimate for any CJK text it found.
//
// Whitespace splitting alone is wrong for zh: Chinese prose has no spaces
// between words, so a fully translated 650-word guide scored ~90 and the
// floor below could only be satisfied by lowering it to a value that no
// longer guarded anything. Counting CJK characters separately keeps one
// floor meaningful for every locale.
func wordCount(s string) int {
	s = htmlTagRE.ReplaceAllString(s, " ")
	var (
		latin strings.Builder
		cjk   int
	)
	for _, r := range s {
		if isCJK(r) {
			cjk++
			continue
		}
		latin.WriteRune(r)
	}
	return len(strings.Fields(latin.String())) + cjk/cjkCharsPerWord
}

// isCJK reports whether r is a character from a script written without word
// spaces. Han covers zh; the kana and Hangul ranges are here so adding ja or
// ko later does not silently reintroduce the bug Han was added to fix.
func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

// resolves reports whether key actually translated to prose rather than
// falling back to the literal key. Localizer.T tries lang, then the
// fallback language (en), and only returns the literal key itself as a
// last resort so a page never crashes on a missing translation. A key that
// "resolves" to itself is exactly the typo-in-prod scenario these tests
// exist to catch; a ru-only-falls-back-to-en gap is caught separately by
// scripts/check_i18n.sh, which enforces key parity between locale files.
func resolves(t *testing.T, localz interface {
	T(string, string, ...any) string
}, lang, key string) string {
	t.Helper()
	got := localz.T(lang, key)
	if got == key || got == "" {
		t.Errorf("[%s] %s did not resolve (got %q)", lang, key, got)
	}
	return got
}

// TestTemplateMeetsPublicationThreshold enforces the /templates/<slug>
// publication bar established while writing the depth content (Task 5):
// every entry needs a resolvable answer paragraph, exactly 3 FAQ pairs, a
// "differences" paragraph of at least 120 characters in both locales, and
// enough highlighted response Fields to be worth a reader's time.
//
// The Fields floor depends on Kind rather than being a flat 4: a
// KindResponder template echoes part of the caller's own request, so its
// response body is legitimately short (slack-events-api's is a single
// {"challenge": ...} field) — a flat floor would force padding the field
// list with paths the shown payload doesn't contain. KindPayload/KindAPI
// templates carry an independent canned body, so 4 is the real bar there.
func TestTemplateMeetsPublicationThreshold(t *testing.T) {
	u := testUI(t)
	for _, tpl := range MockTemplates {
		tpl := tpl
		t.Run(tpl.Slug, func(t *testing.T) {
			minFields := 4
			if tpl.Kind == KindResponder {
				minFields = 1
			}
			if len(tpl.Fields) < minFields {
				t.Errorf("Fields has %d entries, want >= %d for kind %q", len(tpl.Fields), minFields, tpl.Kind)
			}
			if len(tpl.Fields) > 5 {
				t.Errorf("Fields has %d entries, want <= 5", len(tpl.Fields))
			}

			if len(tpl.FAQ) != 3 {
				t.Errorf("FAQ has %d entries, want exactly 3, got %v", len(tpl.FAQ), tpl.FAQ)
			}

			for _, lang := range u.localz.Supported() {
				for _, path := range tpl.Fields {
					resolves(t, u.localz, lang, tpl.KeyPrefix+".field."+path)
				}

				diffKey := tpl.KeyPrefix + ".differences"
				diff := resolves(t, u.localz, lang, diffKey)
				if n := len(diff); n < 120 {
					t.Errorf("[%s] %s is %d chars, want >= 120", lang, diffKey, n)
				}

				resolves(t, u.localz, lang, tpl.KeyPrefix+".answer")
			}
		})
	}
}

// TestLongFormGuidesMeetWordFloor enforces the 500-word depth floor on the
// three long-form /guide/<slug> pages (mock-rest-api, mock-webhook-receiver,
// test-retry-logic) established while writing them (Task 4). Entries with
// no Sections (the other seven guides) are short by design and exempt.
//
// The count is body copy only: the answer paragraph, each section's body
// (not its title — titles are navigation, not prose), and both halves of
// each FAQ pair.
// longFormWordFloor is the length a guide has to reach before it earns a
// Sections block. It is deliberately high: a /guide/<slug> page that ranks
// on its own is the point, and a few hundred words of prose around a curl
// snippet is the thin content the floor exists to keep out. Lower it and the
// guard stops guarding — the guides that pass today run 500-700 words.
const longFormWordFloor = 500

func TestLongFormGuidesMeetWordFloor(t *testing.T) {
	u := testUI(t)
	for _, c := range UseCases {
		if len(c.Sections) == 0 {
			continue
		}
		c := c
		t.Run(c.Slug, func(t *testing.T) {
			for _, lang := range u.localz.Supported() {
				var b strings.Builder
				b.WriteString(u.localz.T(lang, c.KeyPrefix+".answer"))
				for _, sec := range c.Sections {
					b.WriteByte(' ')
					b.WriteString(u.localz.T(lang, c.KeyPrefix+".sec."+sec.Key+".body"))
				}
				for _, suffix := range c.FAQ {
					b.WriteByte(' ')
					b.WriteString(u.localz.T(lang, c.KeyPrefix+".faq."+suffix+".q"))
					b.WriteByte(' ')
					b.WriteString(u.localz.T(lang, c.KeyPrefix+".faq."+suffix+".a"))
				}
				if n := wordCount(b.String()); n < longFormWordFloor {
					t.Errorf("[%s] long-form word count = %d, want >= %d", lang, n, longFormWordFloor)
				}
			}
		})
	}
}

// realTagRE matches the HTML element names these pages actually use. It is
// deliberately not htmlTagRE: prose here legitimately contains angle-bracket
// placeholders like /api/mocks/<slug>, which are not markup and must keep
// rendering literally.
var realTagRE = regexp.MustCompile(`(?i)</?(?:p|br|ul|ol|li|code|pre|strong|em|b|i|a|div|span|dl|dt|dd|h[1-6])\b[^>]*>`)

// TestEscapedContentSlotsCarryNoMarkup guards the split between the keys
// guide_case.html / templates_case.html render with `t` and the ones they
// render with `tHTML`. Only section bodies and FAQ answers are trusted HTML;
// a <code> tag written into a summary, answer, why or FAQ question reaches
// the reader as the literal text "<code>", which is the kind of defect that
// survives a review because the locale file looks perfectly reasonable.
func TestEscapedContentSlotsCarryNoMarkup(t *testing.T) {
	u := testUI(t)
	plain := []string{".title", ".summary", ".answer", ".why", ".differences"}

	check := func(t *testing.T, lang, key string) {
		t.Helper()
		if m := realTagRE.FindString(u.localz.T(lang, key)); m != "" {
			t.Errorf("[%s] %s is rendered escaped but contains %s", lang, key, m)
		}
	}

	for _, lang := range u.localz.Supported() {
		for _, c := range UseCases {
			for _, suffix := range plain {
				check(t, lang, c.KeyPrefix+suffix)
			}
			for _, q := range c.FAQ {
				check(t, lang, c.KeyPrefix+".faq."+q+".q")
			}
			for _, sec := range c.Sections {
				check(t, lang, c.KeyPrefix+".sec."+sec.Key+".title")
			}
		}
		for _, tpl := range MockTemplates {
			for _, suffix := range plain {
				check(t, lang, tpl.KeyPrefix+suffix)
			}
			for _, q := range tpl.FAQ {
				check(t, lang, tpl.KeyPrefix+".faq."+q+".q")
			}
		}
	}
}

// TestGuideAndTemplateContentKeysResolve is the main safety net named in the
// plan: Localizer.T falls back to returning the literal key when nothing is
// loaded for it, so a typo'd suffix (a Sections/FAQ Key that doesn't match
// what was written into the locale files) would otherwise render as raw
// text like "guide.case.x.sec.y.body" in production instead of failing a
// test.
func TestGuideAndTemplateContentKeysResolve(t *testing.T) {
	u := testUI(t)
	langs := u.localz.Supported()

	for _, c := range UseCases {
		c := c
		t.Run("guide/"+c.Slug, func(t *testing.T) {
			for _, lang := range langs {
				for _, sec := range c.Sections {
					resolves(t, u.localz, lang, c.KeyPrefix+".sec."+sec.Key+".title")
					resolves(t, u.localz, lang, c.KeyPrefix+".sec."+sec.Key+".body")
				}
				for _, suffix := range c.FAQ {
					resolves(t, u.localz, lang, c.KeyPrefix+".faq."+suffix+".q")
					resolves(t, u.localz, lang, c.KeyPrefix+".faq."+suffix+".a")
				}
				if len(c.Sections) > 0 {
					resolves(t, u.localz, lang, c.KeyPrefix+".answer")
				}
			}
		})
	}

	for _, tpl := range MockTemplates {
		tpl := tpl
		t.Run("templates/"+tpl.Slug, func(t *testing.T) {
			for _, lang := range langs {
				for _, suffix := range tpl.FAQ {
					resolves(t, u.localz, lang, tpl.KeyPrefix+".faq."+suffix+".q")
					resolves(t, u.localz, lang, tpl.KeyPrefix+".faq."+suffix+".a")
				}
				resolves(t, u.localz, lang, tpl.KeyPrefix+".answer")
			}
		})
	}
}

// TestTemplateSourcesWellFormed enforces that every /templates/<slug> entry
// cites at least one external reference, and that every citation is an
// actual absolute https URL with a non-empty title — the two things a
// malformed Source would silently break (a dead link or a bare URL shown as
// link text).
func TestTemplateSourcesWellFormed(t *testing.T) {
	for _, tpl := range MockTemplates {
		tpl := tpl
		t.Run(tpl.Slug, func(t *testing.T) {
			if len(tpl.Sources) == 0 {
				t.Fatal("Sources is empty, want at least one reference")
			}
			for _, src := range tpl.Sources {
				u, err := url.Parse(src.URL)
				if err != nil {
					t.Errorf("url.Parse(%q): %v", src.URL, err)
					continue
				}
				if u.Scheme != "https" {
					t.Errorf("%q has scheme %q, want https", src.URL, u.Scheme)
				}
				if strings.TrimSpace(src.Title) == "" {
					t.Errorf("Source %q has an empty Title", src.URL)
				}
			}
		})
	}
}
