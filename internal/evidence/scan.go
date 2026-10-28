package evidence

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// The scanners below find what three regexps used to, match for match: every
// assistant message of every session goes through them when the index is
// built, and the regexps were a fifth of the CPU of a cold build. Each one
// keeps the regexp's leftmost-first choice, and scan_test.go checks them
// against it.

// identMatches is evIdent.FindAllString(s, -1):
//
//	`[^`]+`
//	| [A-Za-z_][A-Za-z0-9_]*[._/-][A-Za-z0-9_./-]+
//	| [a-z]+[A-Z][A-Za-z0-9]*
//	| [A-Z][a-z]+[A-Z][A-Za-z0-9]*
//	| [A-Z][A-Z0-9_]{2,}
//
// Every alternative opens on an ASCII byte, so a match can only start at one.
func identMatches(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		if end := identAt(s, i); end > i {
			out = append(out, s[i:end])
			i = end
			continue
		}
		i++
	}
	return out
}

// identAt is the end of the evIdent match that starts at i, or i when none does.
func identAt(s string, i int) int {
	c := s[i]
	if c == '`' {
		if j := strings.IndexByte(s[i+1:], '`'); j > 0 {
			return i + 1 + j + 1
		}
		return i
	}
	if !isIdentStart(c) {
		return i
	}
	if e, ok := pathIdentAt(s, i); ok {
		return e
	}
	switch {
	case isLower(c):
		// [a-z]+[A-Z][A-Za-z0-9]*
		p := i + 1
		for p < len(s) && isLower(s[p]) {
			p++
		}
		if p < len(s) && isUpper(s[p]) {
			return alnumRun(s, p+1)
		}
	case isUpper(c):
		// [A-Z][a-z]+[A-Z][A-Za-z0-9]*
		p := i + 1
		for p < len(s) && isLower(s[p]) {
			p++
		}
		if p > i+1 && p < len(s) && isUpper(s[p]) {
			return alnumRun(s, p+1)
		}
		// [A-Z][A-Z0-9_]{2,}
		e := i + 1
		for e < len(s) && (isUpper(s[e]) || isDigit(s[e]) || s[e] == '_') {
			e++
		}
		if e-i >= 3 {
			return e
		}
	}
	return i
}

// pathIdentAt is the end of [A-Za-z_][A-Za-z0-9_]*[._/-][A-Za-z0-9_./-]+ at
// i, where s[i] already opens it.
//
// The star stops at some p inside the run of word bytes after i or just past
// it, and the separator class takes s[p]: inside the run only an underscore,
// past it a dot, slash or dash. Wherever it stops, the last class runs on to
// the same place, the end of the path bytes from i, since every byte before
// it is one too. So all that decides is whether some p works.
func pathIdentAt(s string, i int) (int, bool) {
	end := i + 1
	for end < len(s) && isWordByte(s[end]) {
		end++
	}
	var ok bool
	switch {
	case end+1 < len(s) && isPathPunct(s[end]) && isPathByte(s[end+1]):
		// The star takes the whole run and the separator the byte after it.
		ok = true
	case end > i+1 && s[end-1] == '_' && end < len(s) && isPathPunct(s[end]):
		// The run ends on an underscore, followed by a path byte.
		ok = true
	default:
		// An underscore inside the run, followed by a word byte.
		ok = strings.IndexByte(s[i+1:max(i+1, end-1)], '_') >= 0
	}
	if !ok {
		return i, false
	}
	for end < len(s) && isPathByte(s[end]) {
		end++
	}
	return end, true
}

// numMatches is evNum.FindAllString(s, -1): \d+(?:[.,]\d+)*.
func numMatches(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		if !isDigit(s[i]) {
			i++
			continue
		}
		e := digitRun(s, i)
		for e+1 < len(s) && (s[e] == '.' || s[e] == ',') && isDigit(s[e+1]) {
			e = digitRun(s, e+1)
		}
		out = append(out, s[i:e])
		i = e
	}
	return out
}

// wordMatches is evWord.FindAllString(s, -1): [\p{L}][\p{L}\p{N}_-]{3,}.
func wordMatches(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		r, w := decodeRune(s, i)
		if !isLetter(r) {
			i += w
			continue
		}
		e, n := i+w, 0
		for e < len(s) {
			rr, rw := decodeRune(s, e)
			if !isLetter(rr) && !isNumber(rr) && rr != '_' && rr != '-' {
				break
			}
			e += rw
			n++
		}
		if n >= 3 {
			out = append(out, s[i:e])
			i = e
			continue
		}
		i += w
	}
	return out
}

// decodeRune is the rune at i as the regexp reads it: a byte that does not
// start valid UTF-8 is U+FFFD, one byte wide.
func decodeRune(s string, i int) (rune, int) {
	if c := s[i]; c < utf8.RuneSelf {
		return rune(c), 1
	}
	return utf8.DecodeRuneInString(s[i:])
}

func isLetter(r rune) bool {
	if r < utf8.RuneSelf {
		return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z'
	}
	return unicode.Is(unicode.L, r)
}

func isNumber(r rune) bool {
	if r < utf8.RuneSelf {
		return '0' <= r && r <= '9'
	}
	return unicode.Is(unicode.N, r)
}

func digitRun(s string, i int) int {
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return i
}

func alnumRun(s string, i int) int {
	for i < len(s) && (isLower(s[i]) || isUpper(s[i]) || isDigit(s[i])) {
		i++
	}
	return i
}

func isLower(c byte) bool      { return 'a' <= c && c <= 'z' }
func isUpper(c byte) bool      { return 'A' <= c && c <= 'Z' }
func isDigit(c byte) bool      { return '0' <= c && c <= '9' }
func isIdentStart(c byte) bool { return isLower(c) || isUpper(c) || c == '_' }
func isWordByte(c byte) bool   { return isIdentStart(c) || isDigit(c) }
func isPathPunct(c byte) bool  { return c == '.' || c == '/' || c == '-' }
func isPathByte(c byte) bool   { return isWordByte(c) || c == '.' || c == '/' || c == '-' }
