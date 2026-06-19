package codegen

import (
	"fmt"
	"strings"

	"github.com/archelab/sqlc-gen-python-arche/internal/codegen/builders"
	"github.com/archelab/sqlc-gen-python-arche/internal/core"
)

// buildPyEnum emits a PG ENUM as `class <Name>(enum.StrEnum):`. The
// enum.StrEnum base is unconditional — there is no use_str_enum knob; the fork
// always emits a StrEnum.
//
// This forward-port FIXES the four upstream sqlc-gen-python StrEnum defects:
//   - #47 PascalCase class names: the class name is the PascalCase enum type
//     (core.Enum.Name = SnakeToCamel(enum)), e.g. `OrderStatus`, NOT a snake or
//     lowercased name.
//   - #49 abnormal type names: members are named from the VALUE
//     (sanitized + uppercased, e.g. `PENDING`), NOT the prefixed
//     `OrderStatusPending`.
//   - #51 no Any values: members are plain string assignments, never typed
//     `Any`.
//   - #4 no double-quoted values: each value is a double-quoted Python string
//     literal (`PENDING = "pending"`).
func buildPyEnum(enum core.Enum, body *builders.IndentStringBuilder) error {
	body.WriteLine(fmt.Sprintf("class %s(enum.StrEnum):", enum.Name))
	if enum.Comment != "" {
		body.WriteIndentedLine(1, fmt.Sprintf(`"""%s"""`, enum.Comment))
	}
	if len(enum.Constants) == 0 {
		body.WriteIndentedLine(1, "pass")
		return nil
	}
	// Member identifiers are derived from the raw value (sanitize + uppercase),
	// so two values that differ only by case or by which non-identifier symbol
	// they use ("pending"/"PENDING", "a-b"/"a/b") collapse to the SAME Python
	// member name. In Python the second assignment silently shadows the first,
	// making the earlier value unreachable and round-trip wrong. There is no
	// safe automatic rename (which value keeps the bare name is arbitrary), so
	// fail loud and name the offending values rather than emit a lossy class.
	seen := make(map[string]string, len(enum.Constants))
	for _, c := range enum.Constants {
		member := enumMemberName(c.Value)
		if prev, found := seen[member]; found {
			return fmt.Errorf(
				"enum %q: values %q and %q both map to StrEnum member %q; "+
					"rename one value so the members are distinct",
				enum.Name, prev, c.Value, member,
			)
		}
		seen[member] = c.Value
		body.WriteIndentedLine(1, fmt.Sprintf(`%s = "%s"`, member, c.Value))
	}
	return nil
}

// enumMemberName derives a Python-safe StrEnum member identifier from a raw PG
// enum value: non-identifier runes become `_`, the result is uppercased, and a
// leading digit is prefixed with `_`. An empty/all-symbol value falls back to a
// stable placeholder.
func enumMemberName(value string) string {
	cleaned := core.EnumReplace(value)
	if cleaned == "" {
		return "_EMPTY"
	}
	name := strings.ToUpper(cleaned)
	if name[0] >= '0' && name[0] <= '9' {
		name = "_" + name
	}
	return name
}
