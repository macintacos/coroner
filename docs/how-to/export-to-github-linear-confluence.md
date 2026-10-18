# Share a timeline in GitHub, Linear, Confluence and chat

*Audience: people who use coroner and want to paste a timeline into a ticket, a wiki page
or a chat.*

The Markdown export is written to be pasted. It uses headings, bullets, bold, italics,
inline code, a fenced code block for evidence of more than one line, and links to
attachments, with no tables and no front matter, so most destinations keep its structure.
This page lists what survives in each one.

## Copy the export

Export the current timeline as Markdown into a file:

```sh
coroner timeline export --format md > rca.md
```

Copy the file to the clipboard, then paste it where the steps below say:

```sh
pbcopy < rca.md
```

On Linux, use `wl-copy < rca.md` or `xclip -selection clipboard < rca.md`.

Times are in UTC. To show them in the zone your readers work in, add `--tz`, as in
`--tz America/New_York`.

## GitHub and GitLab

Paste into an issue, a pull request or a comment. Headings, bullets, bold, code and links
all render.

Both render Mermaid code blocks as diagrams, so you can append a gantt chart of the
events, one section per source:

```sh
coroner timeline export --format md --diagram > rca.md
```

## Linear

Paste into an issue description or a comment. The editor converts the Markdown as you
paste it, keeping headings, bullets, bold, code and links.

## Confluence

Paste into a page. Confluence converts the Markdown, with two limits:

- Lists nested more than three levels deep are collapsed. The export nests annotations
  only one level under their events, so it stays within the limit.
- A YAML front matter block at the top breaks the conversion. The export has none; do not
  add one before pasting.

## Google Docs

Turn on Markdown once, under **Tools › Preferences › Enable Markdown**. Then paste with
**Paste from Markdown**.

## Slack

Slack's message composer understands only part of Markdown: headings appear as literal `#`
characters, and tables are dropped. The export uses bullets rather than tables for this
reason, so the events and annotations stay readable.

Paste the export into a message. For a long timeline, upload `rca.md` as a file instead.

## Attachments

Attachments are the files added with `--attach`. Pasting Markdown uploads none of them, in
any of these destinations. The export links each attachment to a file on your machine, and
says so on stderr.

To share attachments, export into a directory. coroner writes the Markdown and every
attachment beside it, linked by relative paths:

```console
$ coroner timeline export --format md -o rca/
Wrote rca/checkout-latency.md
Wrote 1 attachment file to rca/attachments
```

For GitHub and GitLab, commit `rca/` to a repository and open `rca/checkout-latency.md`
there, where its images show. For Confluence and other destinations, attach the files from
`rca/attachments/` to the page by hand.
