package core

import (
	"strings"
	"testing"
)

// readPyTripleQuoted reads `"""` + encoded + `"""` the way Python reads a `"""`
// string that is not raw, for the escapes PyTripleQuotedText writes. It fails
// on any other escape, on a CR or NUL in the source, and on a `"""` that ends
// the string before the closing delimiter.
func readPyTripleQuoted(t *testing.T, encoded string) string {
	t.Helper()
	source := encoded + `"""`
	var out strings.Builder
	for i := 0; i < len(source); i++ {
		switch c := source[i]; {
		case c == '\r' || c == 0:
			t.Fatalf("%q: Python can not keep the byte %q in source at %d", encoded, c, i)
		case c == '\\':
			switch {
			case strings.HasPrefix(source[i:], `\\`):
				out.WriteByte('\\')
			case strings.HasPrefix(source[i:], `\"`):
				out.WriteByte('"')
			case strings.HasPrefix(source[i:], `\r`):
				out.WriteByte('\r')
			case strings.HasPrefix(source[i:], `\x00`):
				out.WriteByte(0)
				i += 2
			default:
				t.Fatalf("%q: a backslash that Python reads as another escape at %d", encoded, i)
			}
			i++
		case strings.HasPrefix(source[i:], `"""`):
			if i != len(encoded) {
				t.Fatalf("%q: an unescaped \"\"\" ends the string at %d", encoded, i)
			}
			return out.String()
		default:
			out.WriteByte(c)
		}
	}
	t.Fatalf("%q: the closing \"\"\" was consumed", encoded)
	return ""
}

func TestPyTripleQuotedText(t *testing.T) {
	cases := map[string]string{
		`SELECT 1`:            `SELECT 1`,
		`E'\n'`:               `E'\\n'`,
		`E'\u001f'`:           `E'\\u001f'`,
		`'^(4|5|6)\.'`:        `'^(4|5|6)\\.'`,
		`ESCAPE '\'`:          `ESCAPE '\\'`,
		"a line end \\\nnext": "a line end \\\\\nnext",
		"a\rb":                `a\rb`,
		"a\x00b":              `a\x00b`,
		`"""`:                 `""\"`,
		`""""""`:              `""\"""\"`,
		`\"""`:                `\\""\"`,
		`"a""b"`:              `"a""b\"`,
		`say "hi"`:            `say "hi\"`,
		`""`:                  `"\"`,
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

// TestPyTripleQuotedTextReadsBackExactly checks every string of up to 6
// characters over the characters that matter, with the closing `"""` right
// after it: Python must read the encoded text back as the input.
func TestPyTripleQuotedTextReadsBackExactly(t *testing.T) {
	alphabet := []string{`\`, `"`, "n", ":", "\n", "\r", "\x00"}
	inputs := []string{""}
	for length := 1; length <= 6; length++ {
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

func TestPyStringLiteral(t *testing.T) {
	cases := map[string]string{
		"pending":    `"pending"`,
		`back\slash`: `"back\\slash"`,
		`say "hi"`:   `"say \"hi\""`,
		"two\nlines": `"two\nlines"`,
		"é":          `"é"`,
	}
	for in, want := range cases {
		if got := PyStringLiteral(in); got != want {
			t.Errorf("PyStringLiteral(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestPyCommentLines(t *testing.T) {
	got := PyCommentLines("one\ntwo\r\nthree\rfour")
	want := []string{"one", "two", "three", "four"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("PyCommentLines = %q, want %q", got, want)
	}
}
