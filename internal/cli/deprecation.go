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
	// replacement says what to use instead, such as "use --json instead".
	replacement string
	// removalVersion is a release, such as v0.9.0.
	removalVersion string
	// removalDate is a day, such as 2027-03-14.
	removalDate string
}

// deprecateCommand hides cmd from help and makes running it warn on stderr with d's notice.
func deprecateCommand(cmd *cobra.Command, d deprecation) error {
	notice, err := d.notice()
	if err != nil {
		return fmt.Errorf("deprecate %s: %w", cmd.CommandPath(), err)
	}

	cmd.Deprecated = notice

	return nil
}

// deprecateFlag hides the flag name in flags from help and makes passing it warn on stderr
// with d's notice.
func deprecateFlag(flags *pflag.FlagSet, name string, d deprecation) error {
	notice, err := d.notice()
	if err != nil {
		return fmt.Errorf("deprecate --%s: %w", name, err)
	}

	return flags.MarkDeprecated(name, notice)
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
