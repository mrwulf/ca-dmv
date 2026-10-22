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

// Sign images were once cached for 24 hours under fixed names. The server now revalidates them with
// ETags, but this query string makes every device fetch the revised artwork once, right away.
const SIGNS_REV = "2";
const signSrc = (image) => `/${image}?v=${SIGNS_REV}`;

// The picture shown with a question. Small raster pictures, such as the handbook's 60 px signal faces,
// are shown at about twice their size instead of being stretched to the full width.
function signImg(image, alt) {
  return h("img", {
    class: "sign", src: signSrc(image), alt: alt || "Road sign or marking",
    onload: (e) => {
      const img = e.target;
      if (/\.(gif|png|jpe?g)\?/.test(img.src) && img.naturalWidth < 110) img.style.width = `${Math.min(img.naturalWidth * 2.2, 220)}px`;
    },
  });
}

// A handbook illustration shown with the explanation, after the question is answered.
function explainFigure(src, alt) {
  if (!src) return null;
  return h("img", { class: "explain", src: signSrc(src), alt: alt || "Handbook illustration", loading: "lazy" });
}

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

// A new release changes the X-App-Build header the server sends. When it no longer matches the
// build this page was loaded from, reload to pick up the new code. The reload is rate limited so
// mixed old and new replicas during a rollout cannot cause a loop.
const LOADED_BUILD = document.querySelector('meta[name="app-build"]')?.content || "";

function reloadIfStale(res) {
  const build = res.headers.get("X-App-Build");
  if (!LOADED_BUILD || !build || build === LOADED_BUILD) return;
  try {
    const last = Number(sessionStorage.getItem("reloadedAt")) || 0;
    if (Date.now() - last < 30000) return;
    sessionStorage.setItem("reloadedAt", String(Date.now()));
  } catch (e) {
    return; // Without storage there is no way to rate limit, so do not risk a loop.
  }
  location.reload();
}

// Returning to a tab that sat open through a release should not keep running the old code.
document.addEventListener("visibilitychange", () => {
  if (document.visibilityState !== "visible") return;
  fetch("/", { method: "HEAD", cache: "no-store" }).then(reloadIfStale).catch(() => {});
});

async function send(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  reloadIfStale(res);
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
const fmtDateTime = (unix) =>
  new Date(unix * 1000).toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });

// A burst of confetti on a canvas over the page. It starts with pieces already on screen so it
// reads as instant, keeps raining for a few seconds, then fades. Skipped for people who prefer
// reduced motion.
function confetti(count) {
  if (matchMedia("(prefers-reduced-motion: reduce)").matches) return;
  const canvas = h("canvas", { class: "confetti", "aria-hidden": "true" });
  document.body.append(canvas);
  const dpr = window.devicePixelRatio || 1;
  canvas.width = innerWidth * dpr;
  canvas.height = innerHeight * dpr;
  const ctx = canvas.getContext("2d");
  const colors = ["#f59e0b", "#ef4444", "#10b981", "#3b82f6", "#a855f7", "#ec4899"];
  const RAIN_MS = 4500; // pieces that fall off the bottom restart at the top until then
  const TOTAL_MS = 7000;
  const place = (b, onScreen) => Object.assign(b, {
    x: Math.random() * innerWidth,
    y: onScreen ? Math.random() * innerHeight * 0.5 : -20 - Math.random() * 80,
    vx: (Math.random() - 0.5) * 5, vy: 1.5 + Math.random() * 3,
    w: 6 + Math.random() * 6, h: 8 + Math.random() * 8,
    r: Math.random() * Math.PI, vr: (Math.random() - 0.5) * 0.3,
    color: colors[Math.floor(Math.random() * colors.length)],
  });
  const bits = Array.from({ length: count }, () => place({}, true));
  const start = performance.now();
  let last = start;
  const frame = (now) => {
    const t = now - start;
    const k = Math.min(3, (now - last) / 16.67); // keep the same speed at 60 and 120 Hz
    last = now;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, innerWidth, innerHeight);
    for (const b of bits) {
      b.x += b.vx * k;
      b.y += b.vy * k;
      b.vy = Math.min(b.vy + 0.05 * k, 4.5);
      b.r += b.vr * k;
      if (b.y > innerHeight + 20 && t < RAIN_MS) place(b, false);
      ctx.save();
      ctx.translate(b.x, b.y);
      ctx.rotate(b.r);
      ctx.globalAlpha = Math.max(0, Math.min(1, (TOTAL_MS - t) / 1200));
      ctx.fillStyle = b.color;
      ctx.fillRect(-b.w / 2, -b.h / 2, b.w, b.h);
      ctx.restore();
    }
    if (t < TOTAL_MS) requestAnimationFrame(frame);
    else canvas.remove();
  };
  requestAnimationFrame(frame);
}

