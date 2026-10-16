package ui

import (
	"reflect"
	"testing"
)

func TestGraphQLTokens(t *testing.T) {
	tests := []struct {
		line    string
		inBlock bool
		want    []jsonToken
		after   bool
	}{
		{`query Find($id: ID!) {`, false, []jsonToken{{0, 5, tokenLiteral}, {11, 3, tokenKey}}, false},
		{`  me: user(id: $id, first: 10, name: "a\"b") @include(if: true) {`, false, []jsonToken{
			{2, 2, tokenKey}, {11, 2, tokenKey}, {15, 3, tokenKey}, {20, 5, tokenKey}, {27, 2, tokenNumber},
			{31, 4, tokenKey}, {37, 6, tokenString}, {45, 8, tokenLiteral}, {54, 2, tokenKey}, {58, 4, tokenLiteral},
		}, false},
		{`  id # a "comment" with: words`, false, nil, false},
		{`fragment F on User { x2 }`, false, []jsonToken{{0, 8, tokenLiteral}, {11, 2, tokenLiteral}}, false},
		{`"""A block`, false, []jsonToken{{0, 10, tokenString}}, true},
		{`still in it`, true, []jsonToken{{0, 11, tokenString}}, true},
		{`ends""" type T`, true, []jsonToken{{0, 7, tokenString}, {8, 4, tokenLiteral}}, false},
		{`"""one line""" scalar D`, false, []jsonToken{{0, 14, tokenString}, {15, 6, tokenLiteral}}, false},
		{`{ a(x: {{id}}) }`, false, []jsonToken{{4, 1, tokenKey}}, false},
	}
	for _, tt := range tests {
		got, after := graphqlTokens(tt.line, tt.inBlock)
		if !reflect.DeepEqual(got, tt.want) || after != tt.after {
			t.Errorf("graphqlTokens(%q, %t) = %v, %t, want %v, %t", tt.line, tt.inBlock, got, after, tt.want, tt.after)
		}
	}
}
