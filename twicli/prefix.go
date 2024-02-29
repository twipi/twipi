package twicli

import "strings"

// PrefixFunc returns true if the given message body string should activate the
// current command.
type PrefixFunc func(string) (string, bool)

// NewNaturalPrefix returns a PrefixFunc that matches the prefix of a message
// with the phrase "X, ", e.g. "Discord, message <1> ABC". The prefix is matched
// in a case-insensitive manner.
func NewNaturalPrefix(name string) PrefixFunc {
	prefix := strings.ToLower(name) + ","
	return func(msg string) (string, bool) {
		first, tail, err := PopFirstWord(msg)
		if err != nil {
			return "", false
		}

		if strings.ToLower(first) != prefix {
			return "", false
		}

		return tail, true
	}
}

// NewSlashPrefix returns a PrefixFunc that matches the prefix of a message with
// the phrase "/X ", e.g. "/message <1> ABC". The prefix is matched in a
// case-sensitive manner.
func NewSlashPrefix(name string) PrefixFunc {
	prefix := "/" + name
	return func(msg string) (string, bool) {
		first, tail, err := PopFirstWord(msg)
		if err != nil {
			return "", false
		}

		if first != prefix {
			return "", false
		}

		return tail, true
	}
}

// NewWordPrefix returns a PrefixFunc that matches the prefix of a message with
// a word.
func NewWordPrefix(word string, cased bool) PrefixFunc {
	return func(msg string) (string, bool) {
		first, tail, err := PopFirstWord(msg)
		if err != nil {
			return "", false
		}

		var ok bool
		if cased {
			ok = first == word
		} else {
			ok = strings.EqualFold(first, word)
		}

		return tail, ok
	}
}

// CombinePrefixes combines multiple PrefixFuncs into a single PrefixFunc. The
// returned PrefixFunc will return true if any of the given PrefixFuncs return
// true.
func CombinePrefixes(prefixes ...PrefixFunc) PrefixFunc {
	return func(msg string) (string, bool) {
		for _, prefix := range prefixes {
			if body, ok := prefix(msg); ok {
				return body, true
			}
		}
		return "", false
	}
}
