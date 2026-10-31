package issue

import (
	"strconv"
	"strings"
	"unicode"
)

// maxSearchTerms caps the words of one search query.
const maxSearchTerms = 10

// searchVector is the indexed text-search expression (see migration
// 00004_issue_search.sql); it must match the index exactly to use it.
const searchVector = `(setweight(to_tsvector('simple', i.title), 'A') || setweight(to_tsvector('simple', i.description), 'B'))`

// searchQuery turns user input into a prefix tsquery: words of letters and
// digits, each as word:*, AND-ed. No words → ok false (no condition).
func searchQuery(s string) (string, bool) {
	words := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) == 0 {
		return "", false
	}
	if len(words) > maxSearchTerms {
		words = words[:maxSearchTerms]
	}
	for i, w := range words {
		words[i] = w + ":*"
	}
	return strings.Join(words, " & "), true
}

// search adds the full-text condition; an input that is exactly an identifier
// (KEY-N, former keys and numbers too) also matches that issue.
func (w *where) search(s string) {
	s = strings.TrimSpace(s)
	var conds []string
	if tsq, ok := searchQuery(s); ok {
		conds = append(conds, searchVector+" @@ to_tsquery('simple', "+w.arg(tsq)+")")
	}
	if m := identifierRe.FindStringSubmatch(s); m != nil {
		if n, err := strconv.ParseInt(m[2], 10, 32); err == nil {
			conds = append(conds, "i.id IN (SELECT n.issue_id FROM project_keys k JOIN issue_numbers n ON n.project_id = k.project_id"+
				" WHERE k.key = "+w.arg(strings.ToUpper(m[1]))+" AND n.number = "+w.arg(int32(n))+")")
		}
	}
	if len(conds) > 0 {
		w.conds = append(w.conds, "("+strings.Join(conds, " OR ")+")")
	}
}
