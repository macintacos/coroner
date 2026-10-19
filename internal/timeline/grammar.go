package timeline

import "strings"

const (
	slugStart      = "abcdefghijklmnopqrstuvwxyz0123456789"
	namePunct      = "._-"
	sourceTagPunct = "._:/-"
	maxSlugLength  = 64
)

// ValidateTimelineName returns an error unless name matches [a-z0-9][a-z0-9._-]* in at
// most 64 characters.
func ValidateTimelineName(name string) error {
	return validateSlug("name", "timeline name", name, namePunct)
}

// ValidateOverlayName returns an error unless name follows the timeline name grammar.
func ValidateOverlayName(name string) error {
	return validateSlug("name", "overlay name", name, namePunct)
}

// ValidateSourceName returns an error unless name matches [a-z0-9][a-z0-9._:/-]* in at
// most 64 characters and is not none, the keyword that clears an event's source.
func ValidateSourceName(name string) error {
	return validateSourceName("source", name)
}

func validateSourceName(field, name string) error {
	if name == "none" {
		return problem(field, RuleReserved, "source %q: is reserved for clearing a source", name)
	}

	return validateSlug(field, "source", name, sourceTagPunct)
}

// ValidateTag returns an error unless tag matches [a-z0-9][a-z0-9._:/-]* in at most 64
// characters.
func ValidateTag(tag string) error {
	return validateSlug("tags", "tag", tag, sourceTagPunct)
}

func validateSlug(field, what, slug, punct string) error {
	if slug != "" && len(slug) <= maxSlugLength &&
		strings.ContainsRune(slugStart, rune(slug[0])) && strings.Trim(slug, slugStart+punct) == "" {
		return nil
	}

	return problem(field, RuleGrammar, "%s %q: must be at most %d lowercase letters, digits or %q, starting with a letter or digit",
		what, slug, maxSlugLength, punct)
}
