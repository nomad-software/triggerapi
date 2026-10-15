package ui

import (
	"maps"
	"unicode/utf16"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/settings"
)

// tokenKind is a kind of JSON token that is coloured.
type tokenKind int

const (
	tokenKey tokenKind = iota
	tokenString
	tokenNumber
	tokenLiteral
)

// The default colours of the tokens. jsonDark is used on a dark background
// and jsonLight, which holds the colours of the Monaco editor's light theme,
// on a light one.
var (
	jsonLight = map[tokenKind]string{
		tokenKey:     "#a31515",
		tokenString:  "#0451a5",
		tokenNumber:  "#098658",
		tokenLiteral: "#0451a5",
	}
	jsonDark = map[tokenKind]string{
		tokenKey:     "#9bdafc",
		tokenString:  "#a1b796",
		tokenNumber:  "#b0c8a4",
		tokenLiteral: "#a1b796",
	}
)

// tokenKinds are the kinds of token in the order they are offered in the
// settings, with what each is called there.
var tokenKinds = []struct {
	kind  tokenKind
	label string
	// setting returns the place in the settings that holds the colour the
	// user has chosen.
	setting func(*settings.JSONColours) *string
}{
	{tokenKey, "Keys", func(c *settings.JSONColours) *string { return &c.Key }},
	{tokenString, "Strings", func(c *settings.JSONColours) *string { return &c.String }},
	{tokenNumber, "Numbers", func(c *settings.JSONColours) *string { return &c.Number }},
	{tokenLiteral, "true, false and null", func(c *settings.JSONColours) *string { return &c.Literal }},
}

// defaultJSONColours returns the colours of the tokens for the background
// text is shown on: the dark set if it is dark and the light set if not.
func defaultJSONColours() map[tokenKind]string {
	if qt.QGuiApplication_Palette().ColorWithCr(qt.QPalette__Base).Lightness() < 128 {
		return jsonDark
	}
	return jsonLight
}

// customColour returns the place in the settings that holds the colour the
// user has chosen for a kind of token.
func customColour(custom *settings.JSONColours, kind tokenKind) *string {
	for _, k := range tokenKinds {
		if k.kind == kind {
			return k.setting(custom)
		}
	}
	return &custom.Literal
}

// jsonColours returns the colours of the tokens: those the user has chosen,
// and the defaults for the rest.
func jsonColours(custom settings.JSONColours) map[tokenKind]string {
	colours := maps.Clone(defaultJSONColours())
	for kind := range colours {
		if chosen := *customColour(&custom, kind); chosen != "" {
			colours[kind] = chosen
		}
	}
	return colours
}

// jsonToken is the position of a token in a line, in the UTF-16 code units
// Qt counts in.
type jsonToken struct {
	start, length int
	kind          tokenKind
}

// jsonTokens finds the tokens to colour in a line of JSON. A string cannot
// continue onto another line, so each line stands alone. Text that is not
// valid JSON is coloured as far as it can be made sense of.
func jsonTokens(line string) []jsonToken {
	runes := []rune(line)

	// offsets holds the position in UTF-16 code units of each rune, and of
	// the end of the line.
	offsets := make([]int, len(runes)+1)
	for i, r := range runes {
		offsets[i+1] = offsets[i] + utf16.RuneLen(r)
	}

	var tokens []jsonToken
	add := func(from, to int, kind tokenKind) {
		tokens = append(tokens, jsonToken{offsets[from], offsets[to] - offsets[from], kind})
	}

	for i := 0; i < len(runes); {
		switch r := runes[i]; {
		case r == '"':
			end := i + 1
			for end < len(runes) && runes[end] != '"' {
				if runes[end] == '\\' {
					end++
				}
				end++
			}
			end = min(end+1, len(runes))

			// A string followed by a colon is the key of a member.
			next := end
			for next < len(runes) && (runes[next] == ' ' || runes[next] == '\t') {
				next++
			}
			if next < len(runes) && runes[next] == ':' {
				add(i, end, tokenKey)
			} else {
				add(i, end, tokenString)
			}
			i = end

		case r == '-' || (r >= '0' && r <= '9'):
			end := i + 1
			for end < len(runes) && isNumberRune(runes[end]) {
				end++
			}
			if r != '-' || end > i+1 {
				add(i, end, tokenNumber)
			}
			i = end

		case r >= 'a' && r <= 'z':
			end := i + 1
			for end < len(runes) && runes[end] >= 'a' && runes[end] <= 'z' {
				end++
			}
			switch string(runes[i:end]) {
			case "true", "false", "null":
				add(i, end, tokenLiteral)
			}
			i = end

		default:
			i++
		}
	}

	return tokens
}

func isNumberRune(r rune) bool {
	return (r >= '0' && r <= '9') || r == '.' || r == 'e' || r == 'E' || r == '+' || r == '-'
}

// foldRegions finds the objects and arrays of a JSON text that span lines. It
// maps the number of the line each one opens on to the number of the line it
// closes on. If more than one opens on a line the outermost is given.
func foldRegions(text string) map[int]int {
	regions := map[int]int{}

	var open []int
	line := 0
	inString := false

	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case c == '\n':
			line++
			// A string cannot continue onto another line.
			inString = false
		case inString:
			if c == '\\' {
				i++
			} else if c == '"' {
				inString = false
			}
		case c == '"':
			inString = true
		case c == '{' || c == '[':
			open = append(open, line)
		case (c == '}' || c == ']') && len(open) > 0:
			start := open[len(open)-1]
			open = open[:len(open)-1]
			if line > start {
				regions[start] = line
			}
		}
	}

	return regions
}

// jsonFormats are the text formats of the JSON tokens.
type jsonFormats map[tokenKind]*qt.QTextCharFormat

// newJSONFormats returns formats in the default colours.
func newJSONFormats() jsonFormats {
	formats := jsonFormats{}
	for _, t := range tokenKinds {
		formats[t.kind] = qt.NewQTextCharFormat()
	}
	formats.setColours(defaultJSONColours())
	return formats
}

// setColours changes the colours of the formats. Text that has already been
// highlighted keeps its colours until it is highlighted again.
func (f jsonFormats) setColours(colours map[tokenKind]string) {
	for kind, format := range f {
		format.SetForeground(qt.NewQBrush3(qt.NewQColor6(colours[kind])))
	}
}

// apply colours the tokens in a block of text being highlighted.
func (f jsonFormats) apply(highlighter *qt.QSyntaxHighlighter, text string) {
	for _, token := range jsonTokens(text) {
		highlighter.SetFormat(token.start, token.length, f[token.kind])
	}
}
