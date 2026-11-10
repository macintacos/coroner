package cli

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/annotation"
	"github.com/macintacos/coroner/internal/cli/attachment"
	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/cli/event"
	"github.com/macintacos/coroner/internal/cli/overlay"
	"github.com/macintacos/coroner/internal/cli/source"
	"github.com/macintacos/coroner/internal/cli/tag"
	timelinecmd "github.com/macintacos/coroner/internal/cli/timeline"
)

// NewRoot builds coroner's command tree, whose commands run against app. Execute it only
// through Run, which supplies its arguments, streams and exit codes. Commands use ctx, the
// context Run executes them with, not cmd.Context(): contextcheck accepts only a context
// that arrives as a parameter.
func NewRoot(ctx context.Context, app clikit.App) *cobra.Command {
	root := &cobra.Command{
		Use:   "coroner <command> [flags]",
		Short: "Build root-cause investigation timelines",
		Long: "coroner records what happened during an investigation, in order: name a timeline,\n" +
			"add timestamped events from logs, chat and people, then view and export it.",
		Example: "  $ coroner timeline create checkout-outage --title \"Checkout outage\"\n" +
			"  $ coroner event add -t checkout-outage \"Error rate crossed 5%\" --at 2026-09-11T03:14Z --source datadog\n" +
			"  $ coroner event list -t checkout-outage",
		Annotations: map[string]string{clikit.FormatsAnnotation: "text,json"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if showVersion, _ := cmd.Flags().GetBool("version"); showVersion {
				return writeVersion(cmd, app)
			}

			return cmd.Help()
		},
	}

	root.PersistentFlags().AddFlagSet(clikit.GlobalFlags())
	// Cobra's own Version field would print through OutOrStdout rather than app.Out.
	root.Flags().Bool("version", false, "Show coroner's version")
	// Run prints errors once, and usage only on request.
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return clikit.UsageError(cmd, err)
	})

	topics := commandTopics()
	help := newHelpTemplate(topics)
	// Cobra's help function writes through OutOrStdout, bypassing app.Out.
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		// A help function cannot fail the command.
		_ = help.Execute(app.Out, cmd)
	})

	root.AddGroup(
		&cobra.Group{ID: clikit.CoreCommands, Title: "CORE COMMANDS"},
		&cobra.Group{ID: clikit.AdditionalCommands, Title: "ADDITIONAL COMMANDS"},
	)
	root.SetCompletionCommandGroupID(clikit.AdditionalCommands)
	root.AddCommand(
		newVersionCommand(app), timelinecmd.NewCommand(ctx, app), newStatusCommand(ctx, app), event.NewCommand(ctx, app, timelinecmd.AskCreate),
		annotation.NewCommand(ctx, app, timelinecmd.AskCreate), attachment.NewCommand(ctx, app), source.NewCommand(ctx, app), tag.NewCommand(ctx, app),
		newImportCommand(ctx, app), overlay.NewCommand(ctx, app), newAgentCommand(app), newInternalCommand(ctx, app),
	)

	// Last, in this order: cobra adds help and completion only to a root with subcommands,
	// and AddTimelineCompletion and wrapRunErrors reach only the commands already added, so
	// help, like --help, ignores --format and --json.
	clikit.AddTimelineCompletion(ctx, root, app)
	addCompletionCommand(root, app)
	wrapRunErrors(root)
	addHelpCommand(root, app, topics)

	return root
}

// addCompletionCommand adds cobra's completion command now rather than at execution, so its
// scripts go to app.Out: it captures root's OutOrStdout when added. Out is set for that call
// only, before anything can print a deprecation notice through it.
func addCompletionCommand(root *cobra.Command, app clikit.App) {
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
			if cmd.Annotations == nil {
				cmd.Annotations = map[string]string{}
			}
			cmd.Annotations[clikit.DiagnosticLogAnnotation] = "off"
			for _, shell := range cmd.Commands() {
				shell.Example = examples[shell.Name()]
			}
		}
	}
}

// wrapRunErrors makes every error a command's hooks or body return an *Error, recovering a
// panic into an internal one, so Run can tell an untyped error from cobra, which rejected the
// command line, from a command's own. A body runs only once checkCommandLine accepts args.
func wrapRunErrors(cmd *cobra.Command) {
	hooks := []*func(*cobra.Command, []string) error{
		&cmd.PersistentPreRunE, &cmd.PreRunE, &cmd.RunE, &cmd.PostRunE, &cmd.PersistentPostRunE,
	}
	for _, hook := range hooks {
		if run := *hook; run != nil {
			*hook = typedHook(run)
		}
	}

	if check := cmd.Args; check != nil {
		cmd.Args = func(cmd *cobra.Command, args []string) error {
			err := check(cmd, args)
			if _, ok := errors.AsType[*clikit.Error](err); err != nil && !ok {
				return clikit.ArgsError(cmd, err)
			}

			return err
		}
	}

	if run := cmd.RunE; run != nil {
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			err := checkCommandLine(cmd, args)
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

// checkCommandLine accepts cmd's --format and --json through OutputFormat, and its -t,
// --overlay and positional timeline through ReadTimelineInput. A command with subcommands
// prints only help or an error, so any format passes it.
func checkCommandLine(cmd *cobra.Command, args []string) error {
	if !cmd.HasSubCommands() {
		_, err := clikit.OutputFormat(cmd)
		if err != nil {
			return err
		}
	}
	_, err := clikit.ReadTimelineInput(cmd.Flags(), args, clikit.RuleOf(cmd))

	return err
}

// typedHook returns run with its errors and panics made *Error.
func typedHook(run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = &clikit.Error{
					Kind:    clikit.KindInternal,
					Message: fmt.Sprintf("unexpected panic: %v", r),
					Hint:    clikit.InternalHint,
					Err:     fmt.Errorf("%v\n%s", r, debug.Stack()),
				}
			}
		}()

		return clikit.AsInternal(run(cmd, args))
	}
}
