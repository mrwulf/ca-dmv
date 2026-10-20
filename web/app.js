"use strict";

const TOPICS = {
  "signs": "Signs & signals",
  "speed": "Speed limits",
  "right-of-way": "Right-of-way",
  "lanes": "Lanes, turns & passing",
  "parking": "Parking",
  "sharing-road": "Sharing the road",
  "safe-driving": "Safe driving",
  "alcohol": "Alcohol & drugs",
  "licenses": "Licenses, insurance & fines",
  "freeway": "Freeways",
  "rail-bus": "Railroads & school buses",
  "emergencies": "Emergencies & crashes",
};
const topicName = (t) => TOPICS[t] || t;

const app = document.getElementById("app");

function h(tag, props, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(props || {})) {
    if (k === "class") el.className = v;
    else if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
    else if (v !== false && v != null) el.setAttribute(k, v === true ? "" : v);
  }
  for (const kid of kids.flat()) if (kid != null) el.append(kid);
  return el;
}

function bar(pct, cls) {
  const fill = h("span");
  fill.style.width = `${Math.max(0, Math.min(100, pct))}%`;
  return h("div", { class: `bar ${cls || ""}`, role: "presentation" }, fill);
}

async function send(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 401) {
    location.href = "/auth/login";
    return new Promise(() => {});
  }
  if (!res.ok) throw new Error(res.status === 404 ? "That question was not found." : `Request failed (${res.status}).`);
  return res.status === 204 ? null : res.json();
}

const api = (path, body) => send(body === undefined ? "GET" : "POST", path, body);

function show(...nodes) {
  app.replaceChildren(...nodes.flat(Infinity).filter((n) => n != null && n !== false));
  window.scrollTo(0, 0);
}

function showError(err) {
  show(h("div", { class: "card stack" },
    h("p", {}, "Something went wrong. ", err && err.message ? err.message : ""),
    h("button", { onclick: () => route() }, "Try again")));
}

const pct = (c, t) => (t ? Math.round((100 * c) / t) : 0);
const fmtDate = (unix) => new Date(unix * 1000).toLocaleDateString(undefined, { month: "short", day: "numeric" });

function sourceLink(label, url) {
  if (!url || !url.startsWith("https://")) return null;
  return h("p", { class: "src" }, "Source: ", h("a", { href: url, target: "_blank", rel: "noopener noreferrer" }, label || url));
}

// flagBox lets the user flag a question for follow-up with an optional note. state is any object
// with flagged and flag_note, and is updated in place so a redraw shows the current value.
function flagBox(qid, state) {
  const note = h("textarea", { rows: "2", maxlength: "500", placeholder: "What seems wrong? (optional)", "aria-label": "Note" });
  note.value = state.flag_note || "";
  const summary = h("summary", {});
  const status = h("span", { class: "muted", role: "status" });
  const label = () => (state.flagged ? "Flagged for follow-up" : "Think this is wrong? Flag it");
  const save = h("button", { class: "secondary" });
  const clear = h("button", { class: "secondary" }, "Remove flag");
  const refresh = () => {
    summary.textContent = label();
    save.textContent = state.flagged ? "Update note" : "Flag this question";
    clear.hidden = !state.flagged;
  };
  save.addEventListener("click", async () => {
    try {
      await send("PUT", `/api/flags/${qid}`, { note: note.value });
      state.flagged = true;
      state.flag_note = note.value.trim();
      status.textContent = "Saved";
    } catch (e) {
      status.textContent = "Could not save";
    }
    refresh();
  });
  clear.addEventListener("click", async () => {
    try {
      await send("DELETE", `/api/flags/${qid}`);
      state.flagged = false;
      state.flag_note = "";
      note.value = "";
      status.textContent = "Removed";
    } catch (e) {
      status.textContent = "Could not remove";
    }
    refresh();
  });
  const copy = h("button", { class: "link" }, "Copy link to this question");
  copy.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(`${location.origin}/#/q/${qid}`);
      status.textContent = "Link copied";
    } catch (e) {
      status.textContent = `${location.origin}/#/q/${qid}`;
    }
  });
  refresh();
  return h("details", { class: "flagbox", open: state.flagged }, summary,
    h("div", { class: "stack" }, note, h("div", { class: "row2" }, save, clear), h("div", { class: "row" }, copy, status)));
}

// ---------- Home ----------

