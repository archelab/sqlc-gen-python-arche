package core

import (
	"strings"
	"testing"
)

// readPyTripleQuoted reads encoded the way Python reads the inside of a `"""`
// string that is not raw, for the two escapes PyTripleQuotedText writes. It
// fails on any other escape and on a `"""` that would end the string early.
func readPyTripleQuoted(t *testing.T, encoded string) string {
	t.Helper()
	var out strings.Builder
	for i := 0; i < len(encoded); i++ {
		switch c := encoded[i]; {
		case c == '\\':
			if i+1 == len(encoded) || (encoded[i+1] != '\\' && encoded[i+1] != '"') {
				t.Fatalf("%q: a backslash that Python reads as another escape at %d", encoded, i)
			}
			i++
			out.WriteByte(encoded[i])
		case strings.HasPrefix(encoded[i:], `"""`):
			t.Fatalf("%q: an unescaped \"\"\" ends the string at %d", encoded, i)
		default:
			out.WriteByte(c)
		}
	}
	return out.String()
}

func TestPyTripleQuotedText(t *testing.T) {
	cases := map[string]string{
		`SELECT 1`:            `SELECT 1`,
		`E'\n'`:               `E'\\n'`,
		`E'\u001f'`:           `E'\\u001f'`,
		`'^(4|5|6)\.'`:        `'^(4|5|6)\\.'`,
		`ESCAPE '\'`:          `ESCAPE '\\'`,
		"a line end \\\nnext": "a line end \\\\\nnext",
		`"""`:                 `""\"`,
		`""""""`:              `""\"""\"`,
		`\"""`:                `\\""\"`,
		`"a""b"`:              `"a""b"`,
	}
	for in, want := range cases {
		got := PyTripleQuotedText(in)
		if got != want {
			t.Errorf("PyTripleQuotedText(%q) = %q, want %q", in, got, want)
		}
		if back := readPyTripleQuoted(t, got); back != in {
			t.Errorf("Python reads %q back as %q, want %q", got, back, in)
		}
	}
}

// TestPyTripleQuotedTextReadsBackExactly checks every string of up to 7
// characters over the characters that matter: Python must read the encoded
// text back as the input, and no `"""` may end the string early.
func TestPyTripleQuotedTextReadsBackExactly(t *testing.T) {
	alphabet := []string{`\`, `"`, "n", ":", "\n"}
	inputs := []string{""}
	for length := 1; length <= 7; length++ {
		next := make([]string, 0, len(inputs)*len(alphabet))
		for _, prefix := range inputs {
			for _, c := range alphabet {
				next = append(next, prefix+c)
			}
		}
		for _, in := range next {
			if back := readPyTripleQuoted(t, PyTripleQuotedText(in)); back != in {
				t.Fatalf("PyTripleQuotedText(%q) reads back as %q", in, back)
			}
		}
		inputs = next
	}
}
