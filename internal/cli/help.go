package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/platform"
)

// selectLanguage selects the language of the messages before the commands
// are built (their help is translated then): FRANKENMANAGER_LANG, the
// CLI_LANGUAGE setting, then the system language. The setting is read
// without creating anything, so it works before the setup and under sudo.
func selectLanguage(system func() string) {
	setting := ""
	if dir, err := platform.DataDir(); err == nil {
		setting, _, _ = store.ReadConfigValue(filepath.Join(dir, "db.sqlite"), env.LanguageKey)
	}
	i18n.SetLanguage(i18n.Resolve(setting, system))
}

// translateHelp translates what cobra adds itself: the help command and
// the --help flag of every command. The usage is rendered by usage.
func translateHelp(root *cobra.Command) {
	root.SetUsageFunc(usage)
	root.InitDefaultHelpCmd()
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Name() == "help" && c.Parent() == root {
			c.Short = i18n.T("Help about any command")
			c.Long = i18n.Tf("Help provides help for any command in the application.\n"+
				"Simply type %s help [path to command] for full details.", root.DisplayName())
		}
		c.InitDefaultHelpFlag()
		if f := c.Flags().Lookup("help"); f != nil {
			f.Usage = i18n.Tf("help for %s", c.DisplayName())
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
}

// usage is cobra's usage, its headings translated.
func usage(c *cobra.Command) error {
	w := c.OutOrStderr()
	var b strings.Builder
	b.WriteString(i18n.T("Usage:"))
	if c.Runnable() {
		fmt.Fprintf(&b, "\n  %s", c.UseLine())
	}
	if c.HasAvailableSubCommands() {
		fmt.Fprintf(&b, "\n  %s [command]", c.CommandPath())
	}
	if len(c.Aliases) > 0 {
		fmt.Fprintf(&b, "\n\n%s\n  %s", i18n.T("Aliases:"), c.NameAndAliases())
	}
	if c.HasExample() {
		fmt.Fprintf(&b, "\n\n%s\n%s", i18n.T("Examples:"), c.Example)
	}
	if c.HasAvailableSubCommands() {
		b.WriteString("\n\n" + i18n.T("Available Commands:"))
		for _, sub := range c.Commands() {
			if sub.IsAvailableCommand() || sub.Name() == "help" {
				fmt.Fprintf(&b, "\n  %-*s %s", sub.NamePadding(), sub.Name(), sub.Short)
			}
		}
	}
	if c.HasAvailableLocalFlags() {
		fmt.Fprintf(&b, "\n\n%s\n%s", i18n.T("Flags:"), strings.TrimRight(c.LocalFlags().FlagUsages(), " \t\n"))
	}
	if c.HasAvailableInheritedFlags() {
		fmt.Fprintf(&b, "\n\n%s\n%s", i18n.T("Global Flags:"), strings.TrimRight(c.InheritedFlags().FlagUsages(), " \t\n"))
	}
	if c.HasAvailableSubCommands() {
		b.WriteString("\n\n" + i18n.Tf("Use \"%s [command] --help\" for more information about a command.", c.CommandPath()))
	}
	fmt.Fprintln(w, b.String())
	return nil
}