async function home() {
  const [me, stats] = await Promise.all([api("/api/me"), api("/api/stats")]);
  const exams = stats.exams;
  const last5 = exams.slice(-5);
  const avg = last5.length ? Math.round(last5.reduce((s, e) => s + pct(e.correct, e.total), 0) / last5.length) : null;
  const passes = exams.filter((e) => e.passed).length;
  const [quick, full] = me.lengths;

  const start = async (length) => {
    const exam = await api("/api/exams", { length });
    location.hash = `#/exam/${exam.id}`;
  };
  const active = me.active_exam;

  show(
    h("div", { class: "topbar" },
      h("div", {}, h("h1", {}, "CA DMV practice"), h("span", { class: "muted" }, `Hi, ${me.name}`)),
      h("button", { class: "link", onclick: logout }, "Sign out")),
    active && h("div", { class: "card stack" },
      h("strong", {}, "Exam in progress"),
      h("span", { class: "muted" }, `${active.answered} of ${active.total} answered`),
      h("a", { class: "btn", href: `#/exam/${active.id}` }, "Resume")),
    h("div", { class: "card stack" },
      h("button", { onclick: () => start(full).catch(showError) }, `Full practice exam (${full} questions)`),
      h("button", { class: "secondary", onclick: () => start(quick).catch(showError) }, `Quick quiz (${quick} questions)`),
      h("p", { class: "muted center" }, `The DMV pass mark is ${me.pass_percent}%. The number of questions on the real test may differ from this practice exam.`)),
    h("div", { class: "card" },
      h("div", { class: "stats" },
        h("div", {}, h("b", {}, String(exams.length)), h("span", {}, "exams taken")),
        h("div", {}, h("b", {}, avg === null ? "–" : `${avg}%`), h("span", {}, "avg, last 5")),
        h("div", {}, h("b", {}, exams.length ? `${pct(passes, exams.length)}%` : "–"), h("span", {}, "passed"))),
      h("p", {}, h("a", { href: "#/progress" }, "See progress over time →")),
      h("p", {}, h("a", { href: "#/flagged" }, `Flagged questions (${me.flag_count}) →`))),
  );
}

async function logout() {
  await fetch("/auth/logout", { method: "POST" });
  location.href = "/";
}

// ---------- Exam ----------

async function exam(id) {
  let view = await api(`/api/exams/${id}`);
  if (view.finished) {
    location.hash = `#/results/${id}`;
    return;
  }
  const draw = () => {
    const pos = view.questions.findIndex((q) => q.chosen == null);
    if (pos === -1) {
      location.hash = `#/results/${id}`;
      return;
    }
    drawQuestion(view, pos, null, async (choice) => {
      view = await api(`/api/exams/${id}/answer`, { pos, choice });
      drawQuestion(view, pos, view.questions[pos], () => {
        if (view.finished) location.hash = `#/results/${id}`;
        else draw();
      });
    });
  };
  draw();
}

// answered is the question view once the server revealed the answer; null while waiting for a tap.
function drawQuestion(view, pos, answered, onNext) {
  const q = view.questions[pos];
  const done = view.questions.filter((x) => x.chosen != null).length;
  const buttons = q.options.map((text, i) => {
    let cls = "opt";
    if (answered) {
      if (i === answered.correct_index) cls += " right";
      else if (i === answered.chosen) cls += " wrong";
      else cls += " dim";
    }
    return h("button", {
      class: cls, disabled: !!answered,
      onclick: (e) => {
        buttons.forEach((b) => (b.disabled = true));
        onNext(i).catch(showError);
      },
    }, text);
  });
  const right = answered && answered.chosen === answered.correct_index;
  const last = pos === view.total - 1;

  show(
    h("div", { class: "topbar" },
      h("button", { class: "link", onclick: () => (location.hash = "#/") }, "← Exit"),
      h("span", { class: "muted" }, `${view.correct} correct so far`)),
    bar((100 * done) / view.total),
    h("div", { class: "meta" }, h("span", {}, `Question ${pos + 1} of ${view.total}`), h("span", {}, topicName(q.topic))),
    q.image && h("img", { class: "sign", src: `/${q.image}`, alt: q.image_alt || "Road sign or marking" }),
    h("p", { class: "q" }, q.text),
    buttons,
    answered && h("div", { class: `feedback ${right ? "right" : "wrong"}`, role: "status" },
      h("strong", {}, right ? "Correct" : "Not quite"),
      h("span", {}, answered.explanation),
      sourceLink(answered.source_label, answered.source_url)),
    answered && flagBox(q.id, q),
    answered && h("div", { class: "sticky" },
      h("button", { onclick: onNext }, last ? "See results" : "Next question")),
  );
}

