# Comment in the HTML page

*Audience: people who share a timeline's HTML page and want comments back, and the
colleagues who comment on it.*

Commenting on a timeline does not need coroner. You export the timeline as an HTML page
and send it; your colleague opens it in a browser, adds comments to events, and sends back
a file that `coroner import` brings into your database, with their name on each comment. A
comment is the kind of annotation that holds someone's commentary. The HTML file itself
never changes: the browser keeps the comments until your colleague exports them to a file
of their own and sends it.

## Share the page

Export the timeline as one self-contained HTML file from the database you will import the
comments into, and send it:

```sh
coroner timeline export -t inc-4821 --format html -o inc-4821.html
```

## Add comments

1. Open the file in a browser and select an event on the timeline.

2. Choose **Add comment**, below the event's details. The first time, the page asks for
   your name, which becomes the author of every comment you write. The browser remembers
   it; **Change name** changes it.

3. Write the comment and choose **Save draft**. The draft appears under the event's
   annotations in a dashed box headed "Draft comment by" and your name, with **Edit** and
   **Discard** buttons.

Drafts are kept in your browser, under the page's timeline and your name. Closing the page
and opening the same file again brings them back. The page of another timeline does not
show them, and neither does a different name: after **Change name**, the page shows the
drafts saved under the new name, and the earlier name's drafts come back if you change
back to it. If the browser cannot store drafts, which can happen to a file opened from
disk, the page says so and the drafts last until you close it, so export them before you
do.

The page adds comments on events only: it cannot add events or evidence, or change what
the export holds.

## Send your comments back

1. Choose **Export annotations (.jsonl)**, above the event details. The browser downloads
   your drafts as a file named after the timeline and your name, such as
   `inc-4821-annotations-Priya-Shah.jsonl`. Characters in your name other than letters,
   digits, dots, underscores and hyphens become hyphens. A page of several timelines,
   exported with `-t` given once for each, joins their names with `+`.

2. Send the file to whoever shared the page.

3. Choose **Clear exported drafts** to remove the drafts you exported from the browser.
   Drafts you add after exporting stay.

The file is a coroner JSONL export: a header, then one comment annotation per line, as the
[export format](../reference/export-format.md) describes.

## Import the comments

The file holds only comments, so import it into the database you exported the page from,
which has the timeline and its events. Preview what it adds first:

```sh
coroner import inc-4821-annotations-Priya-Shah.jsonl --dry-run
```

stderr counts the comments it would add:

```text
Would import annotations: 2 inserted, 0 identical, 0 conflicting
```

Then import it, and list the timeline's annotations to see each comment with its author:

```sh
coroner import inc-4821-annotations-Priya-Shah.jsonl
coroner annotation list -t inc-4821
```

`coroner timeline view -t inc-4821` shows them under their events too. Importing the same
file again changes nothing. A database without the timeline or one of the events refuses
the file and writes nothing; for that, conflicts and every other case, see
[Import a colleague's export](import.md).
