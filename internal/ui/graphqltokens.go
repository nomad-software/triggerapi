package ui

import "slices"

// graphqlKeywords are the words of GraphQL that are coloured as JSON's true,
// false and null are, which are among them.
var graphqlKeywords = []string{
	"query", "mutation", "subscription", "fragment", "on",
	"schema", "scalar", "type", "interface", "union", "enum", "input",
	"extend", "implements", "directive", "repeatable",
	"true", "false", "null",
}

// graphqlTokens finds the tokens to colour in a line of GraphQL, as the
// kinds of JSON's: a name followed by a colon (an alias, an argument, a
// field of a definition or a variable being defined) and a $variable as
// keys, strings, numbers, and keywords and @directives as literals. Other
// names, the fields and types, are left as they are, as is a # comment. A
// block string can go on over lines: inBlock says that the line starts
// inside one, and the result whether it ends inside one.
func graphqlTokens(line string, inBlock bool) ([]jsonToken, bool) {
	runes := []rune(line)
	var tokens []jsonToken
	add := tokenAdder(runes, &tokens)

	blockEnd := func(from int) (int, bool) {
		for i := from; i+2 < len(runes); i++ {
			if runes[i] == '\\' && runes[i+1] == '"' {
				i++
				continue
			}
			if runes[i] == '"' && runes[i+1] == '"' && runes[i+2] == '"' {
				return i + 3, true
			}
		}
		return len(runes), false
	}

	i := 0
	if inBlock {
		end, closed := blockEnd(0)
		add(0, end, tokenString)
		if !closed {
			return tokens, true
		}
		i = end
	}

	for i < len(runes) {
		switch r := runes[i]; {
		case r == '#':
			return tokens, false

		case r == '"' && i+2 < len(runes) && runes[i+1] == '"' && runes[i+2] == '"':
			end, closed := blockEnd(i + 3)
			add(i, end, tokenString)
			if !closed {
				return tokens, true
			}
			i = end

		case r == '"':
			end := stringEnd(runes, i)
			add(i, end, tokenString)
			i = end

		case r == '-' || (r >= '0' && r <= '9'):
			i = addNumber(runes, i, add)

		case r == '$' || r == '@' || isNameStart(r):
			start, end := i, i
			if !isNameStart(r) {
				end++
			}
			for end < len(runes) && isNameRune(runes[end]) {
				end++
			}
			switch word := string(runes[start:end]); {
			case r == '@':
				add(start, end, tokenLiteral)
			case r == '$' || colonAfter(runes, end):
				add(start, end, tokenKey)
			case slices.Contains(graphqlKeywords, word):
				add(start, end, tokenLiteral)
			}
			i = max(end, i+1)

		default:
			i++
		}
	}
	return tokens, false
}

func isNameStart(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isNameRune(r rune) bool {
	return isNameStart(r) || (r >= '0' && r <= '9')
}
