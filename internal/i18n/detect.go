package i18n

import (
	"os"
	"strings"
)

// EnvVar forces the language over the setting, e.g. for scripts that
// parse the output: FRANKENMANAGER_LANG=en.
const EnvVar = "FRANKENMANAGER_LANG"

// Auto is the setting that follows the system language.
const Auto = "auto"

// Resolve returns the language to use: FRANKENMANAGER_LANG when it names a
// supported language, then setting (the CLI_LANGUAGE configuration) unless
// it is empty or "auto", then the system language, English when it is not
// supported.
func Resolve(setting string, system func() string) string {
	if lang := Normalize(os.Getenv(EnvVar)); Supported(lang) {
		return lang
	}
	if lang := Normalize(setting); lang != Auto && Supported(lang) {
		return lang
	}
	if lang := Normalize(system()); Supported(lang) {
		return lang
	}
	return Default
}

// Normalize reduces a locale name to its language: "fr_FR.UTF-8",
// "fr-FR" and "FR" give "fr"; "C" and "POSIX" give English.
func Normalize(locale string) string {
	l := strings.ToLower(strings.TrimSpace(locale))
	if i := strings.IndexAny(l, "_-.@"); i >= 0 {
		l = l[:i]
	}
	if l == "c" || l == "posix" {
		return Default
	}
	return l
}

// System returns the language of the user: the first of LC_ALL,
// LC_MESSAGES and LANG that is set, as on Unix, otherwise the language of
// the operating system (macOS preferences, Windows display language).
func System() string {
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if l := os.Getenv(v); l != "" {
			return l
		}
	}
	return osLanguage()
}
