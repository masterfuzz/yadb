package store

import (
	"regexp"
	"strings"
)

// Field is a parsed dotted field address. Segments may contain the wildcards
// "*" (matches exactly one prefix segment) and "**" (matches zero or more
// prefix segments). Wildcards are only meaningful in the portion of the field
// that addresses files; the remaining segments form a literal in-file path.
type Field struct {
	Segments []string
	Wildcard bool
}

// ParseField splits a dotted field into segments and notes whether it contains
// wildcards. Empty segments (from leading/trailing/double dots) are dropped.
func ParseField(s string) Field {
	var segs []string
	wild := false
	for _, seg := range strings.Split(s, ".") {
		if seg == "" {
			continue
		}
		if seg == "*" || seg == "**" {
			wild = true
		}
		segs = append(segs, seg)
	}
	return Field{Segments: segs, Wildcard: wild}
}

// Match records how a field resolved against a single file.
type Match struct {
	File File
	// InFile is the literal path inside the file (empty means the whole
	// document). Only meaningful when Ancestor is false.
	InFile []string
	// Ancestor is true when the field addresses a directory level strictly
	// above the file, i.e. the file lives inside the field's subtree.
	Ancestor bool
	// Remainder is the file's prefix segments below the field, set only when
	// Ancestor is true.
	Remainder []string
}

// Resolve maps a field onto the given files, returning every file the field
// touches: as an in-file path (or whole document) or as an ancestor directory.
// Files whose in-file path would require unsupported in-file wildcards are
// skipped.
func Resolve(files []File, field Field) []Match {
	var matches []Match
	for _, f := range files {
		if leftover, ok := matchInside(field.Segments, f.Segments); ok {
			if containsWildcard(leftover) {
				continue
			}
			matches = append(matches, Match{File: f, InFile: leftover})
			continue
		}
		if rem, ok := matchAncestor(field.Segments, f.Segments); ok {
			matches = append(matches, Match{File: f, Ancestor: true, Remainder: rem})
		}
	}
	return matches
}

// matchInside consumes all of pre using pat tokens and returns the leftover pat
// tokens (the in-file path). It reports false when pat cannot cover the whole
// prefix. "**" is greedy, consuming as many prefix segments as possible.
func matchInside(pat, pre []string) ([]string, bool) {
	if len(pre) == 0 {
		// A trailing/leading "**" matches zero remaining segments.
		for len(pat) > 0 && pat[0] == "**" {
			pat = pat[1:]
		}
		return pat, true
	}
	if len(pat) == 0 {
		return nil, false
	}
	switch pat[0] {
	case "**":
		for k := len(pre); k >= 0; k-- {
			if leftover, ok := matchInside(pat[1:], pre[k:]); ok {
				return leftover, true
			}
		}
		return nil, false
	case "*":
		return matchInside(pat[1:], pre[1:])
	default:
		if pat[0] == pre[0] {
			return matchInside(pat[1:], pre[1:])
		}
		return nil, false
	}
}

// matchAncestor reports whether pat is exhausted while matching a strict prefix
// of pre, i.e. the field names a directory above the file. It returns the file
// prefix segments that fall below the field.
func matchAncestor(pat, pre []string) ([]string, bool) {
	if len(pat) == 0 {
		if len(pre) == 0 {
			return nil, false // exact match, not a strict ancestor
		}
		return pre, true
	}
	if len(pre) == 0 {
		return nil, false
	}
	switch pat[0] {
	case "**":
		for k := len(pre); k >= 0; k-- {
			if rem, ok := matchAncestor(pat[1:], pre[k:]); ok {
				return rem, true
			}
		}
		return nil, false
	case "*":
		return matchAncestor(pat[1:], pre[1:])
	default:
		if pat[0] == pre[0] {
			return matchAncestor(pat[1:], pre[1:])
		}
		return nil, false
	}
}

func containsWildcard(segs []string) bool {
	for _, s := range segs {
		if s == "*" || s == "**" {
			return true
		}
	}
	return false
}

// InsideMatches returns only the matches that address a path within a file
// (excluding ancestor-directory matches).
func InsideMatches(matches []Match) []Match {
	var out []Match
	for _, m := range matches {
		if !m.Ancestor {
			out = append(out, m)
		}
	}
	return out
}

// Deepest returns the inside match with the longest file prefix, used to
// disambiguate a non-wildcard field that could land in nested files (the
// deepest file wins). It reports false when there is no inside match.
func Deepest(matches []Match) (Match, bool) {
	best := -1
	var bestMatch Match
	for _, m := range matches {
		if m.Ancestor {
			continue
		}
		if len(m.File.Segments) > best {
			best = len(m.File.Segments)
			bestMatch = m
		}
	}
	return bestMatch, best >= 0
}

var bareKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Expr renders the match's in-file path as a yq expression, e.g. ".config.port"
// or `.metadata["my-label"]`. An empty in-file path yields "." (the whole
// document).
func (m Match) Expr() string {
	if len(m.InFile) == 0 {
		return "."
	}
	var b strings.Builder
	for _, key := range m.InFile {
		if bareKey.MatchString(key) {
			b.WriteByte('.')
			b.WriteString(key)
		} else {
			b.WriteString(`.["`)
			b.WriteString(strings.ReplaceAll(key, `"`, `\"`))
			b.WriteString(`"]`)
		}
	}
	return b.String()
}

// ResolvedField returns the full dotted field this match represents, combining
// the file prefix with the in-file path (for inside matches) or reproducing the
// field-level address (for ancestor matches).
func (m Match) ResolvedField() string {
	segs := append([]string{}, m.File.Segments...)
	if m.Ancestor {
		return strings.Join(segs[:len(segs)-len(m.Remainder)], ".")
	}
	segs = append(segs, m.InFile...)
	return strings.Join(segs, ".")
}
