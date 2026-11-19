package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/leobsst/frankenphp-cli/internal/core/alias"
	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/settings"
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/platform"
	"github.com/leobsst/frankenphp-cli/internal/ui"
)

func newConfigurationCmd(a *app) *cobra.Command {
	var showSecrets bool
	cmd := &cobra.Command{
		Use:     "configuration",
		Aliases: []string{"config"},
		Short:   i18n.T("Show or change the configuration."),
		Long: i18n.T(`Show or change the configuration, stored in db.sqlite.

Some changes apply at once (optional services, CLI alias, defaults read by
the next command); others need a restart, which the command tells.

Examples:
    frankenmanager configuration                          # List every setting
    frankenmanager configuration get DEFAULT_PHP_VERSION
    frankenmanager configuration set DEFAULT_PHP_VERSION 8.4
    frankenmanager configuration set REDIS_ENABLED true   # Starts Redis at once
    frankenmanager configuration reset WEB_HTTPS_PORT     # Back to the default`),
		Args: noArgs,
		RunE: func(*cobra.Command, []string) error { return a.configList(showSecrets) },
	}
	cmd.Flags().BoolVar(&showSecrets, "show-secrets", false, i18n.T("Show the passwords and secrets in clear"))

	list := &cobra.Command{
		Use:   "list",
		Short: i18n.T("List every setting with its value and how a change applies."),
		Args:  noArgs,
		RunE:  func(*cobra.Command, []string) error { return a.configList(showSecrets) },
	}
	list.Flags().BoolVar(&showSecrets, "show-secrets", false, i18n.T("Show the passwords and secrets in clear"))

	get := &cobra.Command{
		Use:               "get NAME",
		Short:             i18n.T("Print the value of a setting."),
		Args:              rangeArgs(1, "NAME"),
		ValidArgsFunction: completeConfigArgs(false),
		RunE:              func(_ *cobra.Command, args []string) error { return a.configGet(args[0]) },
	}
	set := &cobra.Command{
		Use:   "set NAME VALUE",
		Short: i18n.T("Change a setting, applying it live when possible."),
		Long: i18n.T(`Change a setting, applying it live when possible.

An empty VALUE ("") is allowed for the settings without a default (e.g.
DNS_IP: auto-detect). Use 'reset' to go back to a default.`),
		Args:              rangeArgs(2, "NAME", "VALUE"),
		ValidArgsFunction: completeConfigArgs(true),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.configSet(cmd.Context(), args[0], &args[1])
		},
	}
	reset := &cobra.Command{
		Use:               "reset NAME",
		Short:             i18n.T("Put a setting back to its default."),
		Args:              rangeArgs(1, "NAME"),
		ValidArgsFunction: completeConfigArgs(false),
		RunE:              func(cmd *cobra.Command, args []string) error { return a.configSet(cmd.Context(), args[0], nil) },
	}
	cmd.AddCommand(list, get, set, reset)
	return cmd
}

// completeConfigArgs completes the setting names, then (withValue) the
// accepted values of the setting.
func completeConfigArgs(withValue bool) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		switch {
		case len(args) == 0:
			var names []string
			for _, d := range env.Definitions() {
				names = append(names, d.Key+"\t"+d.Describe())
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		case len(args) == 1 && withValue:
			d, ok := env.Lookup(args[0])
			switch {
			case !ok:
				return nil, cobra.ShellCompDirectiveNoFileComp
			case d.Values != nil:
				return d.Values, cobra.ShellCompDirectiveNoFileComp
			case strings.HasSuffix(d.Key, "_PATH") || strings.HasSuffix(d.Key, "_DIR"):
				return nil, cobra.ShellCompDirectiveFilterDirs
			case d.DefaultValue() != "":
				return []string{d.DefaultValue() + "\t" + i18n.T("default")}, cobra.ShellCompDirectiveNoFileComp
			}
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// lookupSetting returns the definition of name, or a usage error with the
// closest names.
func lookupSetting(name string) (env.Definition, error) {
	return checkSetting(name, nil)
}

// checkSetting returns the definition of name after checking value (nil for
// a reset); an unknown name is a usage error.
func checkSetting(name string, value *string) (env.Definition, error) {
	d, err := settings.Check(name, value)
	var unknown *settings.UnknownError
	if errors.As(err, &unknown) {
		return d, usageError{err}
	}
	return d, err
}

// configList shows every setting.
func (a *app) configList(showSecrets bool) error {
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return err
	}
	t := ui.Table{Title: i18n.Tf("Configuration (%s)", filepath.Join(dir, "db.sqlite")), Columns: []ui.Column{
		{Header: i18n.T("Setting"), Color: ui.Cyan}, {Header: i18n.T("Value"), Color: ui.Green},
		{Header: i18n.T("Source"), Faint: true}, {Header: i18n.T("Applies"), Faint: true},
	}}
	for _, entry := range settings.List(e, showSecrets) {
		source := i18n.T("default")
		if entry.Custom {
			source = i18n.T("custom")
		}
		t.Rows = append(t.Rows, []string{entry.Key, entry.Value, source, entry.Apply.Label()})
	}
	a.con.PrintTable(t)
	a.log.Info(i18n.T("Change a setting: frankenmanager configuration set <name> <value>"))
	return nil
}

// configGet prints the value of a setting, alone, for scripts.
func (a *app) configGet(name string) error {
	d, err := lookupSetting(name)
	if err != nil {
		return err
	}
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return err
	}
	fmt.Fprintln(a.out, e.Get(d.Key))
	return nil
}

