package cli

import (
	"errors"

	"github.com/macintacos/coroner/internal/timeline"
)

// kindOf is the one table from a domain rule to an exit kind.
func kindOf(r timeline.Rule) Kind {
	switch r {
	case timeline.RuleYearRange, timeline.RuleEndedBeforeOccurred, timeline.RuleOffsetRange:
		return KindInvalidTime
	case timeline.RuleGrammar, timeline.RuleReserved:
		return KindInvalidName
	case timeline.RuleBlank, timeline.RuleNotUTF8, timeline.RuleUnknown, timeline.RuleSelfLink,
		timeline.RulePrecisionStart, timeline.RuleOwner, timeline.RuleSHA256, timeline.RuleNegativeSize:
		return KindUsage
	}

	return KindInternal
}

// recordError returns a *timeline.ValidationError as the *Error its first problem's rule names,
// with every problem in Message as Validate wrote them and hint on a usage problem only, since
// hints name flags, which an invalid_time or invalid_name message already does. nil, an *Error
// and any other error come back unchanged.
func recordError(err error, hint string) error {
	if _, ok := errors.AsType[*Error](err); ok {
		return err
	}
	first, ok := firstProblem(err)
	if !ok {
		return err
	}
	kind := kindOf(first.Rule)
	if kind != KindUsage {
		hint = ""
	}

	return &Error{Kind: kind, Message: err.Error(), Hint: hint, Err: err}
}

// firstProblem returns the first problem of a *timeline.ValidationError in err and true, and
// false when err holds none.
func firstProblem(err error) (timeline.Problem, bool) {
	verr, ok := errors.AsType[*timeline.ValidationError](err)
	if !ok || len(verr.Problems) == 0 {
		return timeline.Problem{}, false
	}

	return verr.Problems[0], true
}
