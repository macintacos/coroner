package timeline

import (
	"fmt"
	"strings"
)

const (
	slugStart     = "abcdefghijklmnopqrstuvwxyz0123456789"
	nameChars     = slugStart + "._-"
	labelChars    = slugStart + "._:/-"
	maxSlugLength = 64
)

// ValidateTimelineName reports whether name matches [a-z0-9][a-z0-9._-]* in at most 64
// characters.
func ValidateTimelineName(name string) error {
	return validateSlug("timeline name", name, nameChars)
}

// ValidateSourceName reports whether name matches [a-z0-9][a-z0-9._:/-]* in at most 64
// characters.
func ValidateSourceName(name string) error {
	return validateSlug("source", name, labelChars)
}

// ValidateTag reports whether tag matches [a-z0-9][a-z0-9._:/-]* in at most 64 characters.
func ValidateTag(tag string) error {
	return validateSlug("tag", tag, labelChars)
}

func validateSlug(what, s, chars string) error {
	if s != "" && len(s) <= maxSlugLength &&
		strings.ContainsRune(slugStart, rune(s[0])) && strings.Trim(s, chars) == "" {
		return nil
	}

	return fmt.Errorf("%s %q: use at most %d lowercase letters, digits and %q, starting with a letter or digit",
		what, s, maxSlugLength, chars[len(slugStart):])
}
