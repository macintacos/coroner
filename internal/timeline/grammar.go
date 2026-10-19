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

// SuggestTimelineName returns input repaired into a valid timeline name, or "" when nothing
// valid remains.
func SuggestTimelineName(input string) string {
	return suggestSlug(input, namePunct, ValidateTimelineName)
}

// SuggestOverlayName returns input repaired into a valid overlay name, or "" when nothing
// valid remains.
func SuggestOverlayName(input string) string {
	return suggestSlug(input, namePunct, ValidateOverlayName)
}

// SuggestSourceName returns input repaired into a valid source name, or "" when nothing
// valid remains.
func SuggestSourceName(input string) string {
	return suggestSlug(input, sourceTagPunct, ValidateSourceName)
}

// SuggestTag returns input repaired into a valid tag, or "" when nothing valid remains.
func SuggestTag(input string) string {
	return suggestSlug(input, sourceTagPunct, ValidateTag)
}

// suggestSlug lowercases input, dashes runs of other characters, trims what a slug cannot
// start or end with and cuts it to maxSlugLength, returning "" unless valid accepts the result.
func suggestSlug(input, punct string, valid func(string) error) string {
	var b strings.Builder
	for _, r := range strings.ToLower(input) {
		if !strings.ContainsRune(slugStart+punct, r) {
			r = '-'
		}
		if r == '-' && strings.HasSuffix(b.String(), "-") {
			continue
		}
		b.WriteRune(r)
	}

	slug := strings.TrimLeft(b.String(), punct)
	slug = strings.TrimRight(slug[:min(len(slug), maxSlugLength)], "-")
	if valid(slug) != nil {
		return ""
	}

	return slug
}

func validateSlug(field, what, slug, punct string) error {
	if slug != "" && len(slug) <= maxSlugLength &&
		strings.ContainsRune(slugStart, rune(slug[0])) && strings.Trim(slug, slugStart+punct) == "" {
		return nil
	}

	return problem(field, RuleGrammar, "%s %q: must be at most %d lowercase letters, digits or %q, starting with a letter or digit",
		what, slug, maxSlugLength, punct)
}