// Celebrate the moment a finished test's results are opened: more for a perfect score.
function celebrate(view) {
  if (view.correct === view.total) confetti(220);
  else if (view.correct >= view.pass_needed) confetti(100);
}

// ---------- Practice streaks and achievements (days are the user's local calendar days) ----------

const dayKey = (d) => `${d.getFullYear()}-${d.getMonth() + 1}-${d.getDate()}`;
const noon = (d) => {
  const x = new Date(d);
  x.setHours(12, 0, 0, 0); // noon keeps day arithmetic safe across daylight saving changes
  return x;
};
const addDays = (d, n) => {
  const x = noon(d);
  x.setDate(x.getDate() + n);
  return x;
};
const practiceDays = (exams) => new Set(exams.map((e) => dayKey(new Date(e.finished_at * 1000))));

// streakEnding counts consecutive practiced days going back from day.
function streakEnding(days, day) {
  let n = 0;
  for (let d = noon(day); days.has(dayKey(d)); d = addDays(d, -1)) n++;
  return n;
}

function longestStreak(exams) {
  const unique = [...new Map(exams.map((e) => {
    const d = noon(new Date(e.finished_at * 1000));
    return [dayKey(d), d];
  })).values()].sort((a, b) => a - b);
  let best = 0, run = 0, prev = null;
  for (const d of unique) {
    run = prev && dayKey(addDays(prev, 1)) === dayKey(d) ? run + 1 : 1;
    best = Math.max(best, run);
    prev = d;
  }
  return best;
}

// The home card: the current streak, a hint, and the last seven days. The streak stays alive until
// the end of today, so a missing test today does not break it yet.
function streakCard(exams) {
  const days = practiceDays(exams);
  const today = new Date();
  const doneToday = days.has(dayKey(today));
  const n = streakEnding(days, doneToday ? today : addDays(today, -1));
  const best = longestStreak(exams);
  const hint = doneToday ? "You practiced today. Come back tomorrow to keep it going."
    : n > 0 ? "Finish a test today to keep it going." : "Finish a test today to begin.";
  const week = Array.from({ length: 7 }, (_, i) => addDays(today, i - 6));
  const practiced = week.filter((d) => days.has(dayKey(d))).length;
  return h("div", { class: "card stack" },
    h("strong", {}, n > 0 ? `🔥 ${n}-day practice streak` : "Start a practice streak"),
    h("div", { class: "week", role: "img", "aria-label": `Practiced on ${practiced} of the last 7 days` },
      week.map((d) => {
        const done = days.has(dayKey(d));
        return h("div", { class: `day${done ? " done" : ""}${dayKey(d) === dayKey(today) ? " today" : ""}` },
          h("span", { class: "dot" }, done ? "✓" : ""),
          h("span", { class: "dow" }, d.toLocaleDateString(undefined, { weekday: "narrow" })));
      })),
    h("p", { class: "muted" }, hint + (best >= 2 && best > n ? ` Longest streak: ${best} days.` : "")));
}

