package memory

import (
	"strings"
)

// TokenCounter estimates token counts for text.
// The default implementation uses a character heuristic (4 chars ≈ 1 token).
// This interface allows swapping in a real tokenizer later without changing callers.
type TokenCounter interface {
	Count(text string) int
}

// CharHeuristicCounter implements TokenCounter with a 4-characters-per-token heuristic.
type CharHeuristicCounter struct{}

func (c *CharHeuristicCounter) Count(text string) int {
	if text == "" {
		return 0
	}
	// Round up: 1–4 chars = 1 token, 5–8 = 2, etc.
	chars := len([]rune(text))
	return (chars + 3) / 4
}

// DefaultCounter is the global default token counter.
var DefaultCounter TokenCounter = &CharHeuristicCounter{}

// TruncateOptions controls how text is truncated to fit a token budget.
type TruncateOptions struct {
	// Budget is the maximum number of tokens allowed.
	Budget int
	// Counter is the token counter to use. If nil, DefaultCounter is used.
	Counter TokenCounter
	// FromStart truncates from the beginning instead of the end.
	FromStart bool
}

// Truncate truncates text to fit within the given token budget.
// It truncates from the end by default (keeping the beginning), which matches
// the design doc's guidance that "most curated content tends to be organized top-down."
func Truncate(text string, opts TruncateOptions) string {
	counter := opts.Counter
	if counter == nil {
		counter = DefaultCounter
	}

	if opts.Budget <= 0 {
		return ""
	}

	tokens := counter.Count(text)
	if tokens <= opts.Budget {
		return text
	}

	// Binary search for the truncation point that fits within budget.
	// We operate on runes to avoid splitting multi-byte characters.
	runes := []rune(text)
	low, high := 0, len(runes)

	for low < high {
		mid := (low + high + 1) / 2
		var candidate string
		if opts.FromStart {
			candidate = string(runes[len(runes)-mid:])
		} else {
			candidate = string(runes[:mid])
		}
		if counter.Count(candidate) <= opts.Budget {
			low = mid
		} else {
			high = mid - 1
		}
	}

	if opts.FromStart {
		return string(runes[len(runes)-low:])
	}
	return string(runes[:low])
}

// TruncateLines truncates text line-by-line to fit within budget.
// It removes lines from the end (or beginning if FromStart) until the remaining
// text fits. This is less precise than Truncate but preserves line boundaries.
func TruncateLines(text string, opts TruncateOptions) string {
	counter := opts.Counter
	if counter == nil {
		counter = DefaultCounter
	}

	if opts.Budget <= 0 {
		return ""
	}

	lines := strings.Split(text, "\n")
	if opts.FromStart {
		for i := 0; i < len(lines); i++ {
			remaining := strings.Join(lines[i:], "\n")
			if counter.Count(remaining) <= opts.Budget {
				return remaining
			}
		}
		return ""
	}

	for i := len(lines); i > 0; i-- {
		remaining := strings.Join(lines[:i], "\n")
		if counter.Count(remaining) <= opts.Budget {
			return remaining
		}
	}
	return ""
}
