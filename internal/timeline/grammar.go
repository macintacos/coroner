package timeline

import (
	"fmt"
	"strings"
)

const (
	slugStart      = "abcdefghijklmnopqrstuvwxyz0123456789"
	namePunct      = "._-"
	sourceTagPunct = "._:/-"
	maxSlugLength  = 64
)

// ValidateTimelineName returns an error unless name matches [a-z0-9][a-z0-9._-]* in at
// most 64 characters.
func ValidateTimelineName(name string) error {
	return validateSlug("timeline name", name, namePunct)
}

// ValidateSourceName returns an error unless name matches [a-z0-9][a-z0-9._:/-]* in at
// most 64 characters.
func ValidateSourceName(name string) error {
	return validateSlug("source", name, sourceTagPunct)
}

// ValidateTag returns an error unless tag matches [a-z0-9][a-z0-9._:/-]* in at most 64
// characters.
func ValidateTag(tag string) error {
	return validateSlug("tag", tag, sourceTagPunct)
}

func validateSlug(what, slug, punct string) error {
	if slug != "" && len(slug) <= maxSlugLength &&
		strings.ContainsRune(slugStart, rune(slug[0])) && strings.Trim(slug, slugStart+punct) == "" {
		return nil
	}

	return fmt.Errorf("%s %q: must be at most %d lowercase letters, digits or %q, starting with a letter or digit",
		what, slug, maxSlugLength, punct)
}