// ---------- Results ----------

async function results(id) {
  const view = await api(`/api/exams/${id}`);
  if (!view.finished) {
    location.hash = `#/exam/${id}`;
    return;
  }
  const passed = view.correct >= view.pass_needed;
  const byTopic = {};
  for (const q of view.questions) {
    const t = (byTopic[q.topic] ||= { c: 0, n: 0 });
    t.n++;
    if (q.chosen === q.correct_index) t.c++;
  }
  const missed = view.questions.filter((q) => q.chosen !== q.correct_index);
  const again = async (length) => {
    const e = await api("/api/exams", { length });
    location.hash = `#/exam/${e.id}`;
  };

  show(
    h("div", { class: "topbar" }, h("h1", {}, "Results"), h("a", { href: "#/" }, "Home")),
    h("div", { class: "card center" },
      h("div", { class: `score ${passed ? "pass" : "fail"}` }, `${view.correct} / ${view.total}`),
      h("strong", { class: passed ? "pass" : "fail" }, passed ? "Pass" : "Not yet"),
      h("p", { class: "muted" }, `${pct(view.correct, view.total)}% · ${view.pass_needed} correct needed to pass (80%)`)),
    h("div", { class: "stack" },
      h("button", { onclick: () => again(view.total).catch(showError) }, "Take another one"),
      h("button", { class: "secondary", onclick: () => (location.hash = "#/progress") }, "See progress")),
    h("h2", {}, "By topic"),
    Object.entries(byTopic).sort((a, b) => a[1].c / a[1].n - b[1].c / b[1].n).map(([t, s]) =>
      h("div", { class: "topic" },
        h("div", { class: "row" }, h("span", {}, topicName(t)), h("span", { class: "muted" }, `${s.c}/${s.n}`)),
        bar(pct(s.c, s.n), accuracyClass(pct(s.c, s.n))))),
    missed.length > 0 && h("h2", {}, `Review missed questions (${missed.length})`),
    missed.map((q) =>
      h("details", {},
        h("summary", {}, q.text),
        h("p", {}, h("strong", {}, "You answered: "), q.options[q.chosen]),
        h("p", {}, h("strong", {}, "Correct: "), q.options[q.correct_index]),
        h("p", { class: "muted" }, q.explanation),
        sourceLink(q.source_label, q.source_url),
        h("p", {}, h("a", { href: `#/q/${q.id}` }, "Try this question again")),
        flagBox(q.id, q))),
  );
}

const accuracyClass = (p) => (p >= 80 ? "good" : p >= 65 ? "warn" : "bad");

// ---------- Progress ----------

async function progress() {
  const { exams, topics, pass_percent: passPercent } = await api("/api/stats");
  const recent = exams.slice(-30);

  const nodes = [h("div", { class: "topbar" }, h("h1", {}, "Progress"), h("a", { href: "#/" }, "Home"))];
  if (!exams.length) {
    nodes.push(h("div", { class: "card" }, h("p", {}, "Finish an exam to see your progress here.")));
    show(...nodes);
    return;
  }
  nodes.push(
    h("div", { class: "card" }, h("strong", {}, "Score per exam"), chart(recent, passPercent),
      h("p", { class: "muted" }, `Dashed line is the ${passPercent}% pass mark. Last 30 exams.`)),
    h("h2", {}, "Topics, weakest first"),
    [...topics].sort((a, b) => a.correct / a.total - b.correct / b.total).map((t) =>
      h("div", { class: "topic" },
        h("div", { class: "row" }, h("span", {}, topicName(t.topic)), h("span", { class: "muted" }, `${pct(t.correct, t.total)}% of ${t.total}`)),
        bar(pct(t.correct, t.total), accuracyClass(pct(t.correct, t.total))))),
    h("h2", {}, "Recent exams"),
    h("div", { class: "list" }, [...exams].reverse().slice(0, 20).map((e) =>
      h("a", { href: `#/results/${e.id}` },
        h("span", {}, fmtDate(e.finished_at)),
        h("span", {}, `${e.correct}/${e.total} · ${pct(e.correct, e.total)}%`)))),
  );
  show(...nodes);
}

