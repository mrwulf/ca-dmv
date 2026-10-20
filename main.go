package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

//go:embed web
var webFS embed.FS

//go:embed questions/questions.json
var questionsJSON []byte

// Set at build time with -ldflags "-X main.version=... -X main.commit=...". A plain `go build` inside
// a git checkout falls back to the VCS information Go embeds.
var (
	version = "dev"
	commit  string
)

// buildLabel returns the version and a short git SHA for display, for example ("v0.1.1", "3a957de").
func buildLabel(version, commit string) (string, string) {
	if commit == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			dirty := false
			for _, kv := range info.Settings {
				switch kv.Key {
				case "vcs.revision":
					commit = kv.Value
				case "vcs.modified":
					dirty = kv.Value == "true"
				}
			}
			if commit != "" && dirty {
				commit = shortSHA(commit) + "-dirty"
			}
		}
	}
	return version, shortSHA(commit)
}

func shortSHA(s string) string {
	if len(s) > 7 && !strings.Contains(s, "-") {
		return s[:7]
	}
	return s
}

type config struct {
	listen       string
	baseURL      string
	secure       bool
	dbPath       string
	issuer       string
	clientID     string
	clientSecret string
	fullLength   int
	// logoutProvider also ends the identity provider's session on sign-out.
	logoutProvider bool
}

