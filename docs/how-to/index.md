# How-to guides

*Audience: people who already use coroner and have a specific task to do.*

A how-to guide gives the steps for one task, assuming you know the basics. To learn those
basics, start with the [tutorial](../tutorial/index.md). For every flag a command takes,
see the [CLI reference](../reference/cli/coroner.md).

- [Install coroner](install.md): install, upgrade or remove coroner on macOS or Linux.
- [Import a colleague's export](import.md): bring a colleague's events, annotations and
  attachments back into your database, and resolve conflicts.
- [Comment in the HTML page](comment-in-the-html-page.md): add comments to a shared
  timeline's page without coroner, and import them.
- [Capture Slack messages](capture-slack-messages.md): add a Slack message as an event at
  the time it was posted, with its text as evidence.
- [Add log lines](add-log-lines.md): turn MongoDB, syslog and application log lines into
  events, reading each format's time correctly.
- [Add events with the form](add-events-with-the-form.md): add events at a terminal by
  answering prompts instead of typing flags.
- [Share a timeline in GitHub, Linear, Confluence and chat](export-to-github-linear-confluence.md):
  paste the Markdown export, and what survives in each destination.
- [Query a timeline with jq](query-with-jq.md): answer questions about a JSONL export.
- [Show a timeline's events on a Grafana dashboard](grafana-annotations.md): send each
  event to Grafana as an annotation.
- [Use coroner with AI agents](use-with-ai-agents.md): install the agent skill that
  teaches a coding agent to record timelines, and give it the whole CLI in one page.
