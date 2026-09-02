package distill

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

// IsLockfileOrGenerated reports whether a path matches generated files,
// dependency lockfiles, or minified assets that pollute agent context.
func IsLockfileOrGenerated(path string) bool {
	base := filepath.Base(path)
	lower := strings.ToLower(base)

	lockfiles := map[string]bool{
		"package-lock.json": true,
		"pnpm-lock.yaml":    true,
		"yarn.lock":         true,
		"go.sum":            true,
		"cargo.lock":        true,
		"poetry.lock":       true,
		"composer.lock":     true,
		"gemfile.lock":      true,
		"flake.lock":        true,
	}
	if lockfiles[lower] {
		return true
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".map" || strings.HasSuffix(lower, ".min.js") || strings.HasSuffix(lower, ".min.css") {
		return true
	}
	return false
}

// IsUnifiedDiff reports whether text appears to be a unified git diff.
func IsUnifiedDiff(s string) bool {
	return strings.Contains(s, "diff --git ") ||
		(strings.Contains(s, "--- a/") && strings.Contains(s, "+++ b/")) ||
		(strings.Contains(s, "--- ") && strings.Contains(s, "+++ ") && strings.Contains(s, "@@ -"))
}

// DistillDiff processes git unified diffs:
// 1. Collapses massive lockfile and minified asset diffs into 1-line semantic summaries.
// 2. Compacts excessive unchanged context lines (>8 lines) in application code diffs down to 3 lines.
// 3. Real code modifications are preserved 100% verbatim.
func DistillDiff(s string) (string, bool) {
	if !IsUnifiedDiff(s) || len(s) < 512 {
		return s, false
	}

	lines := strings.Split(s, "\n")
	var out []string
	var currentFileSection []string
	currentFilePath := ""
	changed := false

	flushFileSection := func() {
		if len(currentFileSection) == 0 {
			return
		}
		if currentFilePath != "" && IsLockfileOrGenerated(currentFilePath) && len(currentFileSection) > 20 {
			added := 0
			removed := 0
			for _, l := range currentFileSection {
				if strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++") {
					added++
				} else if strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---") {
					removed++
				}
			}

			fullSectionText := strings.Join(currentFileSection, "\n")
			h := sha256.Sum256([]byte(fullSectionText))
			ref := hex.EncodeToString(h[:])[:12]

			summary := fmt.Sprintf("[... squoze: %d lines of %s diff elided (+%d/-%d) · ref %s ...]",
				len(currentFileSection), currentFilePath, added, removed, ref)
			out = append(out, summary)
			changed = true
		} else {
			// Real application source code: compact large unchanged context blocks
			compacted := compactUnchangedContext(currentFileSection)
			if len(compacted) != len(currentFileSection) {
				changed = true
			}
			out = append(out, compacted...)
		}
		currentFileSection = nil
		currentFilePath = ""
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			flushFileSection()
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				// Format: diff --git a/path b/path
				bPath := parts[3]
				currentFilePath = strings.TrimPrefix(bPath, "b/")
			}
			currentFileSection = append(currentFileSection, line)
		} else if strings.HasPrefix(line, "+++ b/") && currentFilePath == "" {
			currentFilePath = strings.TrimPrefix(line, "+++ b/")
			currentFileSection = append(currentFileSection, line)
		} else {
			currentFileSection = append(currentFileSection, line)
		}
	}
	flushFileSection()

	if !changed {
		return s, false
	}
	return strings.Join(out, "\n"), true
}

// compactUnchangedContext shrinks consecutive unchanged lines (>8 lines) to 3 lines.
func compactUnchangedContext(lines []string) []string {
	if len(lines) < 15 {
		return lines
	}

	var res []string
	var contextBlock []string

	flushContext := func() {
		if len(contextBlock) > 8 {
			// Keep first 2, skip middle, keep last 2
			res = append(res, contextBlock[0], contextBlock[1])
			res = append(res, fmt.Sprintf("    [... squoze: %d unchanged context lines elided ...]", len(contextBlock)-4))
			res = append(res, contextBlock[len(contextBlock)-2], contextBlock[len(contextBlock)-1])
		} else {
			res = append(res, contextBlock...)
		}
		contextBlock = nil
	}

	for _, line := range lines {
		// In unified diffs, unchanged context lines start with a single space ' '
		if len(line) > 0 && line[0] == ' ' {
			contextBlock = append(contextBlock, line)
		} else {
			flushContext()
			res = append(res, line)
		}
	}
	flushContext()

	return res
}
