package twicli

import (
	"regexp"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/pattern"
	"mvdan.cc/sh/v3/syntax"
)

var parser = syntax.NewParser(
	syntax.Variant(syntax.LangBash),
)

// PopFirstWord pops the first word from the text and returns it along with the
// rest of the string.
func PopFirstWord(str string) (first, tail string, err error) {
	scanner := NewWordScanner(str)

	words, err := scanner.ScanN(1)
	if err != nil {
		return str, "", err
	}

	return words[0], scanner.Tail(), nil
}

// WordScannner helps scanning words.
type WordScanner struct {
	text string
	word string
	err  error
}

// NewWordScanner creates a new WordScanner.
func NewWordScanner(text string) *WordScanner {
	return &WordScanner{
		text: text,
	}
}

// Word returns the current word.
func (s *WordScanner) Word() string {
	return s.word
}

// Scan scans the next word.
func (s *WordScanner) Scan() bool {
	if s.err != nil {
		return false
	}

	var firstWord *syntax.Word
	err := parser.Words(strings.NewReader(s.text), func(word *syntax.Word) bool {
		firstWord = word
		return false
	})
	if err != nil {
		s.err = errors.Wrap(err, "cannot parse for shell word")
		return false
	}

	lit, err := shLiteral(firstWord)
	if err != nil {
		s.err = errors.Wrap(err, "cannot render parsed shell word")
		return false
	}

	s.word = lit
	s.text = strings.TrimSpace(s.text[firstWord.End().Offset():])

	return true
}

// shLiteral returns the literal string representation of the given shell word.
func shLiteral(word *syntax.Word) (string, error) {
	return expand.Literal(nil, word)
}

// Err returns the error that occurred during scanning, if any.
func (s *WordScanner) Err() error {
	return s.err
}

// ScanN scans the next N words.
func (s *WordScanner) ScanN(n int) ([]string, error) {
	words := make([]string, n)
	for i := 0; i < n; i++ {
		if !s.Scan() {
			return nil, s.Err()
		}
		words[i] = s.Word()
	}
	return words, nil
}

// Tail returns the remaining text.
func (s *WordScanner) Tail() string {
	return s.text
}

// TODO: make this a bounded LRU cache.
var patternRegexes sync.Map

const patternMode = pattern.Shortest

// ValidatePattern validates that the matching pattern is valid.
func ValidatePattern(match string) error {
	if !pattern.HasMeta(match, patternMode) {
		return nil
	}

	if _, ok := patternRegexes.Load(match); ok {
		return nil
	}

	restr, err := pattern.Regexp(match, patternMode)
	if err != nil {
		return errors.Wrap(err, "bad pattern")
	}

	re, err := regexp.Compile(restr)
	if err != nil {
		return errors.Wrap(err, "pattern compiled to erroneous regex")
	}

	patternRegexes.LoadOrStore(match, re)
	return nil
}

// PatternMatch returns true if the given text matches a shell-like star
// pattern, if there's one. If not, then strings are matched literally.
func PatternMatch(src, match string) bool {
	if !pattern.HasMeta(match, patternMode) {
		return src == match
	}

	cached, ok := patternRegexes.Load(match)
	if !ok {
		if err := ValidatePattern(match); err != nil {
			return false
		}
		cached, _ = patternRegexes.Load(match)
	}

	re := cached.(*regexp.Regexp)
	return re.MatchString(src)
}
