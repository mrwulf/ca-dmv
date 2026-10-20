package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

var (
	errNotFound   = errors.New("not found")
	errBadRequest = errors.New("bad request")
)

const schema = `
CREATE TABLE IF NOT EXISTS users (
  sub TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  sub TEXT NOT NULL REFERENCES users(sub) ON DELETE CASCADE,
  expires_at INTEGER NOT NULL,
  id_token TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS login_flows (
  state TEXT PRIMARY KEY,
  nonce TEXT NOT NULL,
  verifier TEXT NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS exams (
  id TEXT PRIMARY KEY,
  sub TEXT NOT NULL REFERENCES users(sub) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  finished_at INTEGER,
  deleted_at INTEGER,
  topic TEXT NOT NULL DEFAULT '',
  total INTEGER NOT NULL,
  correct INTEGER NOT NULL DEFAULT 0,
  questions TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS exams_sub ON exams(sub, finished_at);
CREATE TABLE IF NOT EXISTS flags (
  sub TEXT NOT NULL REFERENCES users(sub) ON DELETE CASCADE,
  qid TEXT NOT NULL,
  question TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  PRIMARY KEY (sub, qid)
);
CREATE TABLE IF NOT EXISTS answers (
  exam_id TEXT NOT NULL REFERENCES exams(id) ON DELETE CASCADE,
  pos INTEGER NOT NULL,
  qid TEXT NOT NULL,
  topic TEXT NOT NULL,
  chosen INTEGER NOT NULL,
  is_correct INTEGER NOT NULL,
  PRIMARY KEY (exam_id, pos)
);
`

type store struct{ db *sql.DB }

func openStore(path string) (*store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection serializes writers, which keeps the read-then-write answer transaction atomic.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &store{db: db}, nil
}

// migrate brings a database created by an older version up to date. Tests are never removed:
// deleting one only sets exams.deleted_at.
func migrate(db *sql.DB) error {
	if err := addColumn(db, "exams", "deleted_at", "INTEGER"); err != nil {
		return err
	}
	if err := addColumn(db, "sessions", "id_token", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return addColumn(db, "exams", "topic", "TEXT NOT NULL DEFAULT ''")
}

func addColumn(db *sql.DB, table, column, ddl string) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('`+table+`') WHERE name=?`, column).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + ddl)
	return err
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// Login flows and sessions.

func (s *store) saveFlow(ctx context.Context, state, nonce, verifier string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO login_flows(state, nonce, verifier, expires_at) VALUES (?,?,?,?)`,
		state, nonce, verifier, time.Now().Add(10*time.Minute).Unix())
	return err
}

// takeFlow returns and deletes a login flow so a state value can be used once.
func (s *store) takeFlow(ctx context.Context, state string) (nonce, verifier string, err error) {
	var exp int64
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT nonce, verifier, expires_at FROM login_flows WHERE state=?`, state).Scan(&nonce, &verifier, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", errNotFound
	} else if err != nil {
		return "", "", err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM login_flows WHERE state=?`, state); err != nil {
		return "", "", err
	}
	if err = tx.Commit(); err != nil {
		return "", "", err
	}
	if time.Now().Unix() > exp {
		return "", "", errNotFound
	}
	return nonce, verifier, nil
}

// createSession stores a login session. idToken is kept so logout can send it to the provider as
// the id_token_hint.
func (s *store) createSession(ctx context.Context, sub, name, idToken string, ttl time.Duration) (string, error) {
	token := randomToken()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO users(sub, name, created_at) VALUES (?,?,?)
		ON CONFLICT(sub) DO UPDATE SET name=excluded.name`, sub, name, time.Now().Unix()); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO sessions(token_hash, sub, expires_at, id_token) VALUES (?,?,?,?)`,
		hashToken(token), sub, time.Now().Add(ttl).Unix(), idToken); err != nil {
		return "", err
	}
	return token, tx.Commit()
}

type user struct{ Sub, Name string }

func (s *store) sessionUser(ctx context.Context, token string) (user, error) {
	var u user
	err := s.db.QueryRowContext(ctx, `SELECT u.sub, u.name FROM sessions s JOIN users u ON u.sub = s.sub
		WHERE s.token_hash=? AND s.expires_at > ?`, hashToken(token), time.Now().Unix()).Scan(&u.Sub, &u.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return u, errNotFound
	}
	return u, err
}

// endSession deletes the session and returns the ID token it was created with ("" if none).
func (s *store) endSession(ctx context.Context, token string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var idToken string
	err = tx.QueryRowContext(ctx, `SELECT id_token FROM sessions WHERE token_hash=?`, hashToken(token)).Scan(&idToken)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, hashToken(token)); err != nil {
		return "", err
	}
	return idToken, tx.Commit()
}

