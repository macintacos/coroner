"use strict";

// Every string from the export is text a person typed or pasted from logs and chat, so it
// reaches the page only through textContent. vis-timeline reads string content and titles as
// HTML, so items and groups get elements instead, and no item has a title.
{
  const read = (id) => JSON.parse(document.getElementById(id).textContent);
  const data = read("coroner-data");
  const zone = read("coroner-zone");

  const element = (tag, text, className) => {
    const node = document.createElement(tag);
    if (text !== undefined) node.textContent = text;
    if (className) node.className = className;
    return node;
  };

  // Browsers disagree on parsing more than three fractional digits, and a Date holds
  // milliseconds anyway.
  const instant = (text) => new Date(text.replace(/(\.\d{3})\d+/, "$1"));

  // spanAt returns the zone span in force at ms; spans are in start order, and the first also
  // stands for all time before it.
  const spanAt = (ms) => {
    let low = 0;
    let high = zone.spans.length - 1;
    while (low < high) {
      const middle = (low + high + 1) >> 1;
      if (zone.spans[middle].start <= ms) low = middle;
      else high = middle - 1;
    }
    return zone.spans[low];
  };

  // atOffset returns the moment at ms, offset seconds east of UTC. keepMinutes stops moment
  // reading an offset under 16 minutes as hours.
  const atOffset = (ms, offset) => vis.moment(ms).utcOffset(offset / 60, false, true);

  // vis-timeline steps its axis by cloning a moment and adding days or months to it, so a
  // zone moment re-reads its offset after every change, the way moment-timezone does.
  vis.moment.momentProperties.push("_coronerZone");
  vis.moment.updateOffset = (moment, keepTime) => {
    if (!moment._coronerZone) return;
    const offset = spanAt(moment.valueOf()).offset / 60;
    if (moment.utcOffset() !== offset) moment.utcOffset(offset, keepTime, true);
  };

  // vis-timeline calls this for every date it draws, so the axis follows the export zone.
  const inZone = (date) => {
    const ms = vis.moment(date).valueOf();
    const moment = atOffset(ms, spanAt(ms).offset);
    moment._coronerZone = true;
    return moment;
  };

  const fractionDigits = { millisecond: 3, microsecond: 6, nanosecond: 9 };
  const clockLayouts = { hour: "HH:--", minute: "HH:mm" };

  // when returns an instant at precision in the export zone, with the zone's abbreviation
  // after a clock time. An hour shows its minutes when the zone puts it off the hour. Digits
  // below the second come from the text, which keeps all nine.
  const when = (text, precision) => {
    const moment = inZone(instant(text));
    if (precision === "day") return moment.format("YYYY-MM-DD");
    const clock = precision === "hour" && moment.minute() !== 0 ? "HH:mm" : clockLayouts[precision] ?? "HH:mm:ss";
    let shown = moment.format("YYYY-MM-DD " + clock);
    const digits = fractionDigits[precision];
    if (digits) shown += "." + text.match(/\.(\d+)/)[1].slice(0, digits);
    return shown + " " + spanAt(moment.valueOf()).abbreviation;
  };

  // started returns when an event started; a day-precision event's date is the one it was
  // recorded on, in its own offset.
  const started = (event) =>
    event.precision === "day"
      ? atOffset(instant(event.occurred_at).valueOf(), event.occurred_offset ?? 0).format("YYYY-MM-DD")
      : when(event.occurred_at, event.precision);

  const firstLine = (text) => text.split(/\r\n|\r|\n/)[0];

  const link = (url) => {
    if (!/^https?:\/\//i.test(url)) return element("span", url);
    const anchor = element("a", url);
    anchor.href = url;
    anchor.rel = "noopener noreferrer";
    anchor.target = "_blank";
    return anchor;
  };

  const annotationsOn = new Map();
  const onTimeline = [];
  for (const annotation of data.annotations) {
    if (annotation.event_id === null) {
      onTimeline.push(annotation);
      continue;
    }
    if (!annotationsOn.has(annotation.event_id)) annotationsOn.set(annotation.event_id, []);
    annotationsOn.get(annotation.event_id).push(annotation);
  }

  const annotationList = (annotations) => {
    const list = element("ul", undefined, "annotations");
    for (const annotation of annotations) {
      const kind = annotation.kind.charAt(0).toUpperCase() + annotation.kind.slice(1);
      const item = element("li");
      item.append(element("p", annotation.author === null ? kind : kind + " by " + annotation.author, "label"));
      item.append(element(annotation.kind === "evidence" ? "pre" : "p", annotation.body, "body"));
      if (annotation.url !== null) item.append(link(annotation.url));
      list.append(item);
    }
    return list;
  };

  const field = (list, name, value) => {
    list.append(element("dt", name));
    const description = element("dd");
    description.append(value);
    list.append(description);
  };

  const eventDetail = (event) => {
    let heading = started(event);
    if (event.ended_at !== null) heading += " to " + when(event.ended_at, event.precision);
    if (event.approximate) heading = "About " + heading;
    const nodes = [element("h2", heading), element("p", event.text, "text")];

    const fields = element("dl");
    if (event.seq !== null) field(fields, "Event", "#" + event.seq);
    field(fields, "Source", event.source ?? "(no source)");
    field(fields, "Tags", event.tags.length ? event.tags.map((tag) => "#" + tag).join(" ") : "(untagged)");
    if (event.url !== null) field(fields, "URL", link(event.url));
    if (event.author !== null) field(fields, "Added by", event.author);
    nodes.push(fields);

    const annotations = annotationsOn.get(event.id) ?? [];
    if (annotations.length) nodes.push(element("h3", "Annotations"), annotationList(annotations));
    return nodes;
  };

  const timelineDetail = () => {
    const hint = data.events.length ? "Select an event to see its full text and annotations." : "This export has no events.";
    const nodes = [element("p", hint, "hint")];
    if (onTimeline.length) nodes.push(element("h3", "On the whole timeline"), annotationList(onTimeline));
    return nodes;
  };

  const exported = inZone(instant(data.exported_at));
  const shown = data.timelines[0];
  const heading = shown.title.trim() ? shown.title + " (" + shown.name + ")" : shown.name;
  document.title = heading;
  document.getElementById("heading").textContent = heading;
  if (shown.refs.length) document.getElementById("refs").textContent = shown.refs.join(", ");
  else document.getElementById("refs").remove();
  document.getElementById("meta").textContent =
    "Times in " + zone.name + ". Exported " + exported.format("YYYY-MM-DD HH:mm") + " " +
    spanAt(exported.valueOf()).abbreviation + " by coroner " + data.coroner_version + ".";

  const detail = document.getElementById("detail");
  const show = (event) => detail.replaceChildren(...(event ? eventDetail(event) : timelineDetail()));

  const sources = [...new Set(data.events.map((event) => event.source))];
  if (sources.includes(null)) sources.push(sources.splice(sources.indexOf(null), 1)[0]);
  const groupOf = new Map(sources.map((source, i) => [source, i]));
  const groups = sources.map((source, i) => ({ id: i, order: i, content: element("span", source ?? "(no source)") }));

  const eventsById = new Map(data.events.map((event) => [event.id, event]));
  const items = data.events.map((event) => {
    const item = {
      id: event.id,
      group: groupOf.get(event.source),
      start: instant(event.occurred_at),
      content: element("span", (event.approximate ? "~" : "") + firstLine(event.text)),
      type: event.ended_at === null ? "point" : "range",
    };
    if (event.ended_at !== null) item.end = instant(event.ended_at);
    return item;
  });

  const options = { moment: inZone, groupOrder: "order", showTooltips: false };
  if (items.length) {
    // A point's label runs right of its dot, so the last one needs room past its instant.
    const first = +items[0].start;
    const last = items.reduce((latest, item) => Math.max(latest, +(item.end ?? item.start)), first);
    // A lone instant still opens on a minute of axis, not a zero-width window.
    const span = Math.max(last - first, 60 * 1000);
    options.start = first - span * 0.05;
    options.end = last + span * 0.3;
  }
  const timeline = new vis.Timeline(document.getElementById("timeline"), items, groups, options);
  timeline.on("select", ({ items: selected }) => show(eventsById.get(selected[0]) ?? null));
  show(null);
}
