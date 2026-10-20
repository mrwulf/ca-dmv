package main

import (
	"bytes"
	"context"
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
	if _, err := st.createSession(ctx, "user-1", "Pat", sessionTTL); err != nil {
		t.Fatal(err)
	}
	bank, _ := loadBank(questionsJSON)
	id, err := st.createExam(ctx, "user-1", buildExam(bank, 3))
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
	st.createSession(ctx, "u", "U", sessionTTL)
	bank, _ := loadBank(questionsJSON)
	first, _ := st.createExam(ctx, "u", buildExam(bank, 3))
	second, _ := st.createExam(ctx, "u", buildExam(bank, 3))
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
	tok, err := st.createSession(ctx, "u", "U", sessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	if u, err := st.sessionUser(ctx, tok); err != nil || u.Sub != "u" {
		t.Fatalf("session lookup: %v %+v", err, u)
	}
	st.deleteSession(ctx, tok)
	if _, err := st.sessionUser(ctx, tok); err != errNotFound {
		t.Fatal("deleted session still valid")
	}
	expired, _ := st.createSession(ctx, "u", "U", -1)
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
	tok, err := st.createSession(context.Background(), "user-1", "Pat", sessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	return &apiClient{t: t, h: s.routes(), cookie: &http.Cookie{Name: sessionCookie, Value: tok}, baseURL: cfg.baseURL}
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
		if !strings.HasPrefix(q.SourceURL, prefix) || q.SourceLabel == "" {
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
