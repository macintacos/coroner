package cli

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const additionalCommands = "additional"

func newRoot(app App) *cobra.Command {
	root := &cobra.Command{
		Use:   "coroner <command> [flags]",
		Short: "Build root-cause investigation timelines",
		Long: "coroner records what happened during an investigation, in order: name a timeline,\n" +
			"add timestamped events from logs, chat and people, then view and export it.",
		Example: "  $ coroner version --json\n" +
			"  $ coroner completion zsh > \"${fpath[1]}/_coroner\"",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if showVersion, _ := cmd.Flags().GetBool("version"); showVersion {
				return writeVersion(cmd, app)
			}

			return cmd.Help()
		},
	}

	root.PersistentFlags().AddFlagSet(globalFlags())
	// Cobra's own Version field would print through OutOrStdout rather than app.Out.
	root.Flags().Bool("version", false, "Show coroner's version")
	// Run prints errors once, and usage only on request.
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return usageError(cmd, err)
	})

	help := newHelpTemplate()
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		// A help function cannot fail the command.
		_ = help.Execute(app.Out, cmd)
	})

	root.AddGroup(&cobra.Group{ID: additionalCommands, Title: "ADDITIONAL COMMANDS"})
	root.SetHelpCommandGroupID(additionalCommands)
	root.SetCompletionCommandGroupID(additionalCommands)
	root.AddCommand(newVersionCommand(app))
	addCompletionCommand(root, app)

	wrapRunErrors(root)

	return root
}

// globalFlags declares the flags every command accepts. Each call returns a fresh set.
func globalFlags() *pflag.FlagSet {
	flags := pflag.NewFlagSet("global", pflag.ContinueOnError)
	flags.String("db", "", "Database file `path` (env: CORONER_DB)")
	flags.String("config", "", "Config file `path` (env: CORONER_CONFIG)")
	flags.String("format", string(formatText), "Output `format`: {text|json|jsonl}")
	flags.Bool("json", false, "Short for --format json")
	flags.BoolP("quiet", "q", false, "Print only essential output")
	flags.Bool("no-color", false, "Disable color (env: NO_COLOR, CORONER_NO_COLOR)")
	flags.String("tz", "", "Time `zone` for this invocation (default: config default_timezone, then TZ)")
	flags.Bool("debug", false, "Print debug detail on stderr (env: CORONER_DEBUG)")

	return flags
}

// addCompletionCommand adds cobra's completion command now rather than at execution, so its
// scripts go to app.Out: it captures root's OutOrStdout when added. Out is set for that call
// only, before anything can print a deprecation notice through it.
func addCompletionCommand(root *cobra.Command, app App) {
	root.SetOut(app.Out)
	root.InitDefaultCompletionCmd()
	root.SetOut(nil)

	for _, cmd := range root.Commands() {
		if cmd.Name() == "completion" {
			cmd.Example = "  $ coroner completion zsh > \"${fpath[1]}/_coroner\"\n" +
				"  $ coroner completion fish > ~/.config/fish/completions/coroner.fish"
		}
	}
}

// wrapRunErrors makes every error a command body returns an *Error, so Run can tell an
// untyped error from cobra, which rejected the command line, from a command's own.
func wrapRunErrors(cmd *cobra.Command) {
	if run := cmd.RunE; run != nil {
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			return asInternal(run(cmd, args))
		}
	}

	for _, child := range cmd.Commands() {
		wrapRunErrors(child)
	}
}
