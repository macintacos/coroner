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

  const inZone = (date) => {
    const ms = vis.moment(date).valueOf();
    return vis.moment(ms).utcOffset(spanAt(ms).offset / 60);
  };

  const fractionDigits = { millisecond: 3, microsecond: 6, nanosecond: 9 };

  // when returns an instant at the event's precision in the export zone, labelled with the
  // zone's abbreviation. Digits below the second come from the text, which keeps all nine.
  const when = (text, event) => {
    const moment = inZone(instant(text));
    if (event.precision === "day") {
      return vis.moment(moment.valueOf()).utcOffset((event.occurred_offset ?? 0) / 60).format("YYYY-MM-DD");
    }
    let shown = moment.format(["hour", "minute"].includes(event.precision) ? "YYYY-MM-DD HH:mm" : "YYYY-MM-DD HH:mm:ss");
    const digits = fractionDigits[event.precision];
    if (digits) shown += "." + text.match(/\.(\d+)/)[1].slice(0, digits);
    return shown + " " + spanAt(moment.valueOf()).abbreviation;
  };

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
      const item = element("li", undefined, annotation.kind);
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
    let heading = when(event.occurred_at, event);
    if (event.ended_at !== null) heading += " to " + when(event.ended_at, event);
    if (event.approximate) heading = "About " + heading;
    const nodes = [element("h2", heading), element("p", event.text, "text")];

    const fields = element("dl");
    if (event.seq !== null) field(fields, "Event", "#" + event.seq);
    field(fields, "Source", event.source ?? "none");
    field(fields, "Tags", event.tags.length ? event.tags.map((tag) => "#" + tag).join(" ") : "none");
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

  const keys = [];
  for (const event of data.events) {
    const key = event.source ?? "";
    if (!keys.includes(key)) keys.push(key);
  }
  keys.sort((a, b) => (a === "") - (b === ""));
  // Ids start at 1: vis-data treats a falsy id as missing.
  const groups = keys.map((key, i) => ({ id: i + 1, order: i, content: element("span", key === "" ? "(no source)" : key) }));

  const items = data.events.map((event, i) => {
    const item = {
      id: i + 1,
      group: keys.indexOf(event.source ?? "") + 1,
      start: instant(event.occurred_at),
      content: element("span", (event.approximate ? "~" : "") + firstLine(event.text)),
      type: event.ended_at === null ? "point" : "range",
    };
    if (event.ended_at !== null) item.end = instant(event.ended_at);
    return item;
  });

  const timeline = new vis.Timeline(document.getElementById("timeline"), items, groups, {
    moment: inZone,
    groupOrder: "order",
    showTooltips: false,
  });
  timeline.on("select", ({ items: selected }) => show(selected.length ? data.events[selected[0] - 1] : null));
  show(null);
}
