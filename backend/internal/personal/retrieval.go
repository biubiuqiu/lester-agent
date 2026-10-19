package personal

import (
	"sort"
	"strings"
	"unicode"
)

// Deterministic, bounded retrieval works for Latin words and CJK bigrams. Facts
// remain explicit rows, not a vector database or a provider-owned memory store.
func selectMemories(memories []Memory, query string) []Memory {
	query = strings.ToLower(query)
	terms := strings.FieldsFunc(query, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	runes := []rune(query)
	for i := 0; i+1 < len(runes); i++ {
		if unicode.Is(unicode.Han, runes[i]) && unicode.Is(unicode.Han, runes[i+1]) {
			terms = append(terms, string(runes[i:i+2]))
		}
	}
	type scored struct {
		memory Memory
		score  int
	}
	ranked := []scored{}
	for _, m := range memories {
		if m.Deleted {
			continue
		}
		score := 0
		if m.Kind == "preference" {
			score = 8
		}
		if m.Kind == "project" {
			score = 6
		}
		haystack := strings.ToLower(m.Topic + " " + m.Content)
		for _, term := range terms {
			if len([]rune(term)) >= 2 && strings.Contains(haystack, term) {
				score += 4
			}
		}
		if score > 0 {
			ranked = append(ranked, scored{m, score})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	items := []Memory{}
	size := 0
	for _, v := range ranked {
		m := v.memory
		m.Evidence = ""
		n := len([]rune(m.Content)) + len([]rune(m.Topic))
		if size+n > 10000 || len(items) >= 24 {
			break
		}
		size += n
		items = append(items, m)
	}
	return items
}
