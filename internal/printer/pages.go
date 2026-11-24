package printer

import (
	"fmt"
	"strconv"
	"strings"
)

// ParsePages turns "1-3,5" into [1 2 3 5] for a document of n pages.
// An empty spec means every page.
func ParsePages(spec string, n int) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if n < 1 {
		return nil, fmt.Errorf("document has no pages")
	}
	if spec == "" || strings.EqualFold(spec, "all") {
		out := make([]int, n)
		for i := range out {
			out[i] = i + 1
		}
		return out, nil
	}
	var out []int
	seen := map[int]bool{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi := part, part
		if a, b, ok := strings.Cut(part, "-"); ok {
			lo, hi = strings.TrimSpace(a), strings.TrimSpace(b)
		}
		from, err1 := strconv.Atoi(lo)
		to, err2 := strconv.Atoi(hi)
		if err1 != nil || err2 != nil || from < 1 || to < from {
			return nil, fmt.Errorf("invalid page range %q", part)
		}
		if to > n {
			to = n
		}
		for p := from; p <= to; p++ {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no pages selected")
	}
	return out, nil
}

// SplitDuplex splits pages for a manual flip: fronts are the odd positions,
// backs the even positions. reverseBacks prints the backs last-to-first, which
// is right when the printed stack comes out face up and is reinserted as is.
func SplitDuplex(pages []int, reverseBacks bool) (fronts, backs []int) {
	for i, p := range pages {
		if i%2 == 0 {
			fronts = append(fronts, p)
		} else {
			backs = append(backs, p)
		}
	}
	if reverseBacks {
		for i, j := 0, len(backs)-1; i < j; i, j = i+1, j-1 {
			backs[i], backs[j] = backs[j], backs[i]
		}
	}
	return fronts, backs
}
