package engine

import (
	"strings"

	"github.com/Rethinger/squoze/internal/profile"
)

// DistillUserToolContent inspects a user message content string.
// If it contains machine-generated tool outputs (such as <tool_output>,
// <command_output>, <command_result>, or fenced blocks like ```terminal / ```diff),
// it distills ONLY the machine content inside the wrapper, while strictly
// preserving all surrounding human prompt instructions.
func (e *Engine) distillUserContent(f profile.Family, text string) (string, bool) {
	if len(text) < 40 {
		return text, false
	}

	changed := false
	result := text

	// 1. Process XML tags: <tool_output>, <command_output>, <command_result>, <file_content>
	xmlTags := []string{"tool_output", "tool_result", "command_output", "command_result", "file_content"}
	for _, tag := range xmlTags {
		openPrefix := "<" + tag
		closeTag := "</" + tag + ">"

		searchPos := 0
		for {
			openIdx := strings.Index(result[searchPos:], openPrefix)
			if openIdx == -1 {
				break
			}
			openIdx += searchPos

			// Find closing '>' of opening tag
			tagEnd := strings.Index(result[openIdx:], ">")
			if tagEnd == -1 {
				break
			}
			contentStart := openIdx + tagEnd + 1

			closeIdx := strings.Index(result[contentStart:], closeTag)
			if closeIdx == -1 {
				break
			}
			contentEnd := contentStart + closeIdx

			inner := result[contentStart:contentEnd]
			distilled := e.distillText(f, inner)
			if distilled != "" && distilled != inner {
				result = result[:contentStart] + distilled + result[contentEnd:]
				changed = true
				searchPos = contentStart + len(distilled) + len(closeTag)
			} else {
				searchPos = contentEnd + len(closeTag)
			}
		}
	}

	// 2. Process fenced code blocks: ```terminal, ```output, ```diff
	fences := []string{"```terminal", "```output", "```diff"}
	for _, fence := range fences {
		searchPos := 0
		for {
			fenceIdx := strings.Index(result[searchPos:], fence)
			if fenceIdx == -1 {
				break
			}
			fenceIdx += searchPos

			// Find end of line after fence
			lineEnd := strings.Index(result[fenceIdx:], "\n")
			if lineEnd == -1 {
				break
			}
			contentStart := fenceIdx + lineEnd + 1

			closeFence := strings.Index(result[contentStart:], "\n```")
			if closeFence == -1 {
				break
			}
			contentEnd := contentStart + closeFence

			inner := result[contentStart:contentEnd]
			distilled := e.distillText(f, inner)
			if distilled != "" && distilled != inner {
				result = result[:contentStart] + distilled + result[contentEnd:]
				changed = true
				searchPos = contentStart + len(distilled) + 4
			} else {
				searchPos = contentEnd + 4
			}
		}
	}

	// 3. Whole-message tool output: when the entire user prompt is raw machine output
	if !changed && (strings.HasPrefix(strings.TrimSpace(result), "diff --git") ||
		strings.HasPrefix(strings.TrimSpace(result), "============================= test session starts") ||
		strings.HasPrefix(strings.TrimSpace(result), "=== RUN   ")) {
		distilled := e.distillText(f, result)
		if distilled != "" && distilled != result {
			return distilled, true
		}
	}

	return result, changed
}
