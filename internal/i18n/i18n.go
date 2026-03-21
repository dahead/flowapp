// Package i18n provides a thin wrapper around go-i18n for FlowApp.
// Templates use the injected T func: {{T "key"}}
// Server logging always stays in English and never goes through this package.
package i18n

import (
	"encoding/json"
	"net/http"
	"strings"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

// Bundle is the application-wide message bundle.
// Load it once at startup via NewBundle.
type Bundle = goi18n.Bundle

// NewBundle creates a bundle with English as the default language and loads
// every locale file passed in localeFiles (map of language tag → JSON bytes).
func NewBundle(localeFiles map[string][]byte) (*Bundle, error) {
	b := goi18n.NewBundle(language.English)
	b.RegisterUnmarshalFunc("json", json.Unmarshal)

	for tag, data := range localeFiles {
		if _, err := b.ParseMessageFileBytes(data, tag+".json"); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// Localizer creates a go-i18n Localizer for the given Accept-Language header.
// Falls back to English if the header is absent or unrecognised.
func Localizer(b *Bundle, acceptLang string) *goi18n.Localizer {
	langs := parseAcceptLanguage(acceptLang)
	langs = append(langs, "en") // always fall back to English
	return goi18n.NewLocalizer(b, langs...)
}

// LocalizerFromRequest is a convenience wrapper around Localizer.
func LocalizerFromRequest(b *Bundle, r *http.Request) *goi18n.Localizer {
	return Localizer(b, r.Header.Get("Accept-Language"))
}

// T translates a message ID using the given localizer.
// Returns the ID itself if the key is missing (visible fallback, never panics).
func T(loc *goi18n.Localizer, id string) string {
	msg, err := loc.Localize(&goi18n.LocalizeConfig{MessageID: id})
	if err != nil || msg == "" {
		return id
	}
	return msg
}

// Tf translates a message ID and substitutes template data.
func Tf(loc *goi18n.Localizer, id string, data map[string]interface{}) string {
	msg, err := loc.Localize(&goi18n.LocalizeConfig{
		MessageID:    id,
		TemplateData: data,
	})
	if err != nil || msg == "" {
		return id
	}
	return msg
}

// parseAcceptLanguage returns a slice of language tags from the header value,
// ordered by q-factor (highest first). Ties keep the original order.
// Ignores malformed entries silently.
func parseAcceptLanguage(header string) []string {
	type langQ struct {
		tag string
		q   float32
	}
	var out []langQ
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tag := part
		q := float32(1.0)
		if idx := strings.Index(part, ";q="); idx != -1 {
			tag = strings.TrimSpace(part[:idx])
			var qv float32
			if _, err := scanFloat(part[idx+3:], &qv); err == nil {
				q = qv
			}
		}
		// normalise: "en-US" → "en-US", just use first two chars for simple matching
		if t := strings.SplitN(tag, "-", 2); len(t) > 0 {
			out = append(out, langQ{tag: t[0], q: q})
		}
	}
	// stable sort by q descending
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].q > out[j-1].q; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	tags := make([]string, len(out))
	for i, lq := range out {
		tags[i] = lq.tag
	}
	return tags
}

func scanFloat(s string, v *float32) (int, error) {
	var f float64
	n, err := parseSimpleFloat(s, &f)
	*v = float32(f)
	return n, err
}

func parseSimpleFloat(s string, v *float64) (int, error) {
	i := 0
	result := 0.0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		result = result*10 + float64(s[i]-'0')
		i++
	}
	if i < len(s) && s[i] == '.' {
		i++
		frac := 0.1
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			result += float64(s[i]-'0') * frac
			frac *= 0.1
			i++
		}
	}
	*v = result
	return i, nil
}