// Achievements for one finished test, as they were when it was taken. exams is the user's finished
// tests oldest first; view.mastery says how this test moved mastery.
function achievements(view, exams) {
  const chips = [];
  const i = exams.findIndex((e) => e.id === view.id);
  if (i >= 0) {
    const e = exams[i];
    const kind = (x) => `${x.total}|${x.topic || ""}`;
    const earlier = exams.slice(0, i).filter((x) => kind(x) === kind(e));
    if (earlier.length >= 2 && pct(e.correct, e.total) > Math.max(...earlier.map((x) => pct(x.correct, x.total)))) {
      chips.push("🏆 New personal best");
    }
    const day = new Date(e.finished_at * 1000);
    const firstToday = !exams.slice(0, i).some((x) => dayKey(new Date(x.finished_at * 1000)) === dayKey(day));
    const n = streakEnding(practiceDays(exams.slice(0, i + 1)), day);
    if (firstToday && n >= 2) chips.push(`🔥 Day ${n} of your practice streak`);
  }
  const m = view.mastery;
  if (m) {
    const hit = [100, 75, 50, 25].find((t) => m.before_pct < t && m.after_pct >= t);
    if (hit) chips.push(hit === 100 ? "🏅 Every question mastered!" : `🏅 ${hit}% of all questions mastered`);
    for (const t of m.new_topics || []) chips.push(`⭐ Mastered every question in ${topicName(t)}`);
  }
  return chips;
}

// Navigation controls share one look: accent text, no underline, a comfortable tap target.
const navLink = (text, href) => h("a", { class: "navlink", href }, text);
const navButton = (text, onclick) => h("button", { class: "navlink", onclick }, text);
const homeLink = () => navLink("Home", "#/");

// questionLinks is a small expander under a question: its handbook source (once the answer is
// revealed) and a link to the question that copies itself when clicked. state needs source_url and
// source_label.
function questionLinks(qid, state, revealed) {
  const url = `${location.origin}/#/q/${qid}`;
  const link = h("a", { href: url }, "Link to this question");
  const status = h("span", {});
  link.addEventListener("click", async (ev) => {
    ev.preventDefault();
    try {
      await navigator.clipboard.writeText(url);
      status.textContent = " (copied)";
    } catch (e) {
      location.hash = `#/q/${qid}`;
    }
  });
  const parts = [];
  if (revealed) {
    parts.push(state.source_url && state.source_url.startsWith("https://")
      ? h("span", {}, "Source: ", h("a", { href: state.source_url, target: "_blank", rel: "noopener noreferrer" }, state.source_label || "Driver Handbook"))
      : h("span", {}, "No handbook source found"));
  }
  parts.push(h("span", {}, link, status));
  return h("details", { class: "linkbox" }, h("summary", {}, "Source and link"),
    h("p", {}, parts.flatMap((x, i) => (i ? [" · ", x] : [x]))));
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
      navButton("Sign out", logout)),
    active && h("div", { class: "card stack" },
      h("strong", {}, "Exam in progress"),
      h("span", { class: "muted" }, `${active.answered} of ${active.total} answered`),
      h("a", { class: "btn", href: `#/exam/${active.id}` }, "Resume")),
    h("div", { class: "card stack" },
      h("button", { onclick: () => start(full).catch(showError) }, `Full practice exam (${full} questions)`),
      h("button", { class: "secondary", onclick: () => start(quick).catch(showError) }, `Quick quiz (${quick} questions)`),
      h("p", { class: "muted center" }, `The DMV pass mark is ${me.pass_percent}%. The number of questions on the real test may differ from this practice exam.`)),
    streakCard(exams),
    h("div", { class: "card stack" },
      h("div", { class: "stats" },
        h("div", {}, h("b", {}, String(exams.length)), h("span", {}, "exams taken")),
        h("div", {}, h("b", {}, avg === null ? "–" : `${avg}%`), h("span", {}, "avg, last 5")),
        h("div", {}, h("b", {}, exams.length ? `${pct(passes, exams.length)}%` : "–"), h("span", {}, "passed"))),
      h("div", { class: "row2" },
        h("a", { class: "btn secondary", href: "#/progress" }, "My stats"),
        h("a", { class: "btn secondary", href: "#/leaderboard" }, "Leaderboard"))),
    h("p", { class: "footer muted center" }, [me.version, me.commit].filter(Boolean).join(" · ")),
    h("p", { class: "footer muted center" }, "Handbook illustrations: California Driver's Handbook, California Department of Motor Vehicles, licensed under ",
      h("a", { href: "https://creativecommons.org/licenses/by-nc/4.0/", target: "_blank", rel: "noopener noreferrer" }, "CC BY-NC 4.0"), "."),
  );
}

