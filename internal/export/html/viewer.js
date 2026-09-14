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
  const instant = (timestamp) => new Date(timestamp.replace(/(\.\d{3})\d+/, "$1"));

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
  // below the second come from the timestamp, which keeps all nine.
  const when = (timestamp, precision) => {
    const moment = inZone(instant(timestamp));
    if (precision === "day") return moment.format("YYYY-MM-DD");
    const clock = precision === "hour" && moment.minute() !== 0 ? "HH:mm" : clockLayouts[precision] ?? "HH:mm:ss";
    let shown = moment.format("YYYY-MM-DD " + clock);
    const digits = fractionDigits[precision];
    if (digits) shown += "." + timestamp.match(/\.(\d+)/)[1].slice(0, digits);
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

  const field = (list, label, value) => {
    list.append(element("dt", label));
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

  // Filters mean what event list's flags do, so the same words select the same events: any
  // checked source, every checked tag, text containing the search ignoring ASCII case only
  // (SQLite's lower()), and occurred at from since, inclusive, to until, exclusive.
  const foldASCII = (text) => text.replace(/[A-Z]/g, (letter) => letter.toLowerCase());

  const keeps = (event, filter) => {
    if (filter.sources.size && !filter.sources.has(event.source)) return false;
    if (![...filter.tags].every((tag) => event.tags.includes(tag))) return false;
    if (filter.q.trim() && !foldASCII(event.text).includes(foldASCII(filter.q))) return false;
    const at = instant(event.occurred_at).valueOf();
    return (filter.since === null || at >= filter.since) && (filter.until === null || at < filter.until);
  };

  // The time inputs hold wall-clock times in the export zone, as the axis shows. Subtracting
  // the offset a second time settles a time within a transition of the first guess.
  const wallToInstant = (value) => {
    const wall = Date.parse(value + "Z");
    return wall - spanAt(wall - spanAt(wall).offset * 1000).offset * 1000;
  };

  const instantToWall = (ms) => inZone(ms).format("YYYY-MM-DDTHH:mm:ss");

  // A bound is compared in whole milliseconds, which is exact against event list only when it
  // has no digits past the millisecond, so any past it must be zeros.
  const rfc3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,3}0{0,6})?Z$/;

  // readHash returns the filter and selection a location hash names, dropping any value this
  // export cannot show: a source, tag or event it lacks, an instant that is not RFC 3339 UTC
  // to the millisecond, or an until not after since.
  const readHash = (hash, known) => {
    const params = new URLSearchParams(hash.slice(1));
    const bound = (key) => (rfc3339.test(params.get(key)) ? instant(params.get(key)).valueOf() : null);
    const filter = {
      sources: new Set(params.getAll("source").filter((source) => known.sources.includes(source))),
      tags: new Set(params.getAll("tag").filter((tag) => known.tags.includes(tag))),
      q: params.get("q") ?? "",
      since: bound("since"),
      until: bound("until"),
    };
    if (filter.since !== null && filter.until !== null && filter.until <= filter.since) filter.until = null;
    const selected = known.events.has(params.get("sel")) ? params.get("sel") : null;
    return { filter, selected };
  };

  const hashOf = (filter, selected) => {
    const params = new URLSearchParams();
    for (const source of filter.sources) params.append("source", source);
    for (const tag of filter.tags) params.append("tag", tag);
    if (filter.q.trim()) params.set("q", filter.q);
    for (const key of ["since", "until"]) {
      if (filter[key] !== null) params.set(key, new Date(filter[key]).toISOString().replace(".000Z", "Z"));
    }
    if (selected !== null) params.set("sel", selected);
    const text = params.toString().replaceAll("%3A", ":");
    return text ? "#" + text : "";
  };

  const checkboxes = (fieldset, names, checked) => {
    fieldset.hidden = !names.length;
    for (const name of names) {
      const box = element("input");
      box.type = "checkbox";
      box.value = name;
      box.checked = checked.has(name);
      const label = element("label");
      label.append(box, name);
      fieldset.append(label);
    }
  };

  const checkedIn = (fieldset) => new Set([...fieldset.querySelectorAll("input:checked")].map((box) => box.value));

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
  const allTimesWindow = { start: options.start, end: options.end };

  const named = sources.filter((source) => source !== null);
  const tags = [...new Set(data.events.flatMap((event) => event.tags))].sort();
  const view = readHash(location.hash, { sources: named, tags, events: eventsById });

  const sourceBoxes = document.getElementById("sources");
  const tagBoxes = document.getElementById("tags");
  const search = document.getElementById("q");
  const since = document.getElementById("since");
  const until = document.getElementById("until");
  checkboxes(sourceBoxes, named, view.filter.sources);
  checkboxes(tagBoxes, tags, view.filter.tags);
  search.value = view.filter.q;
  const showBounds = () => {
    since.value = view.filter.since === null ? "" : instantToWall(view.filter.since);
    until.value = view.filter.until === null ? "" : instantToWall(view.filter.until);
  };
  showBounds();

  // A shared link opens on the time range it filters to.
  const start = view.filter.since ?? options.start;
  const end = view.filter.until ?? options.end;
  if (start < end) Object.assign(options, { start, end });

  // Filtering views of the items and groups, rather than replacing them, keeps the zoom.
  const visibleItems = new vis.DataView(new vis.DataSet(items), {
    filter: (item) => keeps(eventsById.get(item.id), view.filter),
  });
  const visibleGroups = new vis.DataView(new vis.DataSet(groups), {
    filter: (group) => !view.filter.sources.size || view.filter.sources.has(sources[group.id]),
  });
  const timeline = new vis.Timeline(document.getElementById("timeline"), visibleItems, visibleGroups, options);

  const writeHash = () => history.replaceState(null, "", location.href.split("#")[0] + hashOf(view.filter, view.selected));
  const select = (id) => {
    view.selected = visibleItems.get(id) ? id : null;
    show(eventsById.get(view.selected) ?? null);
  };
  const refilter = () => {
    visibleItems.refresh();
    visibleGroups.refresh();
    if (view.selected !== null) select(view.selected);
    writeHash();
  };

  timeline.on("select", ({ items: picked }) => {
    select(picked[0] ?? null);
    writeHash();
  });
  // Panning or zooming is the time brush: the window becomes since and until, to the second.
  timeline.on("rangechanged", ({ start: from, end: to, byUser }) => {
    if (!byUser) return;
    view.filter.since = Math.floor(+from / 1000) * 1000;
    view.filter.until = Math.ceil(+to / 1000) * 1000;
    showBounds();
    refilter();
  });

  document.getElementById("filters").addEventListener("submit", (event) => event.preventDefault());
  sourceBoxes.addEventListener("change", () => {
    view.filter.sources = checkedIn(sourceBoxes);
    refilter();
  });
  tagBoxes.addEventListener("change", () => {
    view.filter.tags = checkedIn(tagBoxes);
    refilter();
  });
  search.addEventListener("input", () => {
    view.filter.q = search.value;
    refilter();
  });
  const moveBounds = () => {
    view.filter.since = since.value ? wallToInstant(since.value) : null;
    view.filter.until = until.value ? wallToInstant(until.value) : null;
    const shownWindow = timeline.getWindow();
    const from = view.filter.since ?? +shownWindow.start;
    const to = view.filter.until ?? +shownWindow.end;
    if (from < to) timeline.setWindow(from, to);
    refilter();
  };
  since.addEventListener("change", moveBounds);
  until.addEventListener("change", moveBounds);
  document.getElementById("all-times").addEventListener("click", () => {
    view.filter.since = null;
    view.filter.until = null;
    showBounds();
    if (allTimesWindow.start !== undefined) timeline.setWindow(allTimesWindow.start, allTimesWindow.end);
    refilter();
  });

  if (view.selected !== null) timeline.setSelection(visibleItems.get(view.selected) ? [view.selected] : []);
  select(view.selected);
  writeHash();
}