// configSet sets a setting (value) or puts it back to its default (nil),
// then applies it.
func (a *app) configSet(ctx context.Context, name string, value *string) error {
	d, err := checkSetting(name, value)
	if err != nil {
		return err
	}
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return err
	}
	c, err := settings.Set(e, d, value)
	if err != nil {
		return err
	}
	if !c.Changed() {
		a.log.Info(i18n.Tf("%s = %s (unchanged)", d.Key, c.Shown))
		return nil
	}
	a.log.Success(fmt.Sprintf("%s = %s", d.Key, c.Shown))

	switch d.Apply {
	case env.ApplyLive:
		return a.applyService(ctx, dir, e, d.Service)
	case env.ApplyAlias:
		return a.syncAlias(c.Old, c.New)
	case env.ApplyNow:
		return nil
	}
	running, err := a.serverRunning(ctx, dir)
	if err != nil {
		return err
	}
	if running {
		a.log.Warning(i18n.Tf("The server is running: apply the change with '%s'.", d.Apply.Hint()))
	} else {
		a.log.Info(i18n.T("The change applies on the next start."))
	}
	return nil
}

// serverRunning reports whether the stack of dir runs.
func (a *app) serverRunning(ctx context.Context, dir string) (running bool, err error) {
	s, err := a.openStore(dir)
	if err != nil {
		return false, err
	}
	defer closeStore(s, &err)
	return a.isRunning(ctx, a.docker(dir), s)
}

// --- CLI alias ---------------------------------------------------------------

// configuredAlias returns CLI_ALIAS without writing anything (safe under
// sudo, where the data directory may be another user's): the default when
// it cannot be read.
func configuredAlias() string {
	dir, err := platform.DataDir()
	if err != nil {
		return env.DefaultAlias
	}
	if v, ok, err := store.ReadConfigValue(filepath.Join(dir, "db.sqlite"), env.AliasKey); err == nil && ok {
		return v
	}
	return env.DefaultAlias
}

// syncAlias replaces the alias old by now (either may be "", no alias). A
// failure to write the alias is a warning: the setting is saved anyway.
func (a *app) syncAlias(old, now string) error {
	inst, err := a.deps.Alias()
	if err != nil {
		return err
	}
	if old != now && old != "" {
		if removed, err := inst.Remove(old); err != nil {
			a.log.Warning(i18n.Tf("Could not remove the alias %s: %v", inst.Path(old), err))
		} else if removed {
			a.log.Info(i18n.Tf("Alias '%s' removed.", old))
		}
	}
	if now == "" {
		a.log.Info(i18n.Tf("No alias: use '%s'.", alias.Command))
		return nil
	}
	return a.installAlias(inst, now, false)
}

// installAlias installs the alias name, quiet dropping the messages of an
// alias already in place.
func (a *app) installAlias(inst *alias.Installer, name string, quiet bool) error {
	already := inst.Installed(name)
	err := inst.Install(name)
	var conflict *alias.ConflictError
	switch {
	case errors.As(err, &conflict):
		a.log.Warning(i18n.Tf("Alias '%s' not installed: %v. Choose another one with "+
			"'frankenmanager configuration set %s <name>'.", name, err, env.AliasKey))
		return nil
	case errors.Is(err, fs.ErrPermission):
		a.log.Warning(i18n.Tf("Alias '%s' not installed: %s is not writable. Run: %s",
			name, inst.BinDir, inst.ManualCommand(name)))
		return nil
	case err != nil:
		return err
	}
	if !already || !quiet {
		a.log.Success(i18n.Tf("Alias '%s' ready: '%s <command>' works like '%s <command>'.",
			name, name, alias.Command))
	}
	if other := inst.Shadowed(name); other != "" {
		a.log.Warning(i18n.Tf("'%s' runs %s, found first in PATH.", name, other))
	}
	if !already && !quiet {
		a.log.Info(i18n.Tf("Shell completion: reload it (see 'frankenmanager completion --help') to complete '%s' too.", name))
	}
	return nil
}

// ensureAlias installs the configured alias when missing (setup, update),
// quietly when it is already in place.
func (a *app) ensureAlias() { a.ensureAliasIn("") }

// ensureAliasIn is ensureAlias in binDir, next to the command when "".
func (a *app) ensureAliasIn(binDir string) {
	name := configuredAlias()
	if name == "" {
		return
	}
	inst, err := a.deps.Alias()
	if err != nil {
		return
	}
	if binDir != "" {
		inst.BinDir, inst.Target = binDir, alias.CommandFile(inst.GOOS)
	}
	if err := a.installAlias(inst, name, true); err != nil {
		a.log.Warning(i18n.Tf("Alias '%s' not installed: %v", name, err))
	}
}

// aliasNames returns the names the completion scripts register: the main
// command and the configured alias.
func aliasNames() []string {
	names := []string{alias.Command}
	if a := configuredAlias(); a != "" && !slices.Contains(names, a) {
		names = append(names, a)
	}
	return names
}