func (s *store) cleanup(ctx context.Context) error {
	now := time.Now().Unix()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, now); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM login_flows WHERE expires_at < ?`, now)
	return err
}

// Exams.

type answer struct {
	Chosen  int
	Correct bool
}

type exam struct {
	Topic      string // set for a focused test on one category, empty otherwise
	ID         string
	CreatedAt  int64
	FinishedAt *int64
	Total      int
	Correct    int
	Questions  []question
	Answers    map[int]answer
}

// createExam replaces any unfinished exam of the user with a new one built from qs.
func (s *store) createExam(ctx context.Context, sub string, qs []question, topic string) (string, error) {
	raw, err := json.Marshal(qs)
	if err != nil {
		return "", err
	}
	id := randomToken()[:22]
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE exams SET deleted_at=? WHERE sub=? AND finished_at IS NULL AND deleted_at IS NULL`, time.Now().Unix(), sub); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO exams(id, sub, created_at, total, questions, topic) VALUES (?,?,?,?,?,?)`,
		id, sub, time.Now().Unix(), len(qs), string(raw), topic); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

type queryer interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
}

func loadExam(ctx context.Context, q queryer, sub, id string) (*exam, error) {
	e := &exam{ID: id, Answers: map[int]answer{}}
	var raw string
	var fin sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT created_at, finished_at, total, correct, questions, topic FROM exams WHERE id=? AND sub=? AND deleted_at IS NULL`, id, sub).
		Scan(&e.CreatedAt, &fin, &e.Total, &e.Correct, &raw, &e.Topic)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	} else if err != nil {
		return nil, err
	}
	if fin.Valid {
		e.FinishedAt = &fin.Int64
	}
	if err = json.Unmarshal([]byte(raw), &e.Questions); err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT pos, chosen, is_correct FROM answers WHERE exam_id=?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pos, chosen, ok int
		if err = rows.Scan(&pos, &chosen, &ok); err != nil {
			return nil, err
		}
		e.Answers[pos] = answer{Chosen: chosen, Correct: ok == 1}
	}
	return e, rows.Err()
}

func (s *store) getExam(ctx context.Context, sub, id string) (*exam, error) {
	return loadExam(ctx, s.db, sub, id)
}

// answerQuestion records an answer. Answering the same position twice keeps the first answer.
func (s *store) answerQuestion(ctx context.Context, sub, id string, pos, choice int) (*exam, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	e, err := loadExam(ctx, tx, sub, id)
	if err != nil {
		return nil, err
	}
	if pos < 0 || pos >= len(e.Questions) {
		return nil, errBadRequest
	}
	q := e.Questions[pos]
	if choice < 0 || choice >= len(q.Options) {
		return nil, errBadRequest
	}
	if _, done := e.Answers[pos]; done {
		return e, nil
	}
	ok := choice == q.Answer
	okInt := 0
	if ok {
		okInt = 1
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO answers(exam_id, pos, qid, topic, chosen, is_correct) VALUES (?,?,?,?,?,?)`,
		id, pos, q.ID, q.Topic, choice, okInt); err != nil {
		return nil, err
	}
	e.Answers[pos] = answer{Chosen: choice, Correct: ok}
	if ok {
		e.Correct++
	}
	if len(e.Answers) == e.Total {
		now := time.Now().Unix()
		e.FinishedAt = &now
		_, err = tx.ExecContext(ctx, `UPDATE exams SET correct=?, finished_at=? WHERE id=?`, e.Correct, now, id)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE exams SET correct=? WHERE id=?`, e.Correct, id)
	}
	if err != nil {
		return nil, err
	}
	return e, tx.Commit()
}

type activeExam struct {
	ID       string `json:"id"`
	Answered int    `json:"answered"`
	Total    int    `json:"total"`
}

func (s *store) activeExam(ctx context.Context, sub string) (*activeExam, error) {
	var a activeExam
	err := s.db.QueryRowContext(ctx, `SELECT e.id, e.total, (SELECT COUNT(*) FROM answers WHERE exam_id=e.id)
		FROM exams e WHERE e.sub=? AND e.finished_at IS NULL AND e.deleted_at IS NULL ORDER BY e.created_at DESC LIMIT 1`, sub).Scan(&a.ID, &a.Total, &a.Answered)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &a, err
}

// Stats.

