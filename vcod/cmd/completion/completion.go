// Package completioncmd is `vcod completion`.
package completioncmd

import (
	"vcod/internal/app"
	"vcod/internal/completion"
	"vcod/internal/registry"
	"vcod/internal/ui"
)

const help = `Usage:
  vcod completion bash     print the bash / zsh completion script
  vcod completion zsh      same script (zsh loads it via bashcompinit)
  vcod completion fish     print the fish completion script

Install it by hand with:

  vcod completion bash > ~/.vcod/vcod-completion.bash
  echo '. ~/.vcod/vcod-completion.bash' >> ~/.bashrc

vcod install does this for you. Re-run it after an update so a newly added
command completes.

The script is generated from the same command table the dispatcher routes on, so it
cannot list a command that does not exist or miss one that does.`

func init() {
	registry.Register(registry.Command{
		Name:        "completion",
		Summary:     "Print the shell completion script",
		Args:        "<shell>",
		Help:        help,
		Group:       registry.GroupEnvironment,
		Order:       50,
		NeedsConfig: false,
		Complete:    func([]string) []string { return []string{"bash", "zsh", "fish"} },
		Run:         run,
	})
}

func run(ctx *app.Context, args []string) int {
	shell := "bash"
	if len(args) > 0 {
		shell = args[0]
	}
	switch shell {
	case "bash", "zsh":
		ctx.Out.Raw(completion.Bash())
		return 0
	case "fish":
		ctx.Out.Raw(completion.Fish())
		return 0
	default:
		ui.Errorf("unknown shell %q. Use: bash | zsh | fish", shell)
		return 1
	}
}
