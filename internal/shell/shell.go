// Package shell reads a command line as a POSIX shell splits it into words.
// Nothing else of a shell is done: nothing is expanded or run.
package shell

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Split splits a command into its words, as a shell does: at spaces, tabs
// and line breaks, keeping together what is in single or double quotes, in
// $'...' quotes (with their escapes) or follows a backslash. A backslash at
// the end of a line joins it to the next.
func Split(command string) ([]string, error) {
	// A line break copied from Windows ends with a carriage return.
	command = strings.ReplaceAll(command, "\r\n", "\n")

	var words []string
	var word strings.Builder
	inWord := false
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '\\':
			if i+1 == len(runes) {
				return nil, errors.New("it ends with a backslash")
			}
			i++
			if runes[i] != '\n' {
				word.WriteRune(runes[i])
				inWord = true
			}
		case r == '\'':
			end := slices.Index(runes[i+1:], '\'')
			if end < 0 {
				return nil, errors.New("it has a ' that is not closed")
			}
			end += i + 1
			word.WriteString(string(runes[i+1 : end]))
			i, inWord = end, true
		case r == '"':
			end, err := doubleQuoted(runes, i+1, &word)
			if err != nil {
				return nil, err
			}
			i, inWord = end, true
		case r == '$' && i+1 < len(runes) && runes[i+1] == '\'':
			end, err := ansiQuoted(runes, i+2, &word)
			if err != nil {
				return nil, err
			}
			i, inWord = end, true
		case r == ' ' || r == '\t' || r == '\n':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, nil
}

// doubleQuoted writes what is in double quotes from start to word, and
// returns the index of the closing quote. In them a backslash escapes only
// $, `, ", a backslash and a line break, and is otherwise itself.
func doubleQuoted(runes []rune, start int, word *strings.Builder) (int, error) {
	for i := start; i < len(runes); i++ {
		switch r := runes[i]; {
		case r == '"':
			return i, nil
		case r == '\\' && i+1 < len(runes) && strings.ContainsRune("$`\"\\\n", runes[i+1]):
			i++
			if runes[i] != '\n' {
				word.WriteRune(runes[i])
			}
		default:
			word.WriteRune(r)
		}
	}
	return 0, errors.New(`it has a " that is not closed`)
}

// ansiQuoted writes what is in $'...' quotes from start to word, with its
// escapes replaced, and returns the index of the closing quote.
func ansiQuoted(runes []rune, start int, word *strings.Builder) (int, error) {
	simple := map[rune]string{
		'a': "\a", 'b': "\b", 'e': "\x1b", 'E': "\x1b", 'f': "\f", 'n': "\n",
		'r': "\r", 't': "\t", 'v': "\v", '\\': "\\", '\'': "'", '"': "\"", '?': "?",
	}
	// Bytes given by \x and octal escapes are gathered, as several can make
	// one character of UTF-8.
	var bytes []byte
	flush := func() {
		word.Write(bytes)
		bytes = bytes[:0]
	}
	// digits reads at most n digits of a base from i, returning the value
	// and how many there were.
	digits := func(i, n, base int) (uint64, int) {
		count := 0
		for count < n && i+count < len(runes) && isDigit(runes[i+count], base) {
			count++
		}
		if count == 0 {
			return 0, 0
		}
		value, _ := strconv.ParseUint(string(runes[i:i+count]), base, 32)
		return value, count
	}

	for i := start; i < len(runes); i++ {
		r := runes[i]
		if r == '\'' {
			flush()
			return i, nil
		}
		if r != '\\' || i+1 == len(runes) {
			flush()
			word.WriteRune(r)
			continue
		}
		i++
		e := runes[i]
		switch {
		case simple[e] != "":
			flush()
			word.WriteString(simple[e])
		case e == 'x':
			value, count := digits(i+1, 2, 16)
			if count == 0 {
				flush()
				word.WriteString(`\x`)
				continue
			}
			bytes = append(bytes, byte(value))
			i += count
		case e >= '0' && e <= '7':
			value, count := digits(i, 3, 8)
			bytes = append(bytes, byte(value))
			i += count - 1
		case e == 'u' || e == 'U':
			n := 4
			if e == 'U' {
				n = 8
			}
			value, count := digits(i+1, n, 16)
			flush()
			if count == 0 || !utf8.ValidRune(rune(value)) {
				word.WriteRune('\\')
				word.WriteRune(e)
				continue
			}
			word.WriteRune(rune(value))
			i += count
		default:
			flush()
			word.WriteRune('\\')
			word.WriteRune(e)
		}
	}
	return 0, errors.New("it has a $' that is not closed")
}

// isDigit reports if r is a digit of the base, which is at most 16.
func isDigit(r rune, base int) bool {
	_, err := strconv.ParseUint(string(r), base, 8)
	return err == nil
}