// The server ends our session and returns the identity provider's logout URL, which has to be
// visited too or the provider would sign the user straight back in.
async function logout() {
  let next = "/";
  try {
    const res = await fetch("/auth/logout", { method: "POST" });
    if (res.ok) next = (await res.json()).redirect || "/";
  } catch (e) {
    // Fall through to the local sign-out page.
  }
  location.href = next;
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
        if (view.finished) {
          celebrate(view);
          location.hash = `#/results/${id}`;
        } else draw();
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
  // Consecutive correct answers ending at the latest answered question.
  let streak = 0;
  for (let i = view.questions.length - 1; i >= 0; i--) {
    const x = view.questions[i];
    if (x.chosen == null) continue;
    if (x.chosen !== x.correct_index) break;
    streak++;
  }

  show(
    h("div", { class: "topbar" },
      navButton("← Exit", () => (location.hash = "#/")),
      h("span", { class: "muted" }, streak >= 3 ? `${view.correct} correct so far · 🔥 ${streak} in a row` : `${view.correct} correct so far`)),
    bar((100 * done) / view.total),
    h("div", { class: "meta" }, h("span", {}, `Question ${pos + 1} of ${view.total}`), h("span", {}, topicName(q.topic))),
    q.image && signImg(q.image, q.image_alt),
    h("p", { class: "q" }, q.text),
    buttons,
    answered && h("div", { class: `feedback ${right ? "right" : "wrong"}`, role: "status" },
      h("strong", {}, right ? "Correct" : "Not quite"),
      h("span", {}, answered.explanation),
      explainFigure(answered.explain_image, answered.explain_image_alt)),
    answered && questionLinks(q.id, q, true),
    answered && h("div", { class: "sticky" },
      h("button", { onclick: onNext }, last ? "See results" : "Next question")),
  );
}

// ---------- Results ----------

