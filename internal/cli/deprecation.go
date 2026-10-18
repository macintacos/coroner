package cli

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// deprecation tells the users of a deprecated command or flag what to use instead, and the
// earliest release and day that may remove it. STABILITY.md sets how far away those may be.
type deprecation struct {
	// replacement completes cobra's sentence with what to use instead, such as
	// "use --json instead".
	replacement string
	// removalVersion is a release, such as v0.9.0.
	removalVersion string
	// removalDate is a day, such as 2027-03-14.
	removalDate string
}

// deprecateCommand hides cmd from help and makes running it warn on stderr with d's notice.
// It panics when d is incomplete, so every spec that builds the command tree fails.
func deprecateCommand(cmd *cobra.Command, d deprecation) {
	notice, err := d.notice()
	if err != nil {
		panic(fmt.Errorf("deprecate %s: %w", cmd.CommandPath(), err))
	}

	cmd.Deprecated = notice
	cmd.Hidden = true
}

// deprecateFlag hides the flag name in flags from help and makes passing it warn on stderr
// with d's notice. It panics when d is incomplete or flags has no such flag, so every spec
// that builds the command tree fails.
func deprecateFlag(flags *pflag.FlagSet, name string, d deprecation) {
	notice, err := d.notice()
	if err == nil {
		err = flags.MarkDeprecated(name, notice)
	}
	if err != nil {
		panic(fmt.Errorf("deprecate --%s: %w", name, err))
	}
}

// notice returns what cobra prints after "Flag --x has been deprecated, " or
// `Command "x" is deprecated, `, or an error naming each part d leaves out or gets wrong.
func (d deprecation) notice() (string, error) {
	var problems []error
	if d.replacement == "" {
		problems = append(problems, errors.New("name a replacement"))
	}
	if !regexp.MustCompile(`^v\d+\.\d+\.\d+$`).MatchString(d.removalVersion) {
		problems = append(problems, fmt.Errorf("removal version %q is not vMAJOR.MINOR.PATCH", d.removalVersion))
	}
	_, err := time.Parse(time.DateOnly, d.removalDate)
	if err != nil {
		problems = append(problems, fmt.Errorf("removal date %q is not YYYY-MM-DD", d.removalDate))
	}
	if len(problems) > 0 {
		return "", errors.Join(problems...)
	}

	return fmt.Sprintf("%s; it may be removed in %s or later, and no earlier than %s",
		d.replacement, d.removalVersion, d.removalDate), nil
}
