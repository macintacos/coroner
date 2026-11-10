package help

import (
	"embed"
	"path"
	"slices"
	"strings"
)

//go:embed topics/*.md
var topicFiles embed.FS

// Topic is a built-in help topic, printed by `coroner help <Name>`. Short is the file's
// first line, which must be a `# ` heading, without the marker. Body is the Markdown source.
type Topic struct {
	Name, Short, Body string
}

// Topics returns the built-in help topics, in file-name order.
func Topics() []Topic {
	// Embedded files cannot fail to read.
	entries, _ := topicFiles.ReadDir("topics")

	topics := make([]Topic, len(entries))
	for i, entry := range entries {
		body, _ := topicFiles.ReadFile(path.Join("topics", entry.Name()))
		heading, _, _ := strings.Cut(string(body), "\n")

		topics[i] = Topic{
			Name:  strings.TrimSuffix(entry.Name(), ".md"),
			Short: strings.TrimPrefix(heading, "# "),
			Body:  string(body),
		}
	}

	return topics
}

func topicNamed(topics []Topic, name string) (Topic, bool) {
	i := slices.IndexFunc(topics, func(topic Topic) bool { return topic.Name == name })
	if i < 0 {
		return Topic{}, false
	}

	return topics[i], true
}