func loadConfig() (config, error) {
	c := config{
		listen:       envOr("LISTEN_ADDR", ":8080"),
		baseURL:      strings.TrimRight(os.Getenv("BASE_URL"), "/"),
		dbPath:       envOr("DB_PATH", "/data/ca-dmv.db"),
		issuer:       os.Getenv("OIDC_ISSUER_URL"),
		clientID:     os.Getenv("OIDC_CLIENT_ID"),
		clientSecret: os.Getenv("OIDC_CLIENT_SECRET"), // optional: leave empty for a public PKCE client
		fullLength:   defaultFullExamLength,

		logoutProvider: os.Getenv("OIDC_LOGOUT_PROVIDER") == "true",
	}
	if v := os.Getenv("FULL_EXAM_LENGTH"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= quickExamLength {
			return c, fmt.Errorf("FULL_EXAM_LENGTH must be a number greater than %d", quickExamLength)
		}
		c.fullLength = n
	}
	for name, v := range map[string]string{"BASE_URL": c.baseURL, "OIDC_ISSUER_URL": c.issuer, "OIDC_CLIENT_ID": c.clientID} {
		if v == "" {
			return c, fmt.Errorf("%s is required", name)
		}
	}
	u, err := url.Parse(c.baseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return c, errors.New("BASE_URL must be an absolute http(s) URL")
	}
	c.secure = u.Scheme == "https"
	return c, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

type server struct {
	cfg  config
	st   *store
	auth *authn
	bank []question
	byID map[string]question
}

func (s *server) lengths() []int { return []int{quickExamLength, s.cfg.fullLength} }

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	bank, err := loadBank(questionsJSON)
	if err != nil {
		return fmt.Errorf("question bank: %w", err)
	}
	st, err := openStore(cfg.dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	if cfg.fullLength > len(bank) {
		return fmt.Errorf("FULL_EXAM_LENGTH %d is more than the %d questions available", cfg.fullLength, len(bank))
	}
	s := &server{cfg: cfg, st: st, auth: &authn{cfg: cfg, st: st}, bank: bank, byID: map[string]question{}}
	for _, q := range bank {
		s.byID[q.ID] = q
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		for {
			if err := st.cleanup(ctx); err != nil && ctx.Err() == nil {
				slog.Error("cleanup", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Hour):
			}
		}
	}()

	srv := &http.Server{
		Addr:              cfg.listen,
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	ver, sha := buildLabel(version, commit)
	slog.Info("listening", "addr", cfg.listen, "questions", len(bank), "version", ver, "commit", sha)
	if err = srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /auth/login", s.auth.login)
	mux.HandleFunc("GET /auth/callback", s.auth.callback)
	mux.Handle("POST /auth/logout", s.sameOrigin(http.HandlerFunc(s.auth.logout)))
	mux.Handle("GET /api/me", s.api(s.me))
	mux.Handle("POST /api/exams", s.api(s.createExam))
	mux.Handle("GET /api/exams/{id}", s.api(s.getExam))
	mux.Handle("DELETE /api/exams/{id}", s.api(s.deleteExam))
	mux.Handle("POST /api/exams/{id}/answer", s.api(s.answer))
	mux.Handle("GET /api/stats", s.api(s.stats))
	mux.Handle("GET /api/leaderboard", s.api(s.leaderboard))
	mux.Handle("GET /api/leaderboard/{ref}", s.api(s.leaderboardUser))
	mux.Handle("GET /api/questions/{id}", s.api(s.getQuestion))
	mux.Handle("POST /api/questions/{id}/check", s.api(s.checkQuestion))
	mux.Handle("GET /api/flags", s.api(s.listFlags))
	mux.Handle("PUT /api/flags/{id}", s.api(s.putFlag))
	mux.Handle("DELETE /api/flags/{id}", s.api(s.deleteFlag))

	web, _ := fs.Sub(webFS, "web")
	build := buildID()
	mux.Handle("/", staticHandler(web, build))
	return securityHeaders(mux, build)
}

// buildID identifies this release. Pages learn the build they were loaded from, and the X-App-Build
// header on later responses tells them when a newer release is being served, so they can reload.
func buildID() string {
	ver, sha := buildLabel(version, commit)
	return ver + "-" + sha
}

func securityHeaders(next http.Handler, build string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-App-Build", build)
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// sameOrigin rejects state-changing requests that did not come from this site.
func (s *server) sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && o != s.cfg.baseURL {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type apiHandler func(w http.ResponseWriter, r *http.Request, u user) error

// api wraps a handler with session auth, the same-origin check for POSTs, a body limit and error mapping.
func (s *server) api(h apiHandler) http.Handler {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		u, err := s.auth.currentUser(r)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		switch err = h(w, r, u); {
		case err == nil:
		case errors.Is(err, errNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		case errors.Is(err, errBadRequest):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		default:
			slog.Error("api", "path", r.URL.Path, "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		}
	})
	return s.sameOriginUnsafe(inner)
}

// sameOriginUnsafe applies the origin check only to non-GET requests.
func (s *server) sameOriginUnsafe(next http.Handler) http.Handler {
	checked := s.sameOrigin(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		checked.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *server) me(w http.ResponseWriter, r *http.Request, u user) error {
	active, err := s.st.activeExam(r.Context(), u.Sub)
	if err != nil {
		return err
	}
	flags, err := s.st.listFlags(r.Context(), u.Sub)
	if err != nil {
		return err
	}
	ver, sha := buildLabel(version, commit)
	writeJSON(w, http.StatusOK, map[string]any{
		"name": u.Name, "active_exam": active, "lengths": s.lengths(),
		"pass_percent": passPercent, "flag_count": len(flags), "version": ver, "commit": sha,
	})
	return nil
}

type sourceView struct {
	URL   string `json:"source_url,omitempty"`
	Label string `json:"source_label,omitempty"`
}

type questionView struct {
	questionPublic
	Pos          int    `json:"pos"`
	Flagged      bool   `json:"flagged"`
	FlagNote     string `json:"flag_note,omitempty"`
	Chosen       *int   `json:"chosen,omitempty"`
	CorrectIndex *int   `json:"correct_index,omitempty"`
	Explanation  string `json:"explanation,omitempty"`
	sourceView
	ExplainImage    string `json:"explain_image,omitempty"`
	ExplainImageAlt string `json:"explain_image_alt,omitempty"`
}

type examView struct {
	ID         string         `json:"id"`
	Total      int            `json:"total"`
	Topic      string         `json:"topic,omitempty"`
	Mastery    *examMastery   `json:"mastery,omitempty"`
	PassNeeded int            `json:"pass_needed"`
	Correct    int            `json:"correct"`
	Finished   bool           `json:"finished"`
	FinishedAt *int64         `json:"finished_at,omitempty"`
	Questions  []questionView `json:"questions"`
}

func publicQuestion(q question) questionPublic {
	return questionPublic{ID: q.ID, Topic: q.Topic, Text: q.Question, Options: q.Options, Image: q.Image, ImageAlt: q.ImageAlt}
}

// viewExam hides the answer and explanation of every question that has not been answered yet.
// notes holds the user's flagged question ids with their notes.
func viewExam(e *exam, notes map[string]string) examView {
	v := examView{ID: e.ID, Topic: e.Topic, Total: e.Total, PassNeeded: passNeeded(e.Total), Correct: e.Correct,
		Finished: e.FinishedAt != nil, FinishedAt: e.FinishedAt}
	for i, q := range e.Questions {
		note, flagged := notes[q.ID]
		qv := questionView{questionPublic: publicQuestion(q), Pos: i, Flagged: flagged, FlagNote: note}
		if a, ok := e.Answers[i]; ok {
			chosen, correct := a.Chosen, q.Answer
			qv.Chosen, qv.CorrectIndex, qv.Explanation = &chosen, &correct, q.Explanation
			qv.sourceView = sourceView{URL: q.SourceURL, Label: q.SourceLabel}
			qv.ExplainImage, qv.ExplainImageAlt = q.ExplainImage, q.ExplainImageAlt
		}
		v.Questions = append(v.Questions, qv)
	}
	return v
}

// respondExam writes the exam view with the user's flags merged in.
func (s *server) respondExam(w http.ResponseWriter, r *http.Request, u user, status int, e *exam) error {
	notes, err := s.st.flagNotes(r.Context(), u.Sub)
	if err != nil {
		return err
	}
	writeJSON(w, status, viewExam(e, notes))
	return nil
}

func (s *server) createExam(w http.ResponseWriter, r *http.Request, u user) error {
	var body struct {
		Length int    `json:"length"`
		Topic  string `json:"topic"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return errBadRequest
	}
	pool, length := s.bank, body.Length
	if body.Topic != "" {
		// A focused test: quick-test length, drawn only from the chosen category.
		pool, length = nil, quickExamLength
		for _, q := range s.bank {
			if q.Topic == body.Topic {
				pool = append(pool, q)
			}
		}
		if len(pool) == 0 {
			return errBadRequest
		}
	} else if !slices.Contains(s.lengths(), length) {
		return errBadRequest
	}
	id, err := s.st.createExam(r.Context(), u.Sub, buildExam(pool, length), body.Topic)
	if err != nil {
		return err
	}
	e, err := s.st.getExam(r.Context(), u.Sub, id)
	if err != nil {
		return err
	}
	return s.respondExam(w, r, u, http.StatusCreated, e)
}

// examMastery is how one finished test moved the user's mastery: the percentage before and after,
// and any category that became fully mastered because of it.
type examMastery struct {
	BeforePct int      `json:"before_pct"`
	AfterPct  int      `json:"after_pct"`
	NewTopics []string `json:"new_topics,omitempty"`
}

func (s *server) examMastery(ctx context.Context, sub, examID string) (*examMastery, error) {
	beforeAnswers, err := s.st.masteryAsOf(ctx, sub, examID, false)
	if err != nil {
		return nil, err
	}
	afterAnswers, err := s.st.masteryAsOf(ctx, sub, examID, true)
	if err != nil {
		return nil, err
	}
	before, after := s.mastery(beforeAnswers), s.mastery(afterAnswers)
	m := &examMastery{BeforePct: before.Mastered * 100 / max(before.Total, 1), AfterPct: after.Mastered * 100 / max(after.Total, 1)}
	was := map[string]bool{}
	for _, t := range before.Topics {
		was[t.Topic] = t.Mastered == t.Total
	}
	for _, t := range after.Topics {
		if t.Mastered == t.Total && !was[t.Topic] {
			m.NewTopics = append(m.NewTopics, t.Topic)
		}
	}
	return m, nil
}

func (s *server) getExam(w http.ResponseWriter, r *http.Request, u user) error {
	e, err := s.st.getExam(r.Context(), u.Sub, r.PathValue("id"))
	if err != nil {
		return err
	}
	notes, err := s.st.flagNotes(r.Context(), u.Sub)
	if err != nil {
		return err
	}
	v := viewExam(e, notes)
	if v.Finished {
		if v.Mastery, err = s.examMastery(r.Context(), u.Sub, e.ID); err != nil {
			return err
		}
	}
	writeJSON(w, http.StatusOK, v)
	return nil
}

func (s *server) answer(w http.ResponseWriter, r *http.Request, u user) error {
	var body struct {
		Pos    int `json:"pos"`
		Choice int `json:"choice"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return errBadRequest
	}
	e, err := s.st.answerQuestion(r.Context(), u.Sub, r.PathValue("id"), body.Pos, body.Choice)
	if err != nil {
		return err
	}
	return s.respondExam(w, r, u, http.StatusOK, e)
}

func (s *server) stats(w http.ResponseWriter, r *http.Request, u user) error {
	exams, topics, err := s.st.stats(r.Context(), u.Sub)
	if err != nil {
		return err
	}
	latest, err := s.st.latestAnswers(r.Context(), u.Sub)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"exams": exams, "topics": topics, "pass_percent": passPercent, "mastery": s.mastery(latest),
	})
	return nil
}

// The leaderboard is an unlinked page that any signed-in user can open. Users are identified in its
// URLs by a hash of their OIDC subject, never the subject itself, and no test ids are ever returned,
// so another user's individual tests cannot be opened.

func boardRef(sub string) string {
	sum := sha256.Sum256([]byte("leaderboard:" + sub))
	return hex.EncodeToString(sum[:8])
}

type boardUser struct {
	Ref        string `json:"ref"`
	Name       string `json:"name"`
	You        bool   `json:"you"`
	MasteryPct int    `json:"mastery_pct"`
	Mastered   int    `json:"mastered"`
	Total      int    `json:"total"`
	Tests      int    `json:"tests"`
	LastAt     int64  `json:"last_at"`
}

func (s *server) leaderboard(w http.ResponseWriter, r *http.Request, u user) error {
	rows, err := s.st.leaderboardRows(r.Context())
	if err != nil {
		return err
	}
	users := []boardUser{}
	for _, row := range rows {
		latest, err := s.st.latestAnswers(r.Context(), row.Sub)
		if err != nil {
			return err
		}
		m := s.mastery(latest)
		users = append(users, boardUser{
			Ref: boardRef(row.Sub), Name: row.Name, You: row.Sub == u.Sub,
			MasteryPct: m.Mastered * 100 / max(m.Total, 1), Mastered: m.Mastered, Total: m.Total,
			Tests: row.Tests, LastAt: row.LastAt,
		})
	}
	slices.SortStableFunc(users, func(a, b boardUser) int { return b.MasteryPct - a.MasteryPct })
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
	return nil
}

// boardExam is one finished test as other users see it: when and how well, with no id.
type boardExam struct {
	FinishedAt int64  `json:"finished_at"`
	Correct    int    `json:"correct"`
	Total      int    `json:"total"`
	Passed     bool   `json:"passed"`
	Topic      string `json:"topic,omitempty"`
}

func (s *server) leaderboardUser(w http.ResponseWriter, r *http.Request, u user) error {
	rows, err := s.st.leaderboardRows(r.Context())
	if err != nil {
		return err
	}
	var target *boardRow
	for i := range rows {
		if boardRef(rows[i].Sub) == r.PathValue("ref") {
			target = &rows[i]
		}
	}
	if target == nil {
		return errNotFound
	}
	exams, topics, err := s.st.stats(r.Context(), target.Sub)
	if err != nil {
		return err
	}
	latest, err := s.st.latestAnswers(r.Context(), target.Sub)
	if err != nil {
		return err
	}
	public := make([]boardExam, 0, len(exams))
	for _, e := range exams {
		public = append(public, boardExam{FinishedAt: e.FinishedAt, Correct: e.Correct, Total: e.Total, Passed: e.Passed, Topic: e.Topic})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": target.Name, "you": target.Sub == u.Sub, "exams": public, "topics": topics,
		"pass_percent": passPercent, "mastery": s.mastery(latest),
	})
	return nil
}

type topicMastery struct {
	Topic    string `json:"topic"`
	Mastered int    `json:"mastered"`
	Total    int    `json:"total"`
}

type masteryView struct {
	Total    int            `json:"total"`
	Mastered int            `json:"mastered"`
	Missed   int            `json:"missed"`
	Unseen   int            `json:"unseen"`
	Topics   []topicMastery `json:"topics"`
}

// mastery counts a question as mastered when the user's latest answer to it was correct, as missed
// when it was wrong, and as unseen when they have not answered it. Questions no longer in the bank
// are ignored.
func (s *server) mastery(latest map[string]bool) masteryView {
	v := masteryView{Total: len(s.bank)}
	perTopic := map[string]*topicMastery{}
	for _, q := range s.bank {
		t, ok := perTopic[q.Topic]
		if !ok {
			t = &topicMastery{Topic: q.Topic}
			perTopic[q.Topic] = t
		}
		t.Total++
	}
	for qid, ok := range latest {
		q, inBank := s.byID[qid]
		if !inBank {
			continue
		}
		if ok {
			v.Mastered++
			perTopic[q.Topic].Mastered++
		} else {
			v.Missed++
		}
	}
	v.Unseen = v.Total - v.Mastered - v.Missed
	for _, t := range perTopic {
		v.Topics = append(v.Topics, *t)
	}
	slices.SortFunc(v.Topics, func(a, b topicMastery) int { return strings.Compare(a.Topic, b.Topic) })
	return v
}

func (s *server) deleteExam(w http.ResponseWriter, r *http.Request, u user) error {
	if err := s.st.deleteExam(r.Context(), u.Sub, r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Single-question practice, reachable by link (#/q/<id>). Nothing here is recorded in exam stats.

func (s *server) getQuestion(w http.ResponseWriter, r *http.Request, u user) error {
	q, ok := s.byID[r.PathValue("id")]
	if !ok {
		return errNotFound
	}
	notes, err := s.st.flagNotes(r.Context(), u.Sub)
	if err != nil {
		return err
	}
	note, flagged := notes[q.ID]
	writeJSON(w, http.StatusOK, questionView{questionPublic: publicQuestion(shuffledCopy(q)), Flagged: flagged, FlagNote: note})
	return nil
}

// checkQuestion takes the option text the user picked, so the shuffled order needs no server state.
func (s *server) checkQuestion(w http.ResponseWriter, r *http.Request, u user) error {
	q, ok := s.byID[r.PathValue("id")]
	if !ok {
		return errNotFound
	}
	var body struct {
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !slices.Contains(q.Options, body.Answer) {
		return errBadRequest
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"correct": body.Answer == q.Options[q.Answer], "correct_answer": q.Options[q.Answer], "explanation": q.Explanation,
		"source_url": q.SourceURL, "source_label": q.SourceLabel,
		"explain_image": q.ExplainImage, "explain_image_alt": q.ExplainImageAlt,
	})
	return nil
}

// Flags.

func (s *server) listFlags(w http.ResponseWriter, r *http.Request, u user) error {
	flags, err := s.st.listFlags(r.Context(), u.Sub)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"flags": flags})
	return nil
}

func (s *server) putFlag(w http.ResponseWriter, r *http.Request, u user) error {
	q, ok := s.byID[r.PathValue("id")]
	if !ok {
		return errNotFound
	}
	var body struct {
		Note string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || utf8.RuneCountInString(body.Note) > maxNoteLen {
		return errBadRequest
	}
	if err := s.st.setFlag(r.Context(), u.Sub, q.ID, q.Question, strings.TrimSpace(body.Note)); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *server) deleteFlag(w http.ResponseWriter, r *http.Request, u user) error {
	if err := s.st.deleteFlag(r.Context(), u.Sub, r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// staticHandler serves the embedded web files. Browsers revalidate on every request using a
// content-hash ETag, so a release that changes a file (sign images keep their names) reaches
// users immediately, and a file that did not change costs only a 304. The page itself carries the
// build id, so its ETag changes with every release.
func staticHandler(fsys fs.FS, build string) http.Handler {
	etags := map[string]string{}
	etagOf := func(b []byte) string {
		sum := sha256.Sum256(b)
		return `"` + hex.EncodeToString(sum[:8]) + `"`
	}
	fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if b, err := fs.ReadFile(fsys, p); err == nil {
			etags[p] = etagOf(b)
		}
		return nil
	})
	var index []byte
	if b, err := fs.ReadFile(fsys, "index.html"); err == nil {
		index = bytes.ReplaceAll(b, []byte(`<meta name="app-build" content="">`), []byte(`<meta name="app-build" content="`+html.EscapeString(build)+`">`))
		etags["index.html"] = etagOf(index)
	}
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if tag, ok := etags[name]; ok {
			w.Header().Set("ETag", tag)
		}
		w.Header().Set("Cache-Control", "no-cache")
		if r.URL.Path == "/" && index != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
			return
		}
		files.ServeHTTP(w, r)
	})
}