async function results(id) {
  const [view, stats] = await Promise.all([api(`/api/exams/${id}`), api("/api/stats")]);
  if (!view.finished) {
    location.hash = `#/exam/${id}`;
    return;
  }
  const passed = view.correct >= view.pass_needed;
  const perfect = view.correct === view.total;
  const byTopic = {};
  for (const q of view.questions) {
    const t = (byTopic[q.topic] ||= { c: 0, n: 0 });
    t.n++;
    if (q.chosen === q.correct_index) t.c++;
  }
  const missed = view.questions.filter((q) => q.chosen !== q.correct_index);
  const chips = achievements(view, stats.exams);
  const again = async (length) => {
    const e = await api("/api/exams", { length });
    location.hash = `#/exam/${e.id}`;
  };

  show(
    h("div", { class: "topbar" }, h("h1", {}, "Results"), homeLink()),
    h("div", { class: "card center" },
      h("div", { class: `score ${passed ? "pass" : "fail"}` }, `${view.correct} / ${view.total}`),
      h("strong", { class: passed ? "pass" : "fail" }, perfect ? "🎉 Perfect score!" : passed ? "🎉 Passed!" : "Not yet"),
      view.finished_at && h("p", { class: "muted" }, fmtDateTime(view.finished_at)),
      view.topic && h("p", { class: "muted" }, `Focused test: ${topicName(view.topic)}`),
      h("p", { class: "muted" }, `${pct(view.correct, view.total)}% · ${view.pass_needed} correct needed to pass (80%)`)),
    chips.length > 0 && h("div", { class: "badges" }, chips.map((c) => h("div", { class: "badge" }, c))),
    h("div", { class: "stack" },
      h("button", { onclick: () => again(view.total).catch(showError) }, "Take another one"),
      h("button", { class: "secondary", onclick: () => (location.hash = "#/progress") }, "My stats")),
    h("h2", {}, "By topic"),
    Object.entries(byTopic).sort((a, b) => a[1].c / a[1].n - b[1].c / b[1].n).map(([t, s]) =>
      h("div", { class: "topic" },
        h("div", { class: "row" },
          h("span", { class: "grow" }, topicName(t)),
          h("span", { class: "muted" }, `${s.c}/${s.n}`),
          s.c < s.n ? h("button", { class: "secondary small", onclick: () => startTopicTest(t).catch(showError) }, "Focused test") : null),
        bar(pct(s.c, s.n), accuracyClass(pct(s.c, s.n))))),
    missed.length > 0 && h("h2", {}, `Review missed questions (${missed.length})`),
    missed.map((q) =>
      h("details", {},
        h("summary", {}, q.text),
        h("p", {}, h("strong", {}, "You answered: "), q.options[q.chosen]),
        h("p", {}, h("strong", {}, "Correct: "), q.options[q.correct_index]),
        h("p", { class: "muted" }, q.explanation),
        explainFigure(q.explain_image, q.explain_image_alt),
        questionLinks(q.id, q, true))),
  );
}

const accuracyClass = (p) => (p >= 80 ? "good" : p >= 65 ? "warn" : "bad");

// ---------- Progress ----------

async function progress() {
  const { exams, mastery, pass_percent: passPercent } = await api("/api/stats");
  const nodes = [h("div", { class: "topbar" }, h("h1", {}, "My stats"), homeLink())];
  if (exams.length) {
    nodes.push(...progressBody({ exams, mastery, passPercent }, (e) => `#/results/${e.id}`));
  } else {
    nodes.push(h("div", { class: "card" }, h("p", {}, "Finish an exam to see your stats here.")));
  }
  show(...nodes);
}

// The chart, mastery and topic sections shared by your own progress page and the leaderboard view.
// linkFor returns the link for an exam row, or nothing to show the row as plain text.
function progressBody({ exams, mastery, passPercent }, linkFor, who) {
  const recent = exams.slice(-30);
  const row = (e) => {
    const kids = [
      h("span", { class: "when" }, e.topic ? `${fmtDateTime(e.finished_at)} · ${topicName(e.topic)}` : fmtDateTime(e.finished_at)),
      h("span", { class: "result" }, `${e.correct}/${e.total} · ${pct(e.correct, e.total)}%`)];
    const href = linkFor(e);
    return href ? h("a", { href }, ...kids) : h("div", { class: "plainrow" }, ...kids);
  };
  return [
    h("div", { class: "card" }, h("strong", {}, "Score per exam"), chart(recent, passPercent),
      h("p", { class: "muted" }, `Dashed line is the ${passPercent}% pass mark. Last 30 exams.`)),
    h("h2", {}, "Mastery across all questions"),
    masteryBar(mastery, who),
    h("h2", {}, "Topics, weakest first"),
    [...mastery.topics].sort((a, b) => a.mastered / a.total - b.mastered / b.total).map((t) => {
      const p = pct(t.mastered, t.total);
      return h("div", { class: "topic" },
        h("div", { class: "row" }, h("span", {}, t.mastered === t.total ? `${topicName(t.topic)} ⭐` : topicName(t.topic)), h("span", { class: "muted" }, `${t.mastered} of ${t.total} mastered`)),
        bar(p, accuracyClass(p)));
    }),
    h("h2", {}, "Recent exams"),
    h("div", { class: "list" }, [...exams].reverse().slice(0, 20).map(row)),
  ];
}

