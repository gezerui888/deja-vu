package evidence

import (
	"math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The regexps the scanners replace, kept here as the reference.
var (
	refNum   = regexp.MustCompile(`\d+(?:[.,]\d+)*`)
	refIdent = regexp.MustCompile("`[^`]+`|[A-Za-z_][A-Za-z0-9_]*[._/-][A-Za-z0-9_./-]+|[a-z]+[A-Z][A-Za-z0-9]*|[A-Z][a-z]+[A-Z][A-Za-z0-9]*|[A-Z][A-Z0-9_]{2,}")
	refWord  = regexp.MustCompile(`[\p{L}][\p{L}\p{N}_-]{3,}`)
)

var scanCases = []string{
	"", "a", "_", "`", "``", "`a`", "``a`", "`a", "a`b`c", "`multi\nline`", "`ü`",
	"foo", "foo_bar", "foo_", "foo_.", "foo_.x", "_x", "__", "___", "a_b", "a__b", "x_-y", "x_/", "_/a",
	"pkg/index", "pkg/", "a.b", "a..", "a.-", "1.2.3", "v1.2.3", "a/b/c.go", "-a.b", ".a.b",
	"fooBar", "fooB", "fB", "aB1c", "FooBar", "FooB", "Foo", "FB", "FBI", "F_1", "A1", "ABC_", "AB",
	"HTTPServer", "getHTTP", "XMLHttpRequest", "iPhone", "eBay2go", "Ab_Cd", "AbCd_ef.g",
	"12", "1,000", "1,000.50", "1.", "1..2", "1.,2", ",1", "3.14,", "007", "1e10", "٣٤٥", "²³",
	"word", "wor", "words-here", "a-b-c-d", "café", "naïve", "Ünïcödé", "日本語のテキスト", "привет мир",
	"x\u0301yzw", "a\u00b2bc", "\u2160\u2161\u2162\u2163", "ab\xffcd", "\xff\xfe", "ab\xc3", "ﬁle",
	"K\u212aelvin", "ſtop", "the `deja index` run took 3.1x and TestSlugEmpty failed at pkg/index/x.go:12",
	"See README.md, then run `make test`; API_KEY=abc123 and maxRetries=5 in config_v2.yaml",
	strings.Repeat("a_", 200), strings.Repeat("A", 300), strings.Repeat("`x", 100), strings.Repeat("1.", 200),
	strings.Repeat("ab", 500) + "C", strings.Repeat("_", 50) + "/", "end_",
}

func checkScanners(t *testing.T, s string) {
	t.Helper()
	for _, c := range []struct {
		name string
		got  []string
		re   *regexp.Regexp
	}{
		{"ident", identMatches(s), refIdent},
		{"num", numMatches(s), refNum},
		{"word", wordMatches(s), refWord},
	} {
		if want := c.re.FindAllString(s, -1); !slices.Equal(c.got, want) {
			t.Fatalf("%s scanner on %q:\n got %q\nwant %q", c.name, s, c.got, want)
		}
	}
}

func TestScannersMatchTheRegexps(t *testing.T) {
	for _, s := range scanCases {
		checkScanners(t, s)
		// And each case set among others, where a match ends or starts
		// against its neighbours.
		for _, o := range []string{" ", "x", "_", ".", "`", "1", "é", "A"} {
			checkScanners(t, o+s+o)
		}
	}
}

// Random text over the bytes the patterns turn on, including broken UTF-8.
func TestScannersMatchTheRegexpsOnRandomText(t *testing.T) {
	alphabet := []string{"a", "z", "A", "Z", "b", "C", "0", "9", "_", ".", ",", "/", "-", "`", " ", "\n",
		"é", "Ж", "日", "٣", "²", "\u0301", "\xff", "\xc3", "K", "ſ", ":", ";"}
	r := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // seeded so a failure reproduces
	for range 20000 {
		var b strings.Builder
		for range r.IntN(40) {
			b.WriteString(alphabet[r.IntN(len(alphabet))])
		}
		checkScanners(t, b.String())
	}
}

func FuzzScannersMatchTheRegexps(f *testing.F) {
	for _, s := range scanCases {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { checkScanners(t, s) })
}

// RuleLike looks for its words before it runs the regexp; it must answer as
// the regexp alone does.
func TestRuleLikeAnswersAsItsRegexp(t *testing.T) {
	ref := func(s string) bool {
		t := strings.TrimSpace(s)
		return t != "" && !strings.HasSuffix(t, "?") && ruleRE.MatchString(t)
	}
	cases := []string{
		"always run the tests", "ALWAYS run", "Never mind", "whenever you edit", "before you push",
		"Before committing, lint", "after each change", "do not merge", "Don't push", "you MUST rebase",
		"make sure it builds", "I prefer tabs", "use x instead of y", "that is not enough", "not allowed here",
		"see the policy", "rules", "a rule.", "unruled", "everytime", "every  time", "beforehand",
		"mu\u017Ft run", "\u212Aeep it", "всегда запускай тесты", "Никогда не пушь", "правило такое",
		"plain text with nothing in it", "should I always?", "", "   ", "nevertheless", "musty",
	}
	for _, s := range cases {
		if got, want := RuleLike(s), ref(s); got != want {
			t.Errorf("RuleLike(%q) = %v, the regexp says %v", s, got, want)
		}
	}
	words := []string{"rule", "always", "never", "every", "time", "each", "when", "ever", "from", "now", "on",
		"before", "after", "you", "any", "do", "not", "don't", "must", "make", "sure", "prefer", "instead",
		"of", "is", "enough", "allowed", "policy", "x", "ALWAYS", "Must", "MAKE SURE", "?", ".", "\n", "  "}
	r := rand.New(rand.NewPCG(3, 4)) //nolint:gosec // seeded so a failure reproduces
	for range 20000 {
		var b strings.Builder
		for range r.IntN(8) {
			b.WriteString(words[r.IntN(len(words))])
			if r.IntN(3) > 0 {
				b.WriteByte(' ')
			}
		}
		s := b.String()
		if got, want := RuleLike(s), ref(s); got != want {
			t.Fatalf("RuleLike(%q) = %v, the regexp says %v", s, got, want)
		}
	}
}
