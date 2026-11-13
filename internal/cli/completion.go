package cli

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newCompletionCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "completion SHELL",
		Short: "Generate the shell completion script (bash, zsh, fish, powershell).",
		Long: `Generate the shell completion script, for frankenmanager and its alias.

Bash (needs bash-completion):
    echo 'source <(frankenmanager completion bash)' >> ~/.bashrc

Zsh:
    echo 'source <(frankenmanager completion zsh)' >> ~/.zshrc
    # with "compinit" run before it

Fish:
    frankenmanager completion fish > ~/.config/fish/completions/frankenmanager.fish

PowerShell:
    frankenmanager completion powershell >> $PROFILE

Run it again after changing the alias (CLI_ALIAS).`,
		Args:      rangeArgs(1, "SHELL"),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			script, err := completionScript(cmd.Root(), args[0], aliasNames()[1:])
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(a.out, script)
			return err
		},
	}
}

// completionScript returns the completion script of root for shell, also
// registered for the aliases.
func completionScript(root *cobra.Command, shell string, aliases []string) (string, error) {
	var b bytes.Buffer
	name := root.Name()
	var err error
	switch shell {
	case "bash":
		err = root.GenBashCompletionV2(&b, true)
		for _, a := range aliases {
			fmt.Fprintf(&b, "complete -o default -F __start_%s %s\n", name, a)
		}
	case "zsh":
		err = root.GenZshCompletion(&b)
		script := b.String()
		// The #compdef line names every command when loaded from fpath
		if len(aliases) > 0 {
			script = strings.Replace(script, "#compdef "+name+"\n",
				"#compdef "+name+" "+strings.Join(aliases, " ")+"\n", 1)
		}
		b.Reset()
		b.WriteString(script)
		for _, a := range aliases {
			fmt.Fprintf(&b, "compdef _%s %s\n", name, a)
		}
	case "fish":
		err = root.GenFishCompletion(&b, true)
		for _, a := range aliases {
			fmt.Fprintf(&b, "complete -c %s -w %s\n", a, name)
		}
	case "powershell":
		err = root.GenPowerShellCompletionWithDesc(&b)
		for _, a := range aliases {
			fmt.Fprintf(&b, "Register-ArgumentCompleter -CommandName '%s' -ScriptBlock ${__%sCompleterBlock}\n", a, name)
		}
	default:
		return "", usageError{fmt.Errorf("Unknown shell '%s'. Use bash, zsh, fish, or powershell.", shell)} //nolint:staticcheck // user-facing message
	}
	return b.String(), err
}
