# Show a timeline's events on a Grafana dashboard

*Audience: people who use coroner and Grafana and want the timeline's events drawn over
their dashboard's graphs.*

Grafana draws annotations as markers on a dashboard's time axis. Its HTTP API adds one
annotation per `POST /api/annotations` request, with times in Unix milliseconds, and has
no endpoint for adding many at once. This recipe turns each event of a JSONL export into
one request body with jq and sends them with curl, so coroner itself needs no Grafana
address or credentials. See
[Grafana's annotations API](https://grafana.com/docs/grafana/latest/developers/http_api/annotations/)
for every field.

You need jq, curl, and a Grafana service account token that can write annotations.

## Build the request bodies

1. Export the current timeline:

   ```sh
   coroner timeline export --format jsonl > rca.jsonl
   ```

2. Save this jq program as `grafana.jq`. It converts each time to milliseconds, tags the
   annotation with `coroner`, the event's source and its tags, and adds `timeEnd` for an
   event that has an end:

   ```text
   def epoch_ms: (sub("\\.[0-9]+Z$"; "Z") | fromdateiso8601) * 1000 + (.[20:23] | tonumber);

   select(.type == "event")
   | {
       dashboardUID: $dashboard,
       time: (.occurred_at | epoch_ms),
       tags: (["coroner", (.source // empty)] + .tags),
       text: .text
     }
     + if .ended_at then {timeEnd: (.ended_at | epoch_ms)} else {} end
   ```

   jq's `fromdateiso8601` rejects fractional seconds
   ([jqlang/jq#2224](https://github.com/jqlang/jq/issues/2224)), so `epoch_ms` strips the
   fraction and adds back its first three digits. An export's times always have nine, in
   the same place.

3. Run it with the UID of the dashboard to annotate, from the dashboard's URL, and check
   the bodies:

   ```sh
   jq -c --arg dashboard checkout-overview -f grafana.jq rca.jsonl
   ```

   Run on
   [`full-model.jsonl`](https://github.com/macintacos/coroner/blob/main/testdata/export/v1.1/full-model.jsonl),
   an export in coroner's repository, it prints:

   ```json
   {"dashboardUID":"checkout-overview","time":1789084800000,"tags":["coroner","change-calendar","phase:context"],"text":"Maintenance window opens"}
   {"dashboardUID":"checkout-overview","time":1789095600000,"tags":["coroner","cron"],"text":"Nightly backup starts"}
   {"dashboardUID":"checkout-overview","time":1789096200000,"tags":["coroner","grafana"],"text":"Oplog window drops under an hour"}
   {"dashboardUID":"checkout-overview","time":1789096440000,"tags":["coroner"],"text":"Replica set has no primary","timeEnd":1789096590000}
   {"dashboardUID":"checkout-overview","time":1789096447000,"tags":["coroner","mongod","election","phase:detect","root-cause","severity:high"],"text":"Primary stepped down"}
   {"dashboardUID":"checkout-overview","time":1789096447412,"tags":["coroner","mongod"],"text":"Election timeout on db-3"}
   {"dashboardUID":"checkout-overview","time":1789096447412,"tags":["coroner","api"],"text":"Driver saw NotWritablePrimary"}
   {"dashboardUID":"checkout-overview","time":1789096448123,"tags":["coroner","mongod"],"text":"db-2 elected primary"}
   ```

   Grafana keeps milliseconds, so events less than a millisecond apart get the same
   `time`, like the two at 1789096447412.

To add organization annotations instead, which any dashboard can show by querying the
Grafana annotations data source by tag, delete the `dashboardUID: $dashboard,` line from
`grafana.jq` and drop `--arg dashboard checkout-overview`.

## Send them to Grafana

Set the Grafana address, then paste the token when `read -rs` waits for it, so it stays
out of your shell history. The header goes in a file only you can read, which keeps the
token off curl's command line. Send one request per body, then delete the file:

```sh
GRAFANA_URL=https://grafana.example.com
read -rs GRAFANA_TOKEN
(umask 077; printf 'Authorization: Bearer %s\n' "$GRAFANA_TOKEN" > grafana.headers)

jq -c --arg dashboard checkout-overview -f grafana.jq rca.jsonl |
  while read -r body; do
    curl -sS --fail-with-body -X POST "$GRAFANA_URL/api/annotations" \
      -H @grafana.headers \
      -H "Content-Type: application/json" \
      -d "$body"
    echo
  done

rm grafana.headers
```

Grafana answers each request with a message and the new annotation's ID, such as:

```json
{"message":"Annotation added","id":1}
```

Grafana does not recognise an annotation it already has, so running the loop twice adds
every annotation twice.
