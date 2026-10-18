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

  // A theme the toggle picked is remembered in this browser; without one the page follows the
  // system, and picking the system's theme again forgets the pick. Storage can throw, on file://
  // in WebKit for one, and then the pick lasts the visit.
  const root = document.documentElement;
  const themeToggle = document.getElementById("theme");
  const systemDark = matchMedia("(prefers-color-scheme: dark)");
  const systemTheme = () => (systemDark.matches ? "dark" : "light");
  const showTheme = (theme) => {
    if (theme === "light" || theme === "dark") root.dataset.theme = theme;
    themeToggle.setAttribute("aria-pressed", String((root.dataset.theme ?? systemTheme()) === "dark"));
  };
  try {
    showTheme(localStorage.getItem("coroner-theme"));
  } catch {
    showTheme(null);
  }
  systemDark.addEventListener("change", () => showTheme(null));
  themeToggle.addEventListener("click", () => {
    const theme = themeToggle.getAttribute("aria-pressed") === "true" ? "light" : "dark";
    const followsSystem = theme === systemTheme();
    if (followsSystem) delete root.dataset.theme;
    showTheme(followsSystem ? null : theme);
    try {
      if (followsSystem) localStorage.removeItem("coroner-theme");
      else localStorage.setItem("coroner-theme", theme);
    } catch {}
  });

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

  // whenParts returns an instant at precision in the export zone: its date and, but for a day,
  // its clock time and the zone's abbreviation and offset then. An hour shows its minutes when
  // the zone puts it off the hour. Digits below the second come from the timestamp, which keeps
  // all nine.
  const whenParts = (timestamp, precision) => {
    const moment = inZone(instant(timestamp));
    const date = moment.format("YYYY-MM-DD");
    if (precision === "day") return { date };
    let clock = moment.format(precision === "hour" && moment.minute() !== 0 ? "HH:mm" : clockLayouts[precision] ?? "HH:mm:ss");
    const digits = fractionDigits[precision];
    if (digits) clock += "." + timestamp.match(/\.(\d+)/)[1].slice(0, digits);
    return { date, clock, abbreviation: spanAt(moment.valueOf()).abbreviation, offset: moment.format("Z") };
  };

  const shownWhen = ({ date, clock, abbreviation }) => (clock === undefined ? date : date + " " + clock + " " + abbreviation);

  const when = (timestamp, precision) => shownWhen(whenParts(timestamp, precision));

  // startedParts returns whenParts of an event's start; a day-precision event's date is the one
  // it was recorded on, in its own offset.
  const startedParts = (event) =>
    event.precision === "day"
      ? { date: atOffset(instant(event.occurred_at).valueOf(), event.occurred_offset ?? 0).format("YYYY-MM-DD") }
      : whenParts(event.occurred_at, event.precision);

  const started = (event) => shownWhen(startedParts(event));

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

  // printDays splits events, in export order, by the date each starts on, with day-precision
  // events first within their day, as days in internal/export/markdown.go does.
  const printDays = (events) => {
    const rows = events.map((event) => ({
      event,
      start: startedParts(event),
      end: event.ended_at === null ? null : whenParts(event.ended_at, event.precision),
    }));
    const timed = (row) => Number(row.event.precision !== "day");
    rows.sort((a, b) => (a.start.date > b.start.date) - (a.start.date < b.start.date) || timed(a) - timed(b));
    const days = [];
    for (const row of rows) {
      if (row.start.date !== days.at(-1)?.[0].start.date) days.push([]);
      days.at(-1).push(row);
    }
    return days;
  };

  // dayZone returns the abbreviations a day's times are shown in, in order, joined by /, and
  // whether they span more than one offset, so that every time on the day needs its own; as
  // dayZone in internal/export/markdown.go does.
  const dayZone = (day) => {
    const times = day.filter((row) => row.event.precision !== "day").flatMap((row) => (row.end ? [row.start, row.end] : [row.start]));
    if (!times.length) return { label: spanAt(instant(day[0].event.occurred_at).valueOf()).abbreviation, offsets: false };
    return {
      label: [...new Set(times.map((parts) => parts.abbreviation))].join("/"),
      offsets: new Set(times.map((parts) => parts.offset)).size > 1,
    };
  };

  // printTime returns when an event happened as a printed day lists it, under a heading that
  // dates it, as when and dayWhen in internal/export/markdown.go do.
  const printTime = ({ event, start, end }, offsets) => {
    if (event.precision === "day") {
      return [event.approximate && "approximately", end && "until " + end.date].filter(Boolean).join(", ");
    }
    const clock = (parts) => (offsets ? parts.clock + " " + parts.offset : parts.clock);
    let time = clock(start);
    if (end) time += "–" + (end.date === start.date ? "" : end.date + " ") + clock(end);
    return event.approximate ? "~" + time : time;
  };

  const printedEvent = (row, offsets) => {
    const { event } = row;
    const item = element("li");
    item.append(element("span", printTime(row, offsets), "time"));
    if (event.source !== null) item.append(element("span", event.source, "source"));
    item.append(element("p", event.text, "text"));
    if (event.tags.length) item.append(element("p", event.tags.map((tag) => "#" + tag).join(" "), "tags"));
    if (annotationsOn.has(event.id)) item.append(annotationList(annotationsOn.get(event.id)));
    return item;
  };

  // printList returns the page as it prints: the annotations on the whole timeline, then
  // events a day at a time, as the Markdown export lists them.
  const printList = (events) => {
    const nodes = [];
    if (onTimeline.length) nodes.push(element("h2", "On the whole timeline"), annotationList(onTimeline));
    const days = printDays(events);
    for (const day of days) {
      const { label, offsets } = dayZone(day);
      const list = element("ol", undefined, "events");
      list.append(...day.map((row) => printedEvent(row, offsets)));
      nodes.push(element("h2", day[0].start.date + " (" + label + ")"), list);
    }
    if (!days.length) nodes.push(element("p", data.events.length ? "No events match the filters." : "This export has no events.", "hint"));
    return nodes;
  };

  const timelineDetail = () => {
    const hint = data.events.length ? "Select an event to see its full text and annotations." : "This export has no events.";
    const nodes = [element("p", hint, "hint")];
    if (onTimeline.length) nodes.push(element("h3", "On the whole timeline"), annotationList(onTimeline));
    return nodes;
  };

  const foldASCII = (text) => text.replace(/[A-Z]/g, (letter) => letter.toLowerCase());

  // keeps selects what eventWhere in internal/store/event_filter.go does; keep them in step:
  // any checked source, every checked tag, and text containing q folding ASCII only
  // (SQLite's lower()).
  const keeps = (event, filter) => {
    if (filter.sources.size && !filter.sources.has(event.source)) return false;
    if (![...filter.tags].every((tag) => event.tags.includes(tag))) return false;
    if (filter.q.trim() && !foldASCII(event.text).includes(foldASCII(filter.q))) return false;
    const at = instant(event.occurred_at).valueOf();
    return (filter.since === null || at >= filter.since) && (filter.until === null || at < filter.until);
  };

  // The time inputs hold wall-clock times in the export zone, as the axis shows. The second
  // lookup corrects a first guess that lands across a transition; a wall time the zone skips
  // or repeats resolves to one of its two offsets, so showBounds rewrites the input. No time,
  // or a year outside the 0000-9999 store.EventFilter accepts, is null.
  const wallToInstant = (value) => {
    const wall = Date.parse(value + "Z");
    const ms = wall - spanAt(wall - spanAt(wall).offset * 1000).offset * 1000;
    const year = new Date(ms).getUTCFullYear();
    return year >= 0 && year <= 9999 ? ms : null;
  };

  const instantToWall = (ms) => inZone(ms).format("YYYY-MM-DDTHH:mm:ss");

  // A bound is compared in whole milliseconds, which is exact against event list only when it
  // has no digits past the millisecond, so any past it must be zeros.
  const utcToMillisecond = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,3}0{0,6})?Z$/;

  const ordered = (filter) =>
    filter.since !== null && filter.until !== null && filter.until <= filter.since ? { ...filter, until: null } : filter;

  // readHash returns the filter and selection a location hash names, dropping any value this
  // export cannot show: a source, tag or event it lacks, an instant that is not a real UTC
  // time to the millisecond, or an until not after since.
  const readHash = (hash, known) => {
    const params = new URLSearchParams(hash.slice(1));
    const bound = (key) => {
      const text = params.get(key);
      if (!utcToMillisecond.test(text)) return null;
      const ms = instant(text).valueOf();
      // Date turns 13-01 into NaN and rolls 02-30 over to 03-02.
      return Number.isFinite(ms) && new Date(ms).toISOString().startsWith(text.slice(0, 19)) ? ms : null;
    };
    const filter = ordered({
      sources: new Set(params.getAll("source").filter((source) => known.sources.includes(source))),
      tags: new Set(params.getAll("tag").filter((tag) => known.tags.includes(tag))),
      q: params.get("q") ?? "",
      since: bound("since"),
      until: bound("until"),
    });
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
    // Colons are legal in a fragment; keeping them leaves instants and phase:detect tags readable.
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

  const svgElement = (tag, attributes) => {
    const node = document.createElementNS("http://www.w3.org/2000/svg", tag);
    for (const [name, value] of Object.entries(attributes)) node.setAttribute(name, value);
    return node;
  };

  const transparent = (color) => color === "transparent" || /^rgba\(.*,\s*0\)$/.test(color);

  // paintBox draws background and borders; it uses the top-left radius for every corner and
  // drops box shadows.
  const paintBox = (parent, style, x, y, width, height) => {
    const sides = ["Top", "Right", "Bottom", "Left"].map((side) => ({
      width: parseFloat(style["border" + side + "Width"]),
      color: style["border" + side + "Color"],
    }));
    const uniform = sides.every((side) => side.width === sides[0].width && side.color === sides[0].color);
    const stroke = uniform ? sides[0].width : 0;
    const fill = transparent(style.backgroundColor) ? "none" : style.backgroundColor;
    if (fill !== "none" || stroke) {
      parent.append(svgElement("rect", {
        x: x + stroke / 2, y: y + stroke / 2,
        width: Math.max(width - stroke, 0), height: Math.max(height - stroke, 0),
        rx: Math.min(parseFloat(style.borderTopLeftRadius) || 0, width / 2, height / 2),
        fill, stroke: stroke ? sides[0].color : "none", "stroke-width": stroke,
      }));
    }
    if (uniform) return;
    const [top, right, bottom, left] = sides;
    const edges = [
      [top, x, y, width, top.width], [right, x + width - right.width, y, right.width, height],
      [bottom, x, y + height - bottom.width, width, bottom.width], [left, x, y, left.width, height],
    ];
    for (const [side, edgeX, edgeY, edgeWidth, edgeHeight] of edges) {
      if (side.width) parent.append(svgElement("rect", { x: edgeX, y: edgeY, width: edgeWidth, height: edgeHeight, fill: side.color }));
    }
  };

  // redraw copies node's visible boxes and text into parent, with origin the page point drawn
  // at the SVG's top left. A text node is drawn as one line at its first line box; children
  // of an element that hides its overflow are clipped to it.
  const redraw = (node, parent, origin, defs) => {
    if (node.nodeType === Node.TEXT_NODE) {
      // XML 1.0 has no C0 control but tab, LF and CR; one makes the SVG image fail to decode.
      const text = node.textContent
        .replace(/[\u0000-\u0008\u000B\u000C\u000E-\u001F\uFFFE\uFFFF]/g, "\uFFFD")
        .replace(/\s+/g, " ")
        .trim();
      const range = document.createRange();
      range.selectNodeContents(node);
      const line = range.getClientRects()[0];
      const style = getComputedStyle(node.parentElement);
      if (!text || !line || style.visibility === "hidden") return;
      const label = svgElement("text", {
        x: line.left - origin.x, y: line.top + line.height / 2 - origin.y, "dominant-baseline": "central",
        fill: style.color, "font-family": style.fontFamily, "font-size": style.fontSize, "font-weight": style.fontWeight,
      });
      label.textContent = text;
      parent.append(label);
      return;
    }
    if (node.nodeType !== Node.ELEMENT_NODE) return;
    const style = getComputedStyle(node);
    if (style.display === "none") return;
    const box = node.getBoundingClientRect();
    const x = box.left - origin.x;
    const y = box.top - origin.y;
    // A hidden element's children can still be visible.
    if (style.visibility !== "hidden") paintBox(parent, style, x, y, box.width, box.height);
    let group = parent;
    if (style.overflowX !== "visible" || style.overflowY !== "visible") {
      const id = "clip" + defs.childElementCount;
      const clip = svgElement("clipPath", { id });
      clip.append(svgElement("rect", { x, y, width: box.width, height: box.height }));
      defs.append(clip);
      group = svgElement("g", { "clip-path": "url(#" + id + ")" });
      parent.append(group);
    }
    for (const child of node.childNodes) redraw(child, group, origin, defs);
  };

  // pageSVG draws the page heading above the timeline as they are shown, at the screen's
  // pixel density. A foreignObject copy of the page rasterised fully transparent in Chromium,
  // Firefox and WebKit, so boxes and text are redrawn as SVG from the laid-out page.
  const pageSVG = (headingElement, timelineElement) => {
    const pad = 16;
    const headingBox = headingElement.getBoundingClientRect();
    const timelineBox = timelineElement.getBoundingClientRect();
    const width = Math.ceil(timelineBox.width + 2 * pad);
    const height = Math.ceil(headingBox.height + timelineBox.height + 3 * pad);
    const svg = svgElement("svg", {
      width: width * devicePixelRatio, height: height * devicePixelRatio, viewBox: "0 0 " + width + " " + height,
    });
    const defs = svgElement("defs", {});
    svg.append(defs, svgElement("rect", { width, height, fill: getComputedStyle(document.body).backgroundColor }));
    redraw(headingElement, svg, { x: headingBox.left - pad, y: headingBox.top - pad }, defs);
    redraw(timelineElement, svg, { x: timelineBox.left - pad, y: timelineBox.top - headingBox.height - 2 * pad }, defs);
    return svg;
  };

  const png = async (svg) => {
    const image = new Image();
    image.src = "data:image/svg+xml;charset=utf-8," + encodeURIComponent(new XMLSerializer().serializeToString(svg));
    await image.decode();
    const canvas = element("canvas");
    canvas.width = image.naturalWidth;
    canvas.height = image.naturalHeight;
    canvas.getContext("2d").drawImage(image, 0, 0);
    return new Promise((resolve) => canvas.toBlob(resolve, "image/png"));
  };

  const download = (blob, filename) => {
    const url = URL.createObjectURL(blob);
    const anchor = element("a");
    anchor.href = url;
    anchor.download = filename;
    anchor.click();
    // The download starts after click returns, and Safari fails one whose URL is already revoked.
    setTimeout(() => URL.revokeObjectURL(url), 1000);
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
  // A source's colour is its place in the export's first appearances, cycling through the palette.
  const sourceColorCount = Number(document.body.dataset.sourceColors);
  const groups = sources.map((source, i) => ({
    id: i,
    order: i,
    content: element("span", source ?? "(no source)"),
    className: source === null ? "source-none" : "source-" + (i % sourceColorCount),
  }));

  const coarsePrecisions = new Set(["day", "hour", "minute"]);
  const eventsById = new Map(data.events.map((event) => [event.id, event]));
  const items = data.events.map((event) => {
    const item = {
      id: event.id,
      group: groupOf.get(event.source),
      start: instant(event.occurred_at),
      content: element("span", (event.approximate ? "~" : "") + firstLine(event.text)),
      type: event.ended_at === null ? "point" : "range",
      className: [event.approximate && "approximate", coarsePrecisions.has(event.precision) && "coarse"].filter(Boolean).join(" "),
    };
    if (event.ended_at !== null) item.end = instant(event.ended_at);
    return item;
  });

  // gapThreshold is the shortest silence the terminal viewer shows as a gap, and no cluster spans
  // one; keep it in step with gapThreshold in internal/viewer/render.go.
  const gapThreshold = 5 * 60 * 1000;

  // rangeMidpoints holds each row's range midpoints in order, indexed by group id.
  const rangeMidpoints = groups.map(() => []);
  for (const item of items) if (item.end !== undefined) rangeMidpoints[item.group].push((+item.start + +item.end) / 2);
  for (const midpoints of rangeMidpoints) midpoints.sort((a, b) => a - b);

  const rangeMidpointBetween = (group, from, to) => {
    const midpoints = rangeMidpoints[group];
    let low = 0;
    let high = midpoints.length;
    while (low < high) {
      const middle = (low + high) >> 1;
      if (midpoints[middle] < Math.min(from, to)) low = middle + 1;
      else high = middle;
    }
    return low < midpoints.length && midpoints[low] <= Math.max(from, to);
  };

  // clusterLabel names a cluster's count and the minutes it spans, on the axis's clock; the
  // cluster splits into its events as the view zooms in.
  const clusterLabel = (cluster) => {
    const [from, to] = [inZone(cluster.min), inZone(cluster.max)];
    const layout = from.format("YYYY-MM-DD") === to.format("YYYY-MM-DD") ? "HH:mm" : "YYYY-MM-DD HH:mm";
    const span = from.format(layout) === to.format(layout) ? from.format(layout) : from.format(layout) + "–" + to.format(layout);
    const label = element("span", undefined, "cluster");
    label.append(element("span", cluster.uiItems.length + " events", "count"), element("span", span, "time"));
    return label;
  };

  // Dense stretches of a row collapse into clusters. vis-timeline sorts a row by center, a
  // range's midpoint, keeps the next items that pass clusterCriteria against the cluster's first,
  // and resumes by a count, not a position, so what it accepts must be contiguous in that order:
  // points under gapThreshold apart with no range's midpoint between them. Ranges stay out: a
  // cluster holding one is drawn as wide as its span, too narrow for its label.
  const options = {
    moment: inZone,
    groupOrder: "order",
    orientation: "top",
    showTooltips: false,
    margin: { item: { horizontal: 4, vertical: 4 } },
    cluster: {
      // Any two events within about 50px, half vis-timeline's clustering window, merge, so no
      // two labels in a row overlap.
      maxItems: 1,
      clusterCriteria: (a, b) =>
        a.end === undefined && b.end === undefined &&
        Math.abs(+b.start - +a.start) < gapThreshold && !rangeMidpointBetween(a.group, +a.start, +b.start),
    },
    template: (item) => (item.isCluster ? clusterLabel(item) : item.content),
  };
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

  const namedSources = sources.filter((source) => source !== null);
  const tags = [...new Set(data.events.flatMap((event) => event.tags))].sort();
  const view = readHash(location.hash, { sources: namedSources, tags, events: eventsById });

  const sourceBoxes = document.getElementById("sources");
  const tagBoxes = document.getElementById("tags");
  const search = document.getElementById("q");
  const since = document.getElementById("since");
  const until = document.getElementById("until");
  checkboxes(sourceBoxes, namedSources, view.filter.sources);
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
  // vis-timeline 8.5.4 first clusters once the timeline has a width, a redraw too late to stack
  // the rows, so clustered events stay hidden until the window moves. Setting the groups again
  // restacks them.
  timeline.once("changed", () => timeline.setGroups(visibleGroups));

  // Typing a search changes the filter with every keystroke. WebKit throws after 100 replaceState
  // calls in 10 seconds, and rebuilding the printed list of thousands of events takes tens of
  // milliseconds, so both wait for a pause.
  const afterPause = (action) => {
    let timer;
    return () => {
      clearTimeout(timer);
      timer = setTimeout(action, 250);
    };
  };
  const writeHash = afterPause(() =>
    history.replaceState(null, "", location.href.split("#")[0] + hashOf(view.filter, view.selected)),
  );
  const select = (id) => {
    view.selected = visibleItems.get(id) ? id : null;
    show(eventsById.get(view.selected) ?? null);
  };
  const printed = document.getElementById("print");
  const showPrintList = () => printed.replaceChildren(...printList(data.events.filter((event) => keeps(event, view.filter))));
  const showPrintListAfterPause = afterPause(showPrintList);
  // A print sent before the pause ends still shows what the filters keep.
  addEventListener("beforeprint", showPrintList);
  const update = (changes) => {
    view.filter = ordered({ ...view.filter, ...changes });
    showBounds();
    visibleItems.refresh();
    visibleGroups.refresh();
    if (view.selected !== null && !visibleItems.get(view.selected)) select(null);
    showPrintListAfterPause();
    writeHash();
  };

  timeline.on("select", ({ items: picked }) => {
    select(picked[0] ?? null);
    writeHash();
  });
  // Panning or zooming is the time brush: the window becomes since and until, to the second.
  // Like event list --since, it hides a range that began before the window and clears a
  // selection outside it.
  timeline.on("rangechanged", ({ start: from, end: to, byUser }) => {
    if (byUser) update({ since: Math.floor(+from / 1000) * 1000, until: Math.ceil(+to / 1000) * 1000 });
  });

  document.getElementById("filters").addEventListener("submit", (event) => event.preventDefault());
  sourceBoxes.addEventListener("change", () => update({ sources: checkedIn(sourceBoxes) }));
  tagBoxes.addEventListener("change", () => update({ tags: checkedIn(tagBoxes) }));
  search.addEventListener("input", () => update({ q: search.value }));
  const moveBound = (changes) => {
    update(changes);
    const shownWindow = timeline.getWindow();
    const from = view.filter.since ?? +shownWindow.start;
    const to = view.filter.until ?? +shownWindow.end;
    if (from < to) timeline.setWindow(from, to);
  };
  since.addEventListener("change", () => moveBound({ since: wallToInstant(since.value) }));
  until.addEventListener("change", () => moveBound({ until: wallToInstant(until.value) }));
  document.getElementById("all-times").addEventListener("click", () => {
    if (allTimesWindow.start !== undefined) timeline.setWindow(allTimesWindow.start, allTimesWindow.end);
    update({ since: null, until: null });
  });
  document.getElementById("export-png").addEventListener("click", async () => {
    const svg = pageSVG(document.getElementById("heading"), document.getElementById("timeline"));
    download(await png(svg), shown.name + ".png");
  });

  if (view.selected !== null) timeline.setSelection(visibleItems.get(view.selected) ? [view.selected] : []);
  select(view.selected);
  showPrintList();
  writeHash();
}
