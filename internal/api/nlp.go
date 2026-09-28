package api

import (
	"sort"
	"strings"
	"unicode"
)

// nlpFreq returns top n word frequencies using a simple bigram + ASCII word tokenizer.
// Full gse-based tokenizer is initialized in internal/nlp for the premium path; here we
// also fall back gracefully for wordcloud preview.
func nlpFreq(text string, topN int) []map[string]any {
	if topN <= 0 {
		topN = 60
	}
	freq := map[string]int{}
	for _, t := range tokenize(text) {
		if len(t) < 2 {
			continue
		}
		freq[t]++
	}
	type kv struct {
		name  string
		value int
	}
	var list []kv
	for k, v := range freq {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].value == list[j].value {
			return list[i].name < list[j].name
		}
		return list[i].value > list[j].value
	})
	out := []map[string]any{}
	for i := 0; i < len(list) && i < topN; i++ {
		out = append(out, map[string]any{"name": list[i].name, "value": list[i].value})
	}
	return out
}

func tokenize(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var tokens []string
	var asciiBuf strings.Builder
	flushASCII := func() {
		if asciiBuf.Len() >= 2 {
			tokens = append(tokens, asciiBuf.String())
		}
		asciiBuf.Reset()
	}
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if unicode.Is(unicode.Han, r) {
			flushASCII()
			// emit unigram (single han char) AND bigrams with neighbors
			tokens = append(tokens, string(r))
			if i+1 < len(runes) && unicode.Is(unicode.Han, runes[i+1]) {
				tokens = append(tokens, string([]rune{r, runes[i+1]}))
			}
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			asciiBuf.WriteRune(unicode.ToLower(r))
		} else {
			flushASCII()
		}
	}
	flushASCII()
	return tokens
}
