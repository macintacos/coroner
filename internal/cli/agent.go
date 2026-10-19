package cli

import (
	"io"

	"github.com/spf13/cobra"
)

func newAgentCommand(app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Print what coding agents need to drive coroner",
		Long: "coroner ships an Agent Skill that teaches coding agents to build timelines with flags and\n" +
			"JSON. 'coroner agent skill' prints the one this binary was built with.",
		GroupID: additionalCommands,
		Example: "  $ coroner agent skill",
		RunE:    runNoun,
	}

	cmd.AddCommand(newAgentSkillCommand(app))

	return cmd
}

func newAgentSkillCommand(app App) *cobra.Command {
	return &cobra.Command{
		Use:   "skill",
		Short: "Print the Agent Skill for this version of coroner",
		Long: "Print the SKILL.md of the coroner Agent Skill this binary was built with, so the skill an\n" +
			"agent reads matches the commands it runs. Its references ship with the skill's directory\n" +
			"in the repository, not in the binary.",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{formatsAnnotation: "text"},
		Example: "  $ coroner agent skill\n" +
			"  $ coroner agent skill | diff - ~/.claude/skills/coroner/SKILL.md",
		RunE: func(*cobra.Command, []string) error {
			_, err := io.WriteString(app.Out, app.Skill)
			return err
		},
	}
}
