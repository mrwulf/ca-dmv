package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestBankIsValid(t *testing.T) {
	bank, err := loadBank(questionsJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(bank) < 46 {
		t.Fatalf("bank has %d questions, need at least 46", len(bank))
	}
	for _, q := range bank {
		if _, ok := TopicNames[q.Topic]; !ok {
			t.Errorf("%s: unknown topic %q", q.ID, q.Topic)
		}
	}
}

// TopicNames must match TOPICS in web/app.js.
var TopicNames = map[string]bool{
	"signs": true, "speed": true, "right-of-way": true, "lanes": true, "parking": true, "sharing-road": true,
	"safe-driving": true, "alcohol": true, "licenses": true, "freeway": true, "rail-bus": true, "emergencies": true,
}

func TestBuildExamKeepsCorrectAnswer(t *testing.T) {
	bank, _ := loadBank(questionsJSON)
	byID := map[string]question{}
	for _, q := range bank {
		byID[q.ID] = q
	}
	qs := buildExam(bank, 46)
	if len(qs) != 46 {
		t.Fatalf("got %d questions", len(qs))
	}
	seen := map[string]bool{}
	for _, q := range qs {
		if seen[q.ID] {
			t.Fatalf("duplicate question %s", q.ID)
		}
		seen[q.ID] = true
		orig := byID[q.ID]
		if q.Options[q.Answer] != orig.Options[orig.Answer] {
			t.Fatalf("%s: shuffled answer points at %q, want %q", q.ID, q.Options[q.Answer], orig.Options[orig.Answer])
		}
	}
}

func TestPassNeeded(t *testing.T) {
	for total, want := range map[int]int{36: 29, 10: 8, 46: 37} {
		if got := passNeeded(total); got != want {
			t.Errorf("passNeeded(%d) = %d, want %d", total, got, want)
		}
	}
}

func newTestStore(t *testing.T) *store {
	t.Helper()
	st, err := openStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestExamFlow(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	if _, err := st.createSession(ctx, "user-1", "Pat", "", sessionTTL); err != nil {
		t.Fatal(err)
	}
	bank, _ := loadBank(questionsJSON)
	id, err := st.createExam(ctx, "user-1", buildExam(bank, 3), "")
	if err != nil {
		t.Fatal(err)
	}

	e, _ := st.getExam(ctx, "user-1", id)
	for _, q := range viewExam(e, nil).Questions {
		if q.CorrectIndex != nil || q.Explanation != "" {
			t.Fatal("unanswered question leaks its answer")
		}
	}
	if _, err = st.getExam(ctx, "someone-else", id); err != errNotFound {
		t.Fatalf("other user can read exam: %v", err)
	}
	if _, err = st.answerQuestion(ctx, "user-1", id, 0, 99); err != errBadRequest {
		t.Fatalf("out-of-range choice: %v", err)
	}

	right := e.Questions[0].Answer
	e, err = st.answerQuestion(ctx, "user-1", id, 0, right)
	if err != nil || e.Correct != 1 {
		t.Fatalf("first answer: %v correct=%d", err, e.Correct)
	}
	// Re-answering with a wrong choice must not change the recorded answer or score.
	e, _ = st.answerQuestion(ctx, "user-1", id, 0, (right+1)%len(e.Questions[0].Options))
	if e.Correct != 1 || !e.Answers[0].Correct {
		t.Fatal("answer was overwritten")
	}
	for pos := 1; pos < 3; pos++ {
		wrong := (e.Questions[pos].Answer + 1) % len(e.Questions[pos].Options)
		if e, err = st.answerQuestion(ctx, "user-1", id, pos, wrong); err != nil {
			t.Fatal(err)
		}
	}
	if e.FinishedAt == nil || e.Correct != 1 {
		t.Fatalf("exam not finished correctly: finished=%v correct=%d", e.FinishedAt != nil, e.Correct)
	}

	exams, topics, err := st.stats(ctx, "user-1")
	if err != nil || len(exams) != 1 || exams[0].Correct != 1 || exams[0].Total != 3 {
		t.Fatalf("stats: %v %+v", err, exams)
	}
	total := 0
	for _, ts := range topics {
		total += ts.Total
	}
	if total != 3 {
		t.Fatalf("topic stats count %d answers, want 3", total)
	}
}

func TestNewExamReplacesUnfinished(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	st.createSession(ctx, "u", "U", "", sessionTTL)
	bank, _ := loadBank(questionsJSON)
	first, _ := st.createExam(ctx, "u", buildExam(bank, 3), "")
	second, _ := st.createExam(ctx, "u", buildExam(bank, 3), "")
	if _, err := st.getExam(ctx, "u", first); err != errNotFound {
		t.Fatal("unfinished exam should be replaced")
	}
	if a, _ := st.activeExam(ctx, "u"); a == nil || a.ID != second {
		t.Fatal("active exam should be the newest")
	}
}

func TestSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	tok, err := st.createSession(ctx, "u", "U", "", sessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	if u, err := st.sessionUser(ctx, tok); err != nil || u.Sub != "u" {
		t.Fatalf("session lookup: %v %+v", err, u)
	}
	tok2, _ := st.createSession(ctx, "u", "U", "the-id-token", sessionTTL)
	if idt, err := st.endSession(ctx, tok2); err != nil || idt != "the-id-token" {
		t.Fatalf("endSession returned %q, %v", idt, err)
	}
	if _, err := st.sessionUser(ctx, tok2); err != errNotFound {
		t.Fatal("ended session still valid")
	}
	st.endSession(ctx, tok)
	if _, err := st.sessionUser(ctx, tok); err != errNotFound {
		t.Fatal("deleted session still valid")
	}
	expired, _ := st.createSession(ctx, "u", "U", "", -1)
	if _, err := st.sessionUser(ctx, expired); err != errNotFound {
		t.Fatal("expired session still valid")
	}
}

func TestSameOrigin(t *testing.T) {
	s := &server{cfg: config{baseURL: "https://dmv.example.com"}}
	h := s.sameOrigin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	cases := []struct {
		name, origin, site string
		want               int
	}{
		{"same origin", "https://dmv.example.com", "same-origin", 200},
		{"no headers", "", "", 200},
		{"cross site fetch", "https://evil.example", "cross-site", 403},
		{"bad origin only", "https://evil.example", "", 403},
	}
	for _, c := range cases {
		r := httptest.NewRequest("POST", "/api/exams", nil)
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if c.site != "" {
			r.Header.Set("Sec-Fetch-Site", c.site)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s: got %d want %d", c.name, w.Code, c.want)
		}
	}
}

func TestAPIRequiresSession(t *testing.T) {
	st := newTestStore(t)
	s := &server{cfg: config{baseURL: "http://localhost"}, st: st, auth: &authn{st: st}}
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, httptest.NewRequest("GET", "/api/stats", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", w.Code)
	}
	w = httptest.NewRecorder()
	s.routes().ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 200 {
		t.Fatalf("healthz got %d", w.Code)
	}
}

// apiClient calls the real handler chain with a signed-in session.
type apiClient struct {
	t       *testing.T
	st      *store
	h       http.Handler
	cookie  *http.Cookie
	baseURL string
}

func newAPIClient(t *testing.T) *apiClient {
	t.Helper()
	st := newTestStore(t)
	bank, err := loadBank(questionsJSON)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config{baseURL: "http://localhost", fullLength: defaultFullExamLength}
	s := &server{cfg: cfg, st: st, auth: &authn{cfg: cfg, st: st}, bank: bank, byID: map[string]question{}}
	for _, q := range bank {
		s.byID[q.ID] = q
	}
	tok, err := st.createSession(context.Background(), "user-1", "Pat", "", sessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	return &apiClient{t: t, st: st, h: s.routes(), cookie: &http.Cookie{Name: sessionCookie, Value: tok}, baseURL: cfg.baseURL}
}

func (c *apiClient) do(method, path string, body any, out any) int {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	r := httptest.NewRequest(method, path, &buf)
	r.AddCookie(c.cookie)
	r.Header.Set("Origin", c.baseURL)
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	if out != nil && w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			c.t.Fatalf("%s %s: bad json %q: %v", method, path, w.Body.String(), err)
		}
	}
	return w.Code
}

func TestFlagsAndSingleQuestion(t *testing.T) {
	c := newAPIClient(t)
	bank, _ := loadBank(questionsJSON)
	q := bank[0]
	path := "/api/questions/" + q.ID

	var qv struct {
		Options     []string `json:"options"`
		Explanation string   `json:"explanation"`
		Flagged     bool     `json:"flagged"`
	}
	if code := c.do("GET", path, nil, &qv); code != 200 || len(qv.Options) != len(q.Options) || qv.Explanation != "" {
		t.Fatalf("get question: %d %+v", code, qv)
	}
	if code := c.do("GET", "/api/questions/nope", nil, nil); code != 404 {
		t.Fatalf("unknown question = %d", code)
	}

	var res struct {
		Correct       bool   `json:"correct"`
		CorrectAnswer string `json:"correct_answer"`
		Explanation   string `json:"explanation"`
	}
	right := q.Options[q.Answer]
	if code := c.do("POST", path+"/check", map[string]string{"answer": right}, &res); code != 200 || !res.Correct || res.Explanation == "" {
		t.Fatalf("check right: %d %+v", code, res)
	}
	wrong := q.Options[(q.Answer+1)%len(q.Options)]
	if code := c.do("POST", path+"/check", map[string]string{"answer": wrong}, &res); code != 200 || res.Correct || res.CorrectAnswer != right {
		t.Fatalf("check wrong: %d %+v", code, res)
	}
	if code := c.do("POST", path+"/check", map[string]string{"answer": "not an option"}, nil); code != 400 {
		t.Fatalf("bogus answer = %d", code)
	}

	// Flag, read back through the question and the list, then unflag.
	if code := c.do("PUT", "/api/flags/"+q.ID, map[string]string{"note": "  looks wrong  "}, nil); code != 204 {
		t.Fatalf("put flag = %d", code)
	}
	var flags struct{ Flags []flag }
	c.do("GET", "/api/flags", nil, &flags)
	if len(flags.Flags) != 1 || flags.Flags[0].Note != "looks wrong" || flags.Flags[0].Question != q.Question {
		t.Fatalf("flags = %+v", flags)
	}
	c.do("GET", path, nil, &qv)
	if !qv.Flagged {
		t.Fatal("question should be flagged")
	}
	if code := c.do("PUT", "/api/flags/"+q.ID, map[string]string{"note": string(make([]rune, maxNoteLen+1))}, nil); code != 400 {
		t.Fatalf("oversized note = %d", code)
	}
	if code := c.do("PUT", "/api/flags/nope", map[string]string{}, nil); code != 404 {
		t.Fatalf("flag unknown question = %d", code)
	}
	c.do("DELETE", "/api/flags/"+q.ID, nil, nil)
	c.do("GET", "/api/flags", nil, &flags)
	if len(flags.Flags) != 0 {
		t.Fatal("flag not removed")
	}
}

func TestExamEndpointsFlagAndSource(t *testing.T) {
	c := newAPIClient(t)
	type qv struct {
		ID          string `json:"id"`
		Flagged     bool   `json:"flagged"`
		Chosen      *int   `json:"chosen"`
		CorrectIdx  *int   `json:"correct_index"`
		Explanation string `json:"explanation"`
	}
	var ex struct {
		ID         string `json:"id"`
		Total      int    `json:"total"`
		PassNeeded int    `json:"pass_needed"`
		Questions  []qv   `json:"questions"`
	}
	if code := c.do("POST", "/api/exams", map[string]int{"length": 7}, nil); code != 400 {
		t.Fatalf("odd length = %d", code)
	}
	if code := c.do("POST", "/api/exams", map[string]int{"length": defaultFullExamLength}, &ex); code != 201 {
		t.Fatalf("create = %d", code)
	}
	if ex.Total != defaultFullExamLength || ex.PassNeeded != 29 {
		t.Fatalf("total=%d pass_needed=%d", ex.Total, ex.PassNeeded)
	}
	for _, q := range ex.Questions {
		if q.CorrectIdx != nil || q.Explanation != "" {
			t.Fatal("exam response leaks answers")
		}
	}
	first := ex.Questions[0].ID
	c.do("PUT", "/api/flags/"+first, map[string]string{"note": "x"}, nil)
	var after struct{ Questions []qv }
	c.do("POST", "/api/exams/"+ex.ID+"/answer", map[string]int{"pos": 0, "choice": 0}, &after)
	if !after.Questions[0].Flagged || after.Questions[0].CorrectIdx == nil || after.Questions[0].Explanation == "" {
		t.Fatalf("answered question view = %+v", after.Questions[0])
	}
}

func TestSourceLinks(t *testing.T) {
	bank, _ := loadBank(questionsJSON)
	const prefix = "https://www.dmv.ca.gov/portal/handbook/california-driver-handbook/"
	linked := 0
	for _, q := range bank {
		isSample := strings.HasPrefix(q.Source, "dmv-sample")
		if q.SourceURL == "" {
			if isSample {
				t.Errorf("%s (%s) is a DMV sample question with no source link", q.ID, q.Source)
			}
			continue
		}
		linked++
		if !(strings.HasPrefix(q.SourceURL, prefix) || strings.HasPrefix(q.SourceURL, "https://www.chp.ca.gov/")) || q.SourceLabel == "" {
			t.Errorf("%s: bad source link %q (label %q)", q.ID, q.SourceURL, q.SourceLabel)
		}
	}
	if share := float64(linked) / float64(len(bank)); share < 0.85 {
		t.Errorf("only %.0f%% of questions link to the handbook, want at least 85%%", share*100)
	}
}

// These answers were out of date or wrong against the current Driver Handbook and were corrected.
func TestCorrectedAnswers(t *testing.T) {
	bank, _ := loadBank(questionsJSON)
	want := map[string]string{
		"California's minimum liability insurance requirement for bodily injury per person is:": "$30,000",
		"California's minimum auto liability insurance for property damage is:":                 "$15,000",
		"When following a motorcycle, you should:":                                              "Follow at least 3 seconds behind",
		"When you make a U-turn in a residential district, U-turns are legal:":                  "Only if no vehicle is approaching within 200 feet",
	}
	for _, q := range bank {
		if w, ok := want[q.Question]; ok {
			if got := q.Options[q.Answer]; got != w {
				t.Errorf("%q: correct answer is %q, want %q", q.Question, got, w)
			}
			delete(want, q.Question)
		}
	}
	for q := range want {
		t.Errorf("question not found: %q", q)
	}
}

// The bank stores the correct answer by index and many questions list it first, so exams must
// shuffle. Over many exams the correct answer should land in every position about equally often.
func TestShuffleSpreadsCorrectAnswer(t *testing.T) {
	bank, _ := loadBank(questionsJSON)
	var four []question
	for _, q := range bank {
		if len(q.Options) == 4 {
			four = append(four, q)
		}
	}
	counts := make([]int, 4)
	total := 0
	for range 200 {
		for _, q := range buildExam(four, 36) {
			counts[q.Answer]++
			total++
		}
	}
	for pos, n := range counts {
		if share := float64(n) / float64(total); share < 0.20 || share > 0.30 {
			t.Errorf("correct answer lands at position %d %.0f%% of the time, want about 25%% (counts %v)", pos, share*100, counts)
		}
	}
}

// finishExam creates an exam of n questions and answers every one with pick(q); it returns the exam id.
func finishExam(t *testing.T, st *store, sub string, qs []question, pick func(q question) int) string {
	t.Helper()
	ctx := context.Background()
	id, err := st.createExam(ctx, sub, qs, "")
	if err != nil {
		t.Fatal(err)
	}
	for pos, q := range qs {
		if _, err = st.answerQuestion(ctx, sub, id, pos, pick(q)); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func rightAnswer(q question) int { return q.Answer }
func wrongAnswer(q question) int { return (q.Answer + 1) % len(q.Options) }

func TestDeleteExamIsSoft(t *testing.T) {
	c := newAPIClient(t)
	ctx := context.Background()
	bank, _ := loadBank(questionsJSON)
	id := finishExam(t, c.st, "user-1", buildExam(bank, 5), wrongAnswer)

	var stats struct {
		Exams   []examSummary `json:"exams"`
		Topics  []topicStat   `json:"topics"`
		Mastery masteryView   `json:"mastery"`
	}
	c.do("GET", "/api/stats", nil, &stats)
	if len(stats.Exams) != 1 || len(stats.Topics) == 0 || stats.Mastery.Missed != 5 {
		t.Fatalf("before delete: %+v", stats)
	}

	if code := c.do("DELETE", "/api/exams/"+id, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete = %d", code)
	}
	if code := c.do("GET", "/api/exams/"+id, nil, nil); code != http.StatusNotFound {
		t.Fatalf("deleted exam still readable: %d", code)
	}
	if code := c.do("DELETE", "/api/exams/"+id, nil, nil); code != http.StatusNotFound {
		t.Fatalf("second delete = %d, want 404", code)
	}
	stats.Exams, stats.Topics = nil, nil
	c.do("GET", "/api/stats", nil, &stats)
	if len(stats.Exams) != 0 || len(stats.Topics) != 0 || stats.Mastery.Mastered+stats.Mastery.Missed != 0 {
		t.Fatalf("deleted exam still counts: %+v", stats)
	}

	// Nothing is removed: the exam and its answers are still in the database, marked deleted.
	var exams, answers int
	var deletedAt sql.NullInt64
	c.st.db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(deleted_at) FROM exams WHERE id=?`, id).Scan(&exams, &deletedAt)
	c.st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM answers WHERE exam_id=?`, id).Scan(&answers)
	if exams != 1 || answers != 5 || !deletedAt.Valid {
		t.Fatalf("rows removed or unmarked: exams=%d answers=%d deleted_at=%v", exams, answers, deletedAt)
	}
}

func TestDeleteExamOwnershipAndOrigin(t *testing.T) {
	c := newAPIClient(t)
	ctx := context.Background()
	bank, _ := loadBank(questionsJSON)
	id := finishExam(t, c.st, "user-1", buildExam(bank, 3), rightAnswer)
	if err := c.st.deleteExam(ctx, "someone-else", id); err != errNotFound {
		t.Fatalf("other user deleted the exam: %v", err)
	}
	r := httptest.NewRequest("DELETE", "/api/exams/"+id, nil)
	r.AddCookie(c.cookie)
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin delete = %d, want 403", w.Code)
	}
	if _, err := c.st.getExam(ctx, "user-1", id); err != nil {
		t.Fatalf("exam was deleted: %v", err)
	}
}

func TestMasteryUsesLatestAnswer(t *testing.T) {
	c := newAPIClient(t)
	bank, _ := loadBank(questionsJSON)
	qs := bank[:4]
	// First exam gets q0,q1 right and q2,q3 wrong; the second flips q1 and q2.
	finishExam(t, c.st, "user-1", qs, func(q question) int {
		if q.ID == qs[0].ID || q.ID == qs[1].ID {
			return rightAnswer(q)
		}
		return wrongAnswer(q)
	})
	finishExam(t, c.st, "user-1", qs, func(q question) int {
		if q.ID == qs[0].ID || q.ID == qs[2].ID {
			return rightAnswer(q)
		}
		return wrongAnswer(q)
	})
	var stats struct{ Mastery masteryView }
	c.do("GET", "/api/stats", nil, &stats)
	m := stats.Mastery
	// Latest: q0 right, q1 wrong, q2 right, q3 wrong.
	if m.Total != len(bank) || m.Mastered != 2 || m.Missed != 2 || m.Unseen != len(bank)-4 {
		t.Fatalf("mastery = %+v", m)
	}
}

func TestMigrationAddsDeletedAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// The schema as shipped in v0.1.x, before deleted_at existed.
	for _, q := range []string{
		`CREATE TABLE users (sub TEXT PRIMARY KEY, name TEXT NOT NULL, created_at INTEGER NOT NULL)`,
		`CREATE TABLE exams (id TEXT PRIMARY KEY, sub TEXT NOT NULL, created_at INTEGER NOT NULL, finished_at INTEGER, total INTEGER NOT NULL, correct INTEGER NOT NULL DEFAULT 0, questions TEXT NOT NULL)`,
		`INSERT INTO users VALUES ('u','U',1)`,
		`INSERT INTO exams VALUES ('e1','u',1,2,1,1,'[]')`,
	} {
		if _, err = old.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()
	st, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.getExam(context.Background(), "u", "e1"); err != nil {
		t.Fatalf("existing exam unreadable after migration: %v", err)
	}
	if err = st.deleteExam(context.Background(), "u", "e1"); err != nil {
		t.Fatalf("delete after migration: %v", err)
	}
	// Opening again must not try to add the column twice.
	if _, err = openStore(path); err != nil {
		t.Fatalf("reopen: %v", err)
	}
}

func TestBuildLabel(t *testing.T) {
	if v, c := buildLabel("v0.1.1", "3a957de0123456789abcdef"); v != "v0.1.1" || c != "3a957de" {
		t.Fatalf("got %q %q", v, c)
	}
	if _, c := buildLabel("v0.1.1", "abc"); c != "abc" {
		t.Fatalf("short sha changed: %q", c)
	}
}

func TestMeReportsVersion(t *testing.T) {
	oldV, oldC := version, commit
	version, commit = "v9.9.9", "0123456789abcdef"
	defer func() { version, commit = oldV, oldC }()
	c := newAPIClient(t)
	var me struct{ Version, Commit string }
	if code := c.do("GET", "/api/me", nil, &me); code != 200 || me.Version != "v9.9.9" || me.Commit != "0123456" {
		t.Fatalf("me = %d %+v", code, me)
	}
}

func TestFocusedTest(t *testing.T) {
	c := newAPIClient(t)
	bank, _ := loadBank(questionsJSON)
	inTopic := map[string]bool{}
	for _, q := range bank {
		if q.Topic == "parking" {
			inTopic[q.ID] = true
		}
	}
	var ex struct {
		ID        string `json:"id"`
		Topic     string `json:"topic"`
		Total     int    `json:"total"`
		Questions []struct {
			ID    string `json:"id"`
			Topic string `json:"topic"`
		} `json:"questions"`
	}
	if code := c.do("POST", "/api/exams", map[string]any{"topic": "parking"}, &ex); code != http.StatusCreated {
		t.Fatalf("create focused test = %d", code)
	}
	if ex.Topic != "parking" || ex.Total != quickExamLength || len(ex.Questions) != quickExamLength {
		t.Fatalf("focused test shape: topic=%q total=%d questions=%d", ex.Topic, ex.Total, len(ex.Questions))
	}
	for _, q := range ex.Questions {
		if q.Topic != "parking" || !inTopic[q.ID] {
			t.Fatalf("question %s (%s) is not in the parking category", q.ID, q.Topic)
		}
	}
	if code := c.do("POST", "/api/exams", map[string]any{"topic": "not-a-category"}, nil); code != http.StatusBadRequest {
		t.Fatalf("unknown category = %d, want 400", code)
	}
	// A normal test is unaffected and has no category.
	var normal struct{ Topic string }
	if code := c.do("POST", "/api/exams", map[string]int{"length": quickExamLength}, &normal); code != http.StatusCreated || normal.Topic != "" {
		t.Fatalf("normal test: %d topic=%q", code, normal.Topic)
	}
}

func TestFocusedTestTopicShowsInStats(t *testing.T) {
	c := newAPIClient(t)
	bank, _ := loadBank(questionsJSON)
	var qs []question
	for _, q := range bank {
		if q.Topic == "signs" && len(qs) < 3 {
			qs = append(qs, q)
		}
	}
	id, err := c.st.createExam(context.Background(), "user-1", qs, "signs")
	if err != nil {
		t.Fatal(err)
	}
	for pos, q := range qs {
		c.st.answerQuestion(context.Background(), "user-1", id, pos, q.Answer)
	}
	var stats struct{ Exams []examSummary }
	c.do("GET", "/api/stats", nil, &stats)
	if len(stats.Exams) != 1 || stats.Exams[0].Topic != "signs" {
		t.Fatalf("stats exams = %+v", stats.Exams)
	}
}
