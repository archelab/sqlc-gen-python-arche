package core

import (
	"bufio"
	"fmt"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"strings"
	"unicode"
	"unicode/utf8"
)

func ModelName(enumName string, schemaName string, conf *Config) string {
	if schemaName != "" {
		enumName = schemaName + "_" + enumName
	}
	return SnakeToCamel(enumName, conf)
}

func SnakeToCamel(s string, conf *Config) string {
	out := ""
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) {
			return r
		}
		if unicode.IsDigit(r) {
			return r
		}
		return rune('_')
	}, s)
	for _, p := range strings.Split(s, "_") {
		if _, found := conf.InitialismsMap[p]; found {
			out += strings.ToUpper(p)
		} else {
			out += cases.Title(language.Und, cases.NoLower).String(p)
		}
	}
	r, _ := utf8.DecodeRuneInString(out)
	if unicode.IsDigit(r) {
		return "_" + out
	} else {
		return out
	}
}

func ColumnName(c *plugin.Column, pos int) string {
	if c.Name != "" {
		return c.Name
	}
	return fmt.Sprintf("column_%d", pos+1)
}

func ParamName(p *plugin.Parameter) string {
	if p.Column.Name != "" {
		return p.Column.Name
	}
	return fmt.Sprintf("dollar_%d", p.Number)
}

func UpperSnakeCase(s string) string {
	result := ""
	for i, r := range s {
		if unicode.IsUpper(r) && i != 0 {
			result += "_" + string(r)
		} else {
			result += string(r)
		}
	}
	result = strings.ToUpper(result)
	return result
}

func SQLToPyFileName(s string) string {
	return strings.ReplaceAll(s, ".sql", ".py")
}

func SplitLines(s string) []string {
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

func IsInMultipleMaps[K comparable, V any](search K, maps ...map[K]V) bool {
	for _, m := range maps {
		if _, found := m[search]; found {
			return true
		}
	}
	return false
}

// NameDisambiguator single-sources the suffix/`seen` collision rule that
// disambiguates field/arg names sharing a base name. It was previously
// inlined only in columnsToStruct; extracting it lets the flat multi-param arg
// loop reuse the IDENTICAL rule (two params named `status` -> `status` /
// `status_2`). Names referring to the SAME numbered placeholder (same id) reuse
// the first chosen suffix; a named param is never suffixed.
type NameDisambiguator struct {
	seen     map[string][]int
	suffixes map[int]int
}

func NewNameDisambiguator() *NameDisambiguator {
	return &NameDisambiguator{seen: map[string][]int{}, suffixes: map[int]int{}}
}

// Next returns the disambiguated name for base name `base` at sequence index
// `i`, keyed by placeholder id `id`. `isNamedParam` and `useID` mirror
// columnsToStruct's two guards exactly.
func (d *NameDisambiguator) Next(base string, i int, id int, isNamedParam bool, useID bool) string {
	suffix := 0
	if o, ok := d.suffixes[id]; ok && useID {
		suffix = o
	} else if v := len(d.seen[base]); v > 0 && !isNamedParam {
		suffix = v + 1
	}
	d.suffixes[id] = suffix
	name := base
	if suffix > 0 {
		name = fmt.Sprintf("%s_%d", base, suffix)
	}
	if _, found := d.seen[base]; !found {
		d.seen[base] = []int{i}
	} else {
		d.seen[base] = append(d.seen[base], i)
	}
	return name
}

// SQLRootIsDML reports whether the SQL statement's root command is a
// data-modifying statement (INSERT/UPDATE/DELETE). It is used by the SQLAlchemy
// :many emitter to choose `conn.execute(...).all()` over `conn.stream(...)`:
// Postgres rejects a server-side cursor (stream) for a DML+RETURNING statement,
// so a :many over INSERT/UPDATE/DELETE ... RETURNING must materialize the rows
// eagerly. Leading whitespace and SQL line (`--`) / block (`/* */`) comments
// are skipped before reading the first keyword.
func SQLRootIsDML(sql string) bool {
	s := sql
	for {
		s = strings.TrimLeft(s, " \t\r\n")
		if strings.HasPrefix(s, "--") {
			if nl := strings.IndexByte(s, '\n'); nl >= 0 {
				s = s[nl+1:]
				continue
			}
			return false
		}
		if strings.HasPrefix(s, "/*") {
			if end := strings.Index(s, "*/"); end >= 0 {
				s = s[end+2:]
				continue
			}
			return false
		}
		break
	}
	upper := strings.ToUpper(s)
	for _, kw := range []string{"INSERT", "UPDATE", "DELETE"} {
		if strings.HasPrefix(upper, kw) {
			// Guard against an identifier that merely starts with the keyword
			// (e.g. `updates`): the keyword must be followed by a non-word char
			// or end-of-string.
			if len(upper) == len(kw) {
				return true
			}
			next := upper[len(kw)]
			if !(next == '_' || (next >= 'A' && next <= 'Z') || (next >= '0' && next <= '9')) {
				return true
			}
		}
	}
	// A writable CTE roots on WITH yet modifies data:
	// `WITH del AS (DELETE FROM t ... RETURNING ...) SELECT ... FROM del`.
	// Postgres rejects a server-side cursor (stream) over a data-modifying
	// statement, so such a :many must materialize eagerly just like a bare
	// DML+RETURNING. Reading only the root keyword (WITH) misses this, so when
	// the root is WITH, scan the CTE bodies for a data-modifying keyword. A
	// read-only CTE (only SELECT) has none and stays non-DML. Biasing toward
	// detecting DML here is safe: a false positive only makes a :many eager
	// rather than streamed (still correct); a false negative is a runtime crash.
	if strings.HasPrefix(upper, "WITH") && containsDMLKeyword(upper) {
		return true
	}
	return false
}

// containsDMLKeyword reports whether INSERT/UPDATE/DELETE appears as a
// standalone word anywhere in upper (an already-uppercased SQL string). Word
// boundaries keep identifiers like `deleted_at` from matching.
func containsDMLKeyword(upper string) bool {
	isWord := func(b byte) bool {
		return b == '_' || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
	}
	for _, kw := range []string{"INSERT", "UPDATE", "DELETE"} {
		from := 0
		for {
			idx := strings.Index(upper[from:], kw)
			if idx < 0 {
				break
			}
			at := from + idx
			beforeOK := at == 0 || !isWord(upper[at-1])
			end := at + len(kw)
			afterOK := end == len(upper) || !isWord(upper[end])
			if beforeOK && afterOK {
				return true
			}
			from = at + 1
		}
	}
	return false
}
