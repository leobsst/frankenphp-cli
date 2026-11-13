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
	"github.com/leobsst/frankenphp-cli/internal/core/store"
	"github.com/leobsst/frankenphp-cli/internal/core/validate"
	"github.com/leobsst/frankenphp-cli/internal/platform"
	"github.com/leobsst/frankenphp-cli/internal/ui"
)

func newConfigurationCmd(a *app) *cobra.Command {
	var showSecrets bool
	cmd := &cobra.Command{
		Use:     "configuration",
		Aliases: []string{"config"},
		Short:   "Show or change the configuration.",
		Long: `Show or change the configuration, stored in db.sqlite.

Some changes apply at once (optional services, CLI alias, defaults read by
the next command); others need a restart, which the command tells.

Examples:
    frankenmanager configuration                          # List every setting
    frankenmanager configuration get DEFAULT_PHP_VERSION
    frankenmanager configuration set DEFAULT_PHP_VERSION 8.4
    frankenmanager configuration set REDIS_ENABLED true   # Starts Redis at once
    frankenmanager configuration reset WEB_HTTPS_PORT     # Back to the default`,
		Args: noArgs,
		RunE: func(*cobra.Command, []string) error { return a.configList(showSecrets) },
	}
	cmd.Flags().BoolVar(&showSecrets, "show-secrets", false, "Show the passwords and secrets in clear")

	list := &cobra.Command{
		Use:   "list",
		Short: "List every setting with its value and how a change applies.",
		Args:  noArgs,
		RunE:  func(*cobra.Command, []string) error { return a.configList(showSecrets) },
	}
	list.Flags().BoolVar(&showSecrets, "show-secrets", false, "Show the passwords and secrets in clear")

	get := &cobra.Command{
		Use:               "get NAME",
		Short:             "Print the value of a setting.",
		Args:              rangeArgs(1, "NAME"),
		ValidArgsFunction: completeConfigArgs(false),
		RunE:              func(_ *cobra.Command, args []string) error { return a.configGet(args[0]) },
	}
	set := &cobra.Command{
		Use:   "set NAME VALUE",
		Short: "Change a setting, applying it live when possible.",
		Long: `Change a setting, applying it live when possible.

An empty VALUE ("") is allowed for the settings without a default (e.g.
DNS_IP: auto-detect). Use 'reset' to go back to a default.`,
		Args:              rangeArgs(2, "NAME", "VALUE"),
		ValidArgsFunction: completeConfigArgs(true),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.configSet(cmd.Context(), args[0], &args[1])
		},
	}
	reset := &cobra.Command{
		Use:               "reset NAME",
		Short:             "Put a setting back to its default.",
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
				names = append(names, d.Key+"\t"+d.Description)
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
				return []string{d.DefaultValue() + "\tdefault"}, cobra.ShellCompDirectiveNoFileComp
			}
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// lookupSetting returns the definition of name, or a usage error with the
// closest names.
func lookupSetting(name string) (env.Definition, error) {
	if d, ok := env.Lookup(name); ok {
		return d, nil
	}
	var close []string
	upper := strings.ToUpper(name)
	for _, k := range env.Keys() {
		if strings.Contains(k, upper) || strings.Contains(upper, k) {
			close = append(close, k)
		}
	}
	msg := fmt.Sprintf("Unknown setting '%s'.", name)
	if len(close) > 0 {
		msg += " Did you mean: " + strings.Join(close, ", ") + "?"
	} else {
		msg += " Run 'frankenmanager configuration list' to see them."
	}
	return env.Definition{}, usageError{errors.New(msg)}
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
	t := ui.Table{Title: "Configuration (" + filepath.Join(dir, "db.sqlite") + ")", Columns: []ui.Column{
		{Header: "Setting", Color: ui.Cyan}, {Header: "Value", Color: ui.Green},
		{Header: "Source", Faint: true}, {Header: "Applies", Faint: true},
	}}
	for _, d := range env.Definitions() {
		value, source := e.Get(d.Key), "default"
		if e.IsSet(d.Key) {
			source = "custom"
		}
		if d.Secret && value != "" && !showSecrets {
			value = "********"
		}
		t.Rows = append(t.Rows, []string{d.Key, value, source, d.Apply.Label()})
	}
	a.con.PrintTable(t)
	a.log.Info("Change a setting: frankenmanager configuration set <name> <value>")
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
	d, err := lookupSetting(name)
	if err != nil {
		return err
	}
	if value != nil {
		if err := d.Check(*value); err != nil {
			return err
		}
		if d.Key == "DEFAULT_PROJECT_PATH" && *value != "" {
			if err := validate.Directory(*value); err != nil {
				return err
			}
		}
	}
	dir, err := a.projectDir()
	if err != nil {
		return err
	}
	e, err := a.loadEnv(dir)
	if err != nil {
		return err
	}

	old := e.Get(d.Key)
	if value != nil {
		err = e.Set(d.Key, *value)
	} else {
		err = e.Reset(d.Key)
	}
	if err != nil {
		return err
	}
	now := e.Get(d.Key)
	shown := now
	if d.Secret && shown != "" {
		shown = "********"
	}
	if now == old {
		a.log.Info(fmt.Sprintf("%s = %s (unchanged)", d.Key, shown))
		return nil
	}
	a.log.Success(fmt.Sprintf("%s = %s", d.Key, shown))

	switch d.Apply {
	case env.ApplyLive:
		return a.applyService(ctx, dir, e, d.Service)
	case env.ApplyAlias:
		return a.syncAlias(old, now)
	case env.ApplyNow:
		return nil
	}
	running, err := a.serverRunning(ctx, dir)
	if err != nil {
		return err
	}
	if running {
		a.log.Warning("The server is running: apply the change with '" + d.Apply.Hint() + "'.")
	} else {
		a.log.Info("The change applies on the next start.")
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
			a.log.Warning(fmt.Sprintf("Could not remove the alias %s: %v", inst.Path(old), err))
		} else if removed {
			a.log.Info("Alias '" + old + "' removed.")
		}
	}
	if now == "" {
		a.log.Info("No alias: use '" + alias.Command + "'.")
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
		a.log.Warning(fmt.Sprintf("Alias '%s' not installed: %v. Choose another one with "+
			"'frankenmanager configuration set %s <name>'.", name, err, env.AliasKey))
		return nil
	case errors.Is(err, fs.ErrPermission):
		a.log.Warning(fmt.Sprintf("Alias '%s' not installed: %s is not writable. Run: %s",
			name, inst.BinDir, inst.ManualCommand(name)))
		return nil
	case err != nil:
		return err
	}
	if !already || !quiet {
		a.log.Success(fmt.Sprintf("Alias '%s' ready: '%s <command>' works like '%s <command>'.",
			name, name, alias.Command))
	}
	if other := inst.Shadowed(name); other != "" {
		a.log.Warning(fmt.Sprintf("'%s' runs %s, found first in PATH.", name, other))
	}
	if !already && !quiet {
		a.log.Info("Shell completion: reload it (see 'frankenmanager completion --help') to complete '" + name + "' too.")
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
		a.log.Warning(fmt.Sprintf("Alias '%s' not installed: %v", name, err))
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