type examSummary struct {
	ID         string `json:"id"`
	FinishedAt int64  `json:"finished_at"`
	Correct    int    `json:"correct"`
	Total      int    `json:"total"`
	Passed     bool   `json:"passed"`
	Topic      string `json:"topic,omitempty"`
}

type topicStat struct {
	Topic   string `json:"topic"`
	Correct int    `json:"correct"`
	Total   int    `json:"total"`
}

// stats returns up to 200 finished exams in chronological order plus per-topic accuracy.
func (s *store) stats(ctx context.Context, sub string) ([]examSummary, []topicStat, error) {
	exams := []examSummary{}
	rows, err := s.db.QueryContext(ctx, `SELECT id, finished_at, correct, total, topic FROM exams
		WHERE sub=? AND finished_at IS NOT NULL AND deleted_at IS NULL ORDER BY finished_at DESC LIMIT 200`, sub)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e examSummary
		if err = rows.Scan(&e.ID, &e.FinishedAt, &e.Correct, &e.Total, &e.Topic); err != nil {
			return nil, nil, err
		}
		e.Passed = e.Correct >= passNeeded(e.Total)
		exams = append([]examSummary{e}, exams...)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	topics := []topicStat{}
	trows, err := s.db.QueryContext(ctx, `SELECT a.topic, SUM(a.is_correct), COUNT(*) FROM answers a
		JOIN exams e ON e.id = a.exam_id WHERE e.sub=? AND e.finished_at IS NOT NULL AND e.deleted_at IS NULL GROUP BY a.topic`, sub)
	if err != nil {
		return nil, nil, err
	}
	defer trows.Close()
	for trows.Next() {
		var t topicStat
		if err = trows.Scan(&t.Topic, &t.Correct, &t.Total); err != nil {
			return nil, nil, err
		}
		topics = append(topics, t)
	}
	return exams, topics, trows.Err()
}

// Flags: questions a user wants to follow up on because they think the answer is wrong.

const maxNoteLen = 500

type flag struct {
	QID       string `json:"qid"`
	Question  string `json:"question"`
	Note      string `json:"note"`
	CreatedAt int64  `json:"created_at"`
}

// setFlag flags or re-flags a question, replacing the note. The question text is stored so the
// flag stays readable if the question bank changes later.
func (s *store) setFlag(ctx context.Context, sub, qid, text, note string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO flags(sub, qid, question, note, created_at) VALUES (?,?,?,?,?)
		ON CONFLICT(sub, qid) DO UPDATE SET note=excluded.note`, sub, qid, text, note, time.Now().Unix())
	return err
}

func (s *store) deleteFlag(ctx context.Context, sub, qid string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM flags WHERE sub=? AND qid=?`, sub, qid)
	return err
}

func (s *store) listFlags(ctx context.Context, sub string) ([]flag, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT qid, question, note, created_at FROM flags WHERE sub=? ORDER BY created_at DESC`, sub)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	flags := []flag{}
	for rows.Next() {
		var f flag
		if err = rows.Scan(&f.QID, &f.Question, &f.Note, &f.CreatedAt); err != nil {
			return nil, err
		}
		flags = append(flags, f)
	}
	return flags, rows.Err()
}

// flagNotes maps question id to note for every flagged question of the user.
func (s *store) flagNotes(ctx context.Context, sub string) (map[string]string, error) {
	flags, err := s.listFlags(ctx, sub)
	if err != nil {
		return nil, err
	}
	notes := make(map[string]string, len(flags))
	for _, f := range flags {
		notes[f.QID] = f.Note
	}
	return notes, nil
}

// deleteExam marks one of the user's exams as deleted. Its rows stay in the database, but it no
// longer appears anywhere or counts toward any statistic.
func (s *store) deleteExam(ctx context.Context, sub, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE exams SET deleted_at=? WHERE id=? AND sub=? AND deleted_at IS NULL`, time.Now().Unix(), id, sub)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return errNotFound
	}
	return nil
}

// latestAnswers maps each question id to whether the user's most recent answer to it was correct,
// over their finished exams that are not deleted.
func (s *store) latestAnswers(ctx context.Context, sub string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.qid, a.is_correct FROM answers a JOIN exams e ON e.id = a.exam_id
		WHERE e.sub=? AND e.finished_at IS NOT NULL AND e.deleted_at IS NULL ORDER BY e.finished_at, e.rowid, a.pos`, sub)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	latest := map[string]bool{}
	for rows.Next() {
		var qid string
		var ok int
		if err = rows.Scan(&qid, &ok); err != nil {
			return nil, err
		}
		latest[qid] = ok == 1
	}
	return latest, rows.Err()
}
