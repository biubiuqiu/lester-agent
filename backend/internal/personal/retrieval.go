package personal

import (
	"sort"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

// Deterministic, bounded keyword retrieval works for Latin words and CJK
// bigrams. It is not semantic search. Facts
// remain explicit rows, not a vector database or a provider-owned memory store.
func selectMemories(memories []Memory, query string) []Memory {
	terms := memoryTerms(query)
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
		haystack := foldMemoryText(m.Topic + " " + m.Content)
		for _, term := range terms {
			if strings.Contains(haystack, term) {
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

func foldMemoryText(value string) string {
	// Only matching is normalized; authored memory bytes stay unchanged.
	value = strings.ToLower(norm.NFKD.String(value))
	latinBase := false
	value = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) && latinBase {
			return -1
		}
		if !unicode.Is(unicode.Mn, r) {
			latinBase = unicode.Is(unicode.Latin, r)
		}
		return r
	}, value)
	return norm.NFC.String(value)
}

func memoryTerms(query string) []string {
	query = foldMemoryText(query)
	terms := strings.FieldsFunc(query, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	runes := []rune(query)
	cjk := func(r rune) bool {
		return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
	}
	for i := 0; i+1 < len(runes); i++ {
		if cjk(runes[i]) && cjk(runes[i+1]) {
			terms = append(terms, string(runes[i:i+2]))
		}
	}
	seen := map[string]bool{}
	unique := []string{}
	for _, term := range terms {
		if len([]rune(term)) >= 2 && !seen[term] {
			seen[term] = true
			unique = append(unique, term)
		}
	}
	return unique
}

func memoryDirectory(memories []Memory) []map[string]any {
	items := make([]map[string]any, 0, len(memories))
	for _, m := range memories {
		topic := []rune(m.Topic)
		truncated := len(topic) > 40
		if truncated {
			topic = topic[:40]
		}
		items = append(items, map[string]any{"id": m.ID, "project_id": m.ProjectID, "kind": m.Kind, "topic": string(topic), "topic_truncated": truncated, "deleted": m.Deleted})
	}
	return items
}

func recallMemories(memories []Memory, ids []string) ([]Memory, error) {
	if len(ids) == 0 || len(ids) > 12 {
		return nil, ErrModel
	}
	byID := make(map[uuid.UUID]Memory, len(memories))
	for _, m := range memories {
		if !m.Deleted {
			byID[m.ID] = m
		}
	}
	seen := map[uuid.UUID]bool{}
	items := make([]Memory, 0, len(ids))
	characters := 0
	for _, raw := range ids {
		id, err := uuid.Parse(raw)
		m, ok := byID[id]
		if err != nil || !ok || seen[id] {
			return nil, ErrModel
		}
		seen[id] = true
		m.Evidence = ""
		characters += len([]rune(m.Topic)) + len([]rune(m.Content))
		if characters > 10000 {
			return nil, ErrModel
		}
		items = append(items, m)
	}
	return items, nil
}
