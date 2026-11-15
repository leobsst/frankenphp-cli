package i18n

import "golang.org/x/sys/windows"

// osLanguage returns the first display language of the user ("fr-FR").
func osLanguage() string {
	langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil || len(langs) == 0 {
		return ""
	}
	return langs[0]
}
