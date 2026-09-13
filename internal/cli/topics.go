package cli

import (
	"embed"
	"path"
	"strings"
)

//go:embed topics/*.md
var topicFiles embed.FS

// HelpTopic is a built-in help topic, printed by `coroner help <Name>`. Short is the first
// line's heading and Body the Markdown source.
type HelpTopic struct {
	Name, Short, Body string
}

// HelpTopics returns the built-in help topics, sorted by name.
func HelpTopics() []HelpTopic {
	// Embedded files cannot fail to read.
	entries, _ := topicFiles.ReadDir("topics")

	topics := make([]HelpTopic, len(entries))
	for i, entry := range entries {
		body, _ := topicFiles.ReadFile(path.Join("topics", entry.Name()))
		heading, _, _ := strings.Cut(string(body), "\n")

		topics[i] = HelpTopic{
			Name:  strings.TrimSuffix(entry.Name(), ".md"),
			Short: strings.TrimPrefix(heading, "# "),
			Body:  string(body),
		}
	}

	return topics
}

// closest returns the candidate the fewest single-letter edits from name, the first of
// several at the same distance.
func closest(name string, candidates []string) string {
	best, bestDistance := "", -1
	for _, candidate := range candidates {
		if distance := levenshtein(name, candidate); bestDistance < 0 || distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}

	return best
}

func levenshtein(a, b string) int {
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}

	for i := range len(a) {
		current := make([]int, len(b)+1)
		current[0] = i + 1
		for j := range len(b) {
			substitution := previous[j]
			if a[i] != b[j] {
				substitution++
			}

			current[j+1] = min(previous[j+1]+1, current[j]+1, substitution)
		}

		previous = current
	}

	return previous[len(b)]
}
