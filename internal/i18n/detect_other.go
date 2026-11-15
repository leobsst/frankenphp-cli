//go:build !darwin && !windows

package i18n

// osLanguage: on Linux the locale variables are the system language.
func osLanguage() string { return "" }
