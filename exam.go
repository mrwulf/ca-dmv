package main

import (
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

// buildExam picks n distinct random questions and shuffles each question's options.
func buildExam(bank []question, n int) []question {
	n = min(n, len(bank))
	out := make([]question, 0, n)
	for _, i := range rand.Perm(len(bank))[:n] {
		out = append(out, shuffledCopy(bank[i]))
	}
	return out
}

// passNeeded is the number of correct answers needed to reach passPercent.
func passNeeded(total int) int {
	return int(math.Ceil(float64(total*passPercent) / 100))
}
