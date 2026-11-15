// Package i18n translates the text the CLI shows. The English text in the
// code is the message ID: English needs no catalog, and every other
// language has a directory of JSON catalogs, locales/<lang>/*.json, mapping
// the English text to its translation. A message without translation falls
// back to English.
//
// Translatable text goes through one of:
//
//	T("Web server stopped!")                  a message
//	Tf("Domain %s added.", name)              a printf message
//	Tn(n, "%d domain", "%d domains", n)       a message depending on a count
//	Errorf("Unknown domain '%s'", name)       an error, %w accepted
//	NewError("No server running")             an error whose text is translated when shown
//	N("Projects root ...")                    text kept for later, translated by T(variable)
//
// Their string literals are what `go run ./cmd/i18ncheck` checks against
// the catalogs: keep them literal, a whole sentence each (never pieces
// joined with +), the variable parts as printf verbs. A translation may
// reorder the arguments with indexed verbs (%[2]s).
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"
	"sync/atomic"
)

// Default is the language of the code, and the fallback.
const Default = "en"

//go:embed locales
var locales embed.FS

var (
	catalogs = mustLoad(locales)
	current  atomic.Value // string
)

func init() { current.Store(Default) }

// mustLoad reads every catalog at build time: the embedded files only
// change with the code, so a broken one is a programming error.
func mustLoad(fsys fs.FS) map[string]map[string]string {
	c, err := Load(fsys)
	if err != nil {
		panic(err)
	}
	return c
}

// Load reads the catalogs of fsys, locales/<lang>/*.json. A message
// translated twice must be translated the same way.
func Load(fsys fs.FS) (map[string]map[string]string, error) {
	files, err := fs.Glob(fsys, "locales/*/*.json")
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	out := map[string]map[string]string{}
	for _, f := range files {
		lang := path.Base(path.Dir(f))
		data, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, err
		}
		var msgs map[string]string
		if err := json.Unmarshal(data, &msgs); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		if out[lang] == nil {
			out[lang] = map[string]string{}
		}
		for id, text := range msgs {
			if prev, ok := out[lang][id]; ok && prev != text {
				return nil, fmt.Errorf("%s: %q translated differently in another %s catalog", f, id, lang)
			}
			out[lang][id] = text
		}
	}
	return out, nil
}

// Catalogs returns the loaded catalogs, by language.
func Catalogs() map[string]map[string]string { return catalogs }

// Languages returns the supported languages, English first.
func Languages() []string {
	langs := []string{Default}
	for lang := range catalogs {
		if lang != Default {
			langs = append(langs, lang)
		}
	}
	slices.Sort(langs[1:])
	return langs
}

// Supported reports whether lang is a supported language.
func Supported(lang string) bool { return slices.Contains(Languages(), lang) }

// SetLanguage selects the language of the messages; an unsupported one
// selects English.
func SetLanguage(lang string) {
	if !Supported(lang) {
		lang = Default
	}
	current.Store(lang)
}

// Language returns the selected language.
func Language() string { return current.Load().(string) }

// T returns the translation of msg, msg itself without one.
func T(msg string) string {
	if text, ok := catalogs[Language()][msg]; ok {
		return text
	}
	return msg
}

// Tf formats the translation of format.
func Tf(format string, args ...any) string { return fmt.Sprintf(T(format), args...) }

// Tn formats the translation of one when n is singular in the selected
// language, of other otherwise. English: 1 is singular; French: 0 and 1.
func Tn(n int, one, other string, args ...any) string {
	msg := other
	if singular(Language(), n) {
		msg = one
	}
	return fmt.Sprintf(T(msg), args...)
}

func singular(lang string, n int) bool {
	if lang == "fr" {
		return n == 0 || n == 1
	}
	return n == 1
}

// Errorf returns an error formatted from the translation of format; %w
// wraps as with fmt.Errorf.
func Errorf(format string, args ...any) error { return fmt.Errorf(T(format), args...) }

// NewError returns an error whose text is translated each time it is
// shown: for the errors created before the language is selected (package
// variables). errors.Is compares the error itself, not its text.
func NewError(msg string) error { return &lazyError{msg} }

type lazyError struct{ msg string }

func (e *lazyError) Error() string { return T(e.msg) }

// N marks msg as translatable and returns it unchanged: for text built
// before the language is selected, translated by T where it is shown.
func N(msg string) string { return msg }