function chart(exams, passPercent) {
  const W = 320, H = 150, L = 28, R = 8, T = 8, B = 18;
  const NS = "http://www.w3.org/2000/svg";
  const el = (name, attrs, text) => {
    const n = document.createElementNS(NS, name);
    for (const [k, v] of Object.entries(attrs)) n.setAttribute(k, v);
    if (text) n.textContent = text;
    return n;
  };
  const svg = el("svg", { class: "chart", viewBox: `0 0 ${W} ${H}`, role: "img", "aria-label": "Exam scores over time" });
  const y = (p) => T + (H - T - B) * (1 - p / 100);
  const x = (i) => (exams.length === 1 ? (L + W - R) / 2 : L + ((W - L - R) * i) / (exams.length - 1));
  for (const p of [0, 50, 100]) {
    svg.append(el("line", { class: "grid", x1: L, x2: W - R, y1: y(p), y2: y(p) }), el("text", { x: 2, y: y(p) + 3 }, `${p}`));
  }
  svg.append(el("line", { class: "passline", x1: L, x2: W - R, y1: y(passPercent), y2: y(passPercent) }));
  const pts = exams.map((e, i) => [x(i), y(pct(e.correct, e.total)), e]);
  svg.append(el("polyline", { class: "trend", points: pts.map((p) => `${p[0]},${p[1]}`).join(" ") }));
  for (const [px, py, e] of pts) {
    svg.append(el("circle", { class: e.passed ? "dot-pass" : "dot-fail", cx: px, cy: py, r: 3.5 }));
  }
  svg.append(el("text", { x: L, y: H - 4 }, fmtDate(exams[0].finished_at)),
    el("text", { x: W - R, y: H - 4, "text-anchor": "end" }, fmtDate(exams[exams.length - 1].finished_at)));
  return svg;
}

// ---------- Single question (reachable by link) ----------

async function practice(id) {
  const q = await api(`/api/questions/${id}`);
  const draw = (res, picked) => {
    const buttons = q.options.map((text) => {
      let cls = "opt";
      if (res) cls += text === res.correct_answer ? " right" : text === picked ? " wrong" : " dim";
      return h("button", {
        class: cls, disabled: !!res,
        onclick: async () => {
          buttons.forEach((b) => (b.disabled = true));
          try {
            draw(await send("POST", `/api/questions/${id}/check`, { answer: text }), text);
          } catch (e) {
            showError(e);
          }
        },
      }, text);
    });
    show(
      h("div", { class: "topbar" }, h("button", { class: "link", onclick: () => history.back() }, "← Back"), h("a", { href: "#/" }, "Home")),
      h("div", { class: "meta" }, h("span", {}, "Practice one question"), h("span", {}, topicName(q.topic))),
      q.image && h("img", { class: "sign", src: `/${q.image}`, alt: q.image_alt || "Road sign or marking" }),
      h("p", { class: "q" }, q.text),
      buttons,
      res && h("div", { class: `feedback ${res.correct ? "right" : "wrong"}`, role: "status" },
        h("strong", {}, res.correct ? "Correct" : "Not quite"),
        h("span", {}, res.explanation),
        sourceLink(res.source_label, res.source_url)),
      res && h("div", { class: "sticky" }, h("button", { onclick: () => route() }, "Try again")),
      flagBox(q.id, q),
    );
  };
  draw(null, null);
}

// ---------- Flagged questions ----------

async function flagged() {
  const { flags } = await api("/api/flags");
  const list = flags.map((f) => {
    const row = h("div", { class: "card stack" },
      h("strong", {}, f.question),
      f.note && h("p", { class: "muted" }, `Your note: ${f.note}`),
      h("div", { class: "row2" },
        h("a", { class: "btn secondary", href: `#/q/${f.qid}` }, "Practice it"),
        h("button", {
          class: "secondary",
          onclick: async () => {
            await send("DELETE", `/api/flags/${f.qid}`);
            row.remove();
          },
        }, "Remove flag")));
    return row;
  });
  show(
    h("div", { class: "topbar" }, h("h1", {}, "Flagged questions"), h("a", { href: "#/" }, "Home")),
    flags.length ? list : h("div", { class: "card" }, h("p", {}, "Nothing flagged. Use \"Flag it\" under any answer you think is wrong.")),
  );
}

// ---------- Router ----------

function route() {
  const [, page, id] = location.hash.split("/");
  const screens = {
    exam: () => id && exam(id),
    results: () => id && results(id),
    q: () => id && practice(id),
    progress: () => progress(),
    flagged: () => flagged(),
  };
  const run = (screens[page] && screens[page]()) || home();
  run.catch(showError);
}

window.addEventListener("hashchange", route);
route();
