package cli

import (
	"embed"
	"path"
	"slices"
	"strings"
)

//go:embed topics/*.md
var topicFiles embed.FS

// HelpTopic is a built-in help topic, printed by `coroner help <Name>`. Short is the file's
// first line, which must be a `# ` heading, without the marker. Body is the Markdown source.
type HelpTopic struct {
	Name, Short, Body string
}

// HelpTopics returns the built-in help topics, in file-name order.
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

func topicNamed(topics []HelpTopic, name string) (HelpTopic, bool) {
	i := slices.IndexFunc(topics, func(topic HelpTopic) bool { return topic.Name == name })
	if i < 0 {
		return HelpTopic{}, false
	}

	return topics[i], true
}
