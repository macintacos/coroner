package timeline

import (
	"errors"
	"fmt"
	"strings"
)

// Rule names the domain rule a Problem breaks. Its values are closed: callers switch on them.
type Rule string

// Rules a record can break.
const (
	RuleBlank               Rule = "blank"
	RuleNotUTF8             Rule = "not_utf8"
	RuleGrammar             Rule = "grammar"
	RuleReserved            Rule = "reserved"
	RuleUnknown             Rule = "unknown"
	RuleYearRange           Rule = "year_range"
	RulePrecisionStart      Rule = "precision_start"
	RuleEndedBeforeOccurred Rule = "ended_before_occurred"
	RuleOffsetRange         Rule = "offset_range"
	RuleSelfLink            Rule = "self_link"
	RuleOwner               Rule = "owner"
	RuleSHA256              Rule = "sha256"
	RuleNegativeSize        Rule = "negative_size"
)

// Problem is one way a record breaks a domain rule. Field is the record's export key.
type Problem struct {
	Field   string
	Rule    Rule
	Message string
}

// ValidationError lists every Problem a record's Validate found, in the order it checked them.
type ValidationError struct {
	Problems []Problem
}

func (e *ValidationError) Error() string {
	messages := make([]string, 0, len(e.Problems))
	for _, p := range e.Problems {
		messages = append(messages, p.Message)
	}

	return strings.Join(messages, "\n")
}

func problem(field string, rule Rule, format string, args ...any) error {
	return &ValidationError{Problems: []Problem{{Field: field, Rule: rule, Message: fmt.Sprintf(format, args...)}}}
}

// join merges the problems of each *ValidationError in errs, returning an untyped nil when
// there are none so a caller's err != nil stays false.
func join(errs ...error) error {
	var problems []Problem
	for _, err := range errs {
		if verr, ok := errors.AsType[*ValidationError](err); ok {
			problems = append(problems, verr.Problems...)
		}
	}
	if len(problems) == 0 {
		return nil
	}

	return &ValidationError{Problems: problems}
}