// ---------- Leaderboard ----------

const BOARD_SORTS = {
  avg: ["Avg score", (a, b) => b.avg_pct - a.avg_pct || b.mastery_pct - a.mastery_pct],
  mastery: ["Mastery", (a, b) => b.mastery_pct - a.mastery_pct || b.mastered - a.mastered],
  tests: ["Tests", (a, b) => b.tests - a.tests],
  recent: ["Last practiced", (a, b) => b.last_at - a.last_at],
  name: ["Name", (a, b) => a.name.localeCompare(b.name)],
};

async function leaderboard(sort) {
  const { users } = await api("/api/leaderboard");
  const key = BOARD_SORTS[sort] ? sort : "avg";
  const rows = [...users].sort((a, b) => BOARD_SORTS[key][1](a, b) || a.name.localeCompare(b.name));
  show(
    h("div", { class: "topbar" }, h("h1", {}, "Leaderboard"), homeLink()),
    h("div", { class: "sorts" }, Object.entries(BOARD_SORTS).map(([k, [label]]) =>
      h("a", { class: k === key ? "chip on" : "chip", href: `#/leaderboard/sort/${k}`, "aria-current": k === key ? "true" : null }, label))),
    rows.length ? h("div", { class: "list" }, rows.map((u, i) =>
      h("a", { href: `#/leaderboard/${u.ref}` },
        h("span", { class: "when" }, `${i + 1}. ${u.name}${u.you ? " (you)" : ""}`,
          h("span", { class: "muted sub" }, `${u.tests} ${u.tests === 1 ? "test" : "tests"} · last ${fmtDate(u.last_at)}`)),
        h("span", { class: "result" }, `${u.avg_pct}% avg`,
          h("span", { class: "muted sub" }, `${u.mastery_pct}% mastered`))))) :
      h("div", { class: "card" }, h("p", {}, "Nobody has finished a test yet.")),
  );
}

async function leaderboardUser(ref) {
  const d = await api(`/api/leaderboard/${encodeURIComponent(ref)}`);
  const title = d.you ? `${d.name} (you)` : d.name;
  show(
    h("div", { class: "topbar" }, h("h1", {}, title), navLink("← Leaderboard", "#/leaderboard")),
    ...progressBody({ exams: d.exams, mastery: d.mastery, passPercent: d.pass_percent }, () => null, d.you ? "you" : "they"),
  );
}

// The question's category, with a way to practice that whole category instead of just this question.
async function startTopicTest(topic) {
  const exam = await api("/api/exams", { topic });
  location.hash = `#/exam/${exam.id}`;
}

function categoryBlock(topic) {
  return h("div", { class: "stack" },
    h("p", { class: "muted" }, `Category: ${topicName(topic)}`),
    h("button", { class: "secondary", onclick: () => startTopicTest(topic).catch(showError) }, `Take a focused test on ${topicName(topic)}`));
}

