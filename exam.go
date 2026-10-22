package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
)

type question struct {
	ID          string   `json:"id"`
	Topic       string   `json:"topic"`
	Question    string   `json:"question"`
	Options     []string `json:"options"`
	Answer      int      `json:"answer"`
	Explanation string   `json:"explanation"`
	Source      string   `json:"source"`
	SourceURL   string   `json:"sourceUrl,omitempty"`
	SourceLabel string   `json:"sourceLabel,omitempty"`
	Image       string   `json:"image,omitempty"`
	ImageAlt    string   `json:"imageAlt,omitempty"`
	// ExplainImage is a picture shown with the explanation, only after the question is answered.
	ExplainImage    string `json:"explainImage,omitempty"`
	ExplainImageAlt string `json:"explainImageAlt,omitempty"`
}

const (
	quickExamLength       = 10
	defaultFullExamLength = 36
	// passPercent is the DMV's published passing score for the knowledge test.
	passPercent = 80
)

// questionPublic is a question with its answer withheld: what the browser sees before answering.
type questionPublic struct {
	ID       string   `json:"id"`
	Topic    string   `json:"topic"`
	Text     string   `json:"text"`
	Options  []string `json:"options"`
	Image    string   `json:"image,omitempty"`
	ImageAlt string   `json:"image_alt,omitempty"`
}

// shuffledCopy returns a copy of q with its options in random order and Answer updated to match.
func shuffledCopy(q question) question {
	q.Options = slices.Clone(q.Options)
	correct := q.Options[q.Answer]
	rand.Shuffle(len(q.Options), func(a, b int) { q.Options[a], q.Options[b] = q.Options[b], q.Options[a] })
	q.Answer = slices.Index(q.Options, correct)
	return q
}

func loadBank(raw []byte) ([]question, error) {
	var bank []question
	if err := json.Unmarshal(raw, &bank); err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, q := range bank {
		switch {
		case q.ID == "" || ids[q.ID]:
			return nil, fmt.Errorf("question %q: missing or duplicate id", q.ID)
		case q.Question == "" || q.Explanation == "" || q.Topic == "":
			return nil, fmt.Errorf("question %s: missing text, topic or explanation", q.ID)
		case len(q.Options) < 2 || q.Answer < 0 || q.Answer >= len(q.Options):
			return nil, fmt.Errorf("question %s: bad options or answer index", q.ID)
		}
		seen := map[string]bool{}
		for _, o := range q.Options {
			if seen[o] {
				return nil, fmt.Errorf("question %s: duplicate option %q", q.ID, o)
			}
			seen[o] = true
		}
		ids[q.ID] = true
	}
	return bank, nil
}

// masteryStreak is how many times in a row a missed question must be answered correctly before it
// counts as mastered again.
const masteryStreak = 3

// progress maps each question id the user has answered to how many more correct answers in a row it
// needs to be mastered: 0 is mastered, anything above is a question still being learned. Questions
// the user has never answered are absent.
type progress map[string]int

// record applies one answer. A wrong answer sets the question back to masteryStreak; a right one
// counts down toward 0, so a question answered right the first time is mastered at once.
func (p progress) record(qid string, correct bool) {
	if !correct {
		p[qid] = masteryStreak
	} else if p[qid] > 0 {
		p[qid]--
	} else {
		p[qid] = 0
	}
}

// buildExam picks n distinct random questions and shuffles each question's options.
func buildExam(bank []question, n int) []question {
	return buildExamFor(bank, n, nil)
}

// Selection weights: a question's chance of being picked is proportional to its weight. Unseen
// questions come up most, missed ones still being learned next, and mastered ones least, but never
// never, so review continues and a missed question does not wait for the whole bank to be seen.
const (
	weightUnseen   = 10.0
	weightLearning = 6.0
	weightMastered = 1.0
)

func pickWeight(p progress, qid string) float64 {
	left, seen := p[qid]
	switch {
	case !seen:
		return weightUnseen
	case left > 0:
		return weightLearning
	}
	return weightMastered
}

// buildExamFor picks n distinct questions by weighted random sampling without replacement
// (Efraimidis-Spirakis: keep the n largest of rand^(1/weight)), then shuffles each question's options.
func buildExamFor(bank []question, n int, p progress) []question {
	n = min(n, len(bank))
	type keyed struct {
		i   int
		key float64
	}
	keys := make([]keyed, len(bank))
	for i, q := range bank {
		keys[i] = keyed{i, math.Pow(rand.Float64(), 1/pickWeight(p, q.ID))}
	}
	slices.SortFunc(keys, func(a, b keyed) int { return cmp.Compare(b.key, a.key) })
	out := make([]question, 0, n)
	for _, k := range keys[:n] {
		out = append(out, shuffledCopy(bank[k.i]))
	}
	return out
}

// passNeeded is the number of correct answers needed to reach passPercent.
func passNeeded(total int) int {
	return int(math.Ceil(float64(total*passPercent) / 100))
}
