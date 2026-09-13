package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	coreCommands       = "core"
	additionalCommands = "additional"
)

// NewRoot builds coroner's command tree, whose commands run against app. Execute it only
// through Run, which supplies its arguments, streams and exit codes. Commands use ctx, the
// context Run executes them with, not cmd.Context(): contextcheck accepts only a context
// that arrives as a parameter.
func NewRoot(ctx context.Context, app App) *cobra.Command {
	root := &cobra.Command{
		Use:   "coroner <command> [flags]",
		Short: "Build root-cause investigation timelines",
		Long: "coroner records what happened during an investigation, in order: name a timeline,\n" +
			"add timestamped events from logs, chat and people, then view and export it.",
		Example:     "  $ coroner version --json",
		Annotations: map[string]string{formatsAnnotation: "text,json"},
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

	topics := HelpTopics()
	help := newHelpTemplate(topics)
	// Cobra's help function writes through OutOrStdout, bypassing app.Out.
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		// A help function cannot fail the command.
		_ = help.Execute(app.Out, cmd)
	})

	root.AddGroup(
		&cobra.Group{ID: coreCommands, Title: "CORE COMMANDS"},
		&cobra.Group{ID: additionalCommands, Title: "ADDITIONAL COMMANDS"},
	)
	root.SetCompletionCommandGroupID(additionalCommands)
	root.AddCommand(
		newVersionCommand(app), newTimelineCommand(ctx, app), newStatusCommand(ctx, app), newEventCommand(ctx, app),
		newAnnotationCommand(ctx, app), newSourceCommand(ctx, app), newTagCommand(ctx, app),
	)

	// Last, in this order: cobra adds help and completion only to a root with subcommands,
	// and addTimelineCompletion and wrapRunErrors reach only the commands already added, so
	// help, like --help, ignores --format and --json.
	addTimelineCompletion(ctx, root, app)
	addCompletionCommand(root, app)
	wrapRunErrors(root)
	addHelpCommand(root, app, topics)

	return root
}

// runNoun is the RunE of a command that only groups verbs. It prints help without a verb
// and rejects an unknown one, which cobra checks only on root.
func runNoun(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}

	message := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
	// cobra defaults the distance to 2 only when it suggests for root.
	cmd.SuggestionsMinimumDistance = cmp.Or(cmd.SuggestionsMinimumDistance, 2)
	if suggestions := cmd.SuggestionsFor(args[0]); len(suggestions) > 0 {
		message += "\n\nDid you mean this?\n\t" + strings.Join(suggestions, "\n\t")
	}

	return usageError(cmd, errors.New(message))
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
	flags.String("tz", "", tzField().usage)
	flags.Bool("debug", false, "Print debug detail on stderr (env: CORONER_DEBUG)")
	flags.BoolP("help", "h", false, "Show help for a command")

	return flags
}

// addCompletionCommand adds cobra's completion command now rather than at execution, so its
// scripts go to app.Out: it captures root's OutOrStdout when added. Out is set for that call
// only, before anything can print a deprecation notice through it.
func addCompletionCommand(root *cobra.Command, app App) {
	root.SetOut(app.Out)
	root.InitDefaultCompletionCmd()
	root.SetOut(nil)

	examples := map[string]string{
		"completion": "  $ coroner completion zsh > \"${fpath[1]}/_coroner\"\n" +
			"  $ coroner completion fish > ~/.config/fish/completions/coroner.fish",
		"bash":       "  $ coroner completion bash > ~/.local/share/bash-completion/completions/coroner",
		"zsh":        "  $ coroner completion zsh > \"${fpath[1]}/_coroner\"",
		"fish":       "  $ coroner completion fish > ~/.config/fish/completions/coroner.fish",
		"powershell": "  PS> coroner completion powershell | Out-String | Invoke-Expression",
	}

	for _, cmd := range root.Commands() {
		if cmd.Name() == "completion" {
			cmd.Example = examples["completion"]
			for _, shell := range cmd.Commands() {
				shell.Example = examples[shell.Name()]
			}
		}
	}
}

// wrapRunErrors makes every error a command's hooks or body return an *Error, recovering a
// panic into an internal one, so Run can tell an untyped error from cobra, which rejected the
// command line, from a command's own. A body runs only once outputFormat accepts
// --format and --json.
func wrapRunErrors(cmd *cobra.Command) {
	hooks := []*func(*cobra.Command, []string) error{
		&cmd.PersistentPreRunE, &cmd.PreRunE, &cmd.RunE, &cmd.PostRunE, &cmd.PersistentPostRunE,
	}
	for _, hook := range hooks {
		if run := *hook; run != nil {
			*hook = typedHook(run)
		}
	}

	if run := cmd.RunE; run != nil {
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			_, err := outputFormat(cmd)
			if err != nil {
				return err
			}

			return run(cmd, args)
		}
	}

	for _, child := range cmd.Commands() {
		wrapRunErrors(child)
	}
}

// typedHook returns run with its errors and panics made *Error.
func typedHook(run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = &Error{
					Kind:    KindInternal,
					Message: fmt.Sprintf("unexpected panic: %v", r),
					Hint:    internalHint,
					Err:     fmt.Errorf("%v\n%s", r, debug.Stack()),
				}
			}
		}()

		return asInternal(run(cmd, args))
	}
}
