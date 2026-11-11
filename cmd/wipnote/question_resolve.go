package main

import (
	"fmt"
	"strings"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/question"
	"github.com/shakestzd/wipnote/core/workitem"
)

// minQuestionPrefix is the shortest id input matched as a prefix: "q-" and
// four hex digits. A shorter input is refused rather than guessed.
const minQuestionPrefix = len("q-") + 4

// questionHit is one stored question an id input names.
type questionHit struct {
	node *models.Node
	id   string
}

// locateQuestion returns the feature, bug or spike holding the question the
// input names, and that question's full id. The input may be the full id or
// a unique prefix of it, with or without "q-". An exact id wins over a longer
// id it prefixes, so an old 8-hex id stays answerable. Input that names more
// than one question is refused, listing them, so an answer never lands on a
// guess.
func locateQuestion(p *workitem.Project, in string) (*models.Node, string, error) {
	qid := in
	if !strings.HasPrefix(qid, "q-") {
		qid = "q-" + qid
	}
	exact, prefix := matchQuestions(questionItems(p), qid)
	hits := exact
	if len(hits) == 0 {
		if len(qid) < minQuestionPrefix {
			return nil, "", fmt.Errorf("question id %s is too short; give the full id or at least 4 hex digits after q-", in)
		}
		hits = prefix
	}
	switch len(hits) {
	case 0:
		return nil, "", fmt.Errorf("no question %s on any feature, bug or spike", in)
	case 1:
		return hits[0].node, hits[0].id, nil
	}
	names := make([]string, len(hits))
	for i, h := range hits {
		names[i] = h.id + " on " + h.node.ID
	}
	return nil, "", fmt.Errorf("question id %s matches more than one question (%s); give more of the id; nothing was recorded", in, strings.Join(names, ", "))
}

// matchQuestions splits the stored questions qid names into exact matches
// and longer ids that qid prefixes.
func matchQuestions(nodes []*models.Node, qid string) (exact, prefix []questionHit) {
	for _, node := range nodes {
		for _, q := range question.FromNode(node) {
			switch {
			case q.ID == qid:
				exact = append(exact, questionHit{node, q.ID})
			case strings.HasPrefix(q.ID, qid):
				prefix = append(prefix, questionHit{node, q.ID})
			}
		}
	}
	return exact, prefix
}
