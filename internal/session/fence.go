package session

import (
	"fmt"
	"strings"
)

// FenceToolResult wraps tool output so the model treats it as data, not
// instructions. Any literal closing tag inside content is escaped so a
// model-controlled fake closing tag cannot break out of the fence,
// keeping the envelope exactly one block.
func FenceToolResult(tool, content string) string {
	escaped := strings.ReplaceAll(content, "</tool_result>", "<\\/tool_result>")
	return fmt.Sprintf("<tool_result tool=%q>\n%s\n</tool_result>", tool, escaped)
}

// FenceUntrusted wraps arbitrary untrusted data (repository context,
// project memory, retrieved evidence) with an explicit source label so
// the model treats it as external data rather than instructions.
func FenceUntrusted(label, content string) string {
	escaped := strings.ReplaceAll(content, "</untrusted>", "<\\/untrusted>")
	return fmt.Sprintf("<untrusted source=%q>\n%s\n</untrusted>", label, escaped)
}
