# Capture Slack messages

*Audience: people who use coroner and want a Slack message on their timeline.*

A Slack message becomes an event at the moment it was posted, linked to the message, with
the message's text kept word for word as evidence. coroner does not read Slack; you copy
the link and the text, and write the event's text yourself.

## Add the message as an event

1. In Slack, open the message's **More actions** menu and choose **Copy link**. The link
   looks like `https://acme.slack.com/archives/C024BE91L/p1789084692123456`. If it ends in
   `?thread_ts=…`, ignore that part.

2. Take the digits after `p`. They are the time the message was posted, as microseconds
   since the Unix epoch.

3. Add the event with those digits as its time, the link as its URL, and `slack` as its
   source:

   ```console
   $ coroner event add "On-call reports checkout p99 at 4s" --at 1789084692123456 --url https://acme.slack.com/archives/C024BE91L/p1789084692123456 --source slack
   Added event #1 to timeline `checkout-latency` at 2026-09-10T23:58:12.123456Z (+00:00 local: 23:58:12)
   ```

   The time is exact to the microsecond and needs no time zone, so the event sorts
   correctly beside log lines from any host.

A message's `ts` from the Slack API or a workspace export, such as `1789084692.123456`, is
the same time in seconds; pass it to `--at` as it is.

## Keep the message text as evidence

1. Select the message text in Slack and copy it.

2. Pipe the clipboard into an evidence annotation on the event, using the seq coroner
   printed:

   ```console
   $ pbpaste | coroner annotation add - -e 1 --kind evidence --source slack --url https://acme.slack.com/archives/C024BE91L/p1789084692123456
   Added evidence annotation 01a09e58-fca7-7426-b74d-7c8700623f2d to event #1 in timeline `checkout-latency`
   ```

   On Linux, use `wl-paste` or `xclip -selection clipboard -o` in place of `pbpaste`.

Reading the body from stdin keeps the message exactly as it was, quotes and line breaks
included, without shell quoting; coroner drops only one trailing newline.

A wrong permalink or source can be fixed later with
`coroner annotation edit <id> --url <url>`, or cleared with `--url none` or
`--source none`, as on `coroner event edit`.