// A question counts as mastered when it was answered right first time, or 3 times in a row after a miss.
function masteryBar(m, who = "you") {
  const p = pct(m.mastered, m.total);
  return h("div", { class: "card" },
    h("div", { class: "topic" },
      h("div", { class: "row" }, h("strong", {}, `${p}% mastered`), h("span", { class: "muted" }, `${m.mastered} of ${m.total} questions`)),
      bar(p, accuracyClass(p))),
    h("p", { class: "muted" }, `${m.missed} still ${who === "you" ? "need" : "needs"} work after a miss, ${m.unseen} not seen yet.`),
    h("p", { class: "muted" }, `⭐ ${m.topics.filter((t) => t.mastered === t.total).length} of ${m.topics.length} topics fully mastered.`));
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

// ---------- Signed out (shown without calling the API, which would send the user to sign in) ----------

async function signedOut() {
  show(
    h("div", { class: "card stack center" },
      h("h1", {}, "Signed out"),
      h("p", { class: "muted" }, "You are signed out of CA DMV practice. Your other apps stay signed in."),
      h("a", { class: "btn", href: "/auth/login?prompt=select_account" }, "Sign in again"),
      h("p", { class: "muted" }, "You can pick a different account when you sign in.")),
  );
}

// ---------- Delete a test (not linked from anywhere; add /delete to a results URL) ----------

const CONFIRM_WORD = "DELETE";

async function deleteTest(id) {
  const view = await api(`/api/exams/${id}`);
  const input = h("input", {
    type: "text", autocomplete: "off", autocapitalize: "characters", spellcheck: "false",
    "aria-label": `Type ${CONFIRM_WORD} to confirm`, placeholder: CONFIRM_WORD,
  });
  const go = h("button", { class: "danger", disabled: true }, "Delete this test permanently");
  const status = h("p", { class: "muted", role: "status" });
  input.addEventListener("input", () => (go.disabled = input.value.trim() !== CONFIRM_WORD));
  go.addEventListener("click", async () => {
    go.disabled = true;
    try {
      await send("DELETE", `/api/exams/${id}`);
      location.hash = "#/progress";
    } catch (e) {
      status.textContent = "Could not delete this test. Try again.";
      go.disabled = false;
    }
  });
  show(
    h("div", { class: "topbar" }, h("h1", {}, "Delete this test?"), navLink("Cancel", `#/results/${id}`)),
    h("div", { class: "card danger-card stack", role: "alert" },
      h("strong", {}, "Warning: this removes the test and everything counted from it."),
      h("p", {}, `This is the ${view.correct} of ${view.total} test${view.finished_at ? ` from ${fmtDateTime(view.finished_at)}` : ""}.`),
      h("p", {}, "Its score disappears from your progress graph and its answers no longer count toward your topic accuracy, your mastery, or your pass rate."),
      h("p", {}, "You cannot undo this from the app."),
    ),
    h("div", { class: "card stack" },
      h("label", { for: "confirm" }, `To confirm, type ${CONFIRM_WORD} below.`),
      input, go, status,
      h("a", { class: "btn secondary", href: `#/results/${id}` }, "No, keep this test")),
  );
  input.id = "confirm";
}

// ---------- Single question (reachable by link) ----------

async function practice(id) {
  const q = await api(`/api/questions/${id}`);
  const draw = (res, picked) => {
    if (res) {
      q.source_url = res.source_url;
      q.source_label = res.source_label;
    }
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
      h("div", { class: "topbar" }, navButton("← Back", () => history.back()), homeLink()),
      h("div", { class: "meta" }, h("span", {}, "Practice one question"), h("span", {}, topicName(q.topic))),
      q.image && signImg(q.image, q.image_alt),
      h("p", { class: "q" }, q.text),
      buttons,
      res && h("div", { class: `feedback ${res.correct ? "right" : "wrong"}`, role: "status" },
        h("strong", {}, res.correct ? "Correct" : "Not quite"),
        h("span", {}, res.explanation),
        explainFigure(res.explain_image, res.explain_image_alt)),
      res && categoryBlock(q.topic),
      questionLinks(q.id, q, !!res),
    );
  };
  draw(null, null);
}

// ---------- Router ----------

function route() {
  const [, page, id, action] = location.hash.split("/");
  const screens = {
    exam: () => id && exam(id),
    results: () => id && (action === "delete" ? deleteTest(id) : results(id)),
    q: () => id && practice(id),
    "signed-out": () => signedOut(),
    progress: () => progress(),
    leaderboard: () => (id === "sort" ? leaderboard(action) : id ? leaderboardUser(id) : leaderboard()),
  };
  const run = (screens[page] && screens[page]()) || home();
  run.catch(showError);
}

window.addEventListener("hashchange", route);
route();
