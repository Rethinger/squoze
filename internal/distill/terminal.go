package distill

import (
	"fmt"
	"strings"
)

// StripANSI removes ANSI escape codes (CSI, OSC) from a string.
// Fast path: if no ESC byte (0x1b) is found, returns the input untouched without allocations.
func StripANSI(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}

	b := []byte(s)
	out := make([]byte, 0, len(b))
	i := 0
	n := len(b)

	for i < n {
		if b[i] == 0x1b && i+1 < n {
			// CSI sequence: ESC [ ... final_byte (0x40 to 0x7e)
			if b[i+1] == '[' {
				j := i + 2
				for j < n && (b[j] < 0x40 || b[j] > 0x7e) {
					j++
				}
				if j < n {
					i = j + 1
					continue
				}
			}
			// OSC sequence: ESC ] ... (BEL 0x07 or ST ESC \)
			if b[i+1] == ']' {
				j := i + 2
				for j < n && b[j] != 0x07 {
					if b[j] == 0x1b && j+1 < n && b[j+1] == '\\' {
						j++
						break
					}
					j++
				}
				if j < n {
					i = j + 1
					continue
				}
			}
		}
		out = append(out, b[i])
		i++
	}

	return string(out)
}

// CleanCarriageReturns resolves in-place line overwrites caused by terminal progress bars (\r).
// Standard CRLF (\r\n) is normalized to \n. Standalone \r keeps only the last segment before \n.
func CleanCarriageReturns(s string) string {
	if !strings.Contains(s, "\r") {
		return s
	}

	// First normalize standard CRLF
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.Contains(s, "\r") {
		return s
	}

	lines := strings.Split(s, "\n")
	for idx, line := range lines {
		if strings.Contains(line, "\r") {
			parts := strings.Split(line, "\r")
			// Keep the last non-empty overwrite segment
			last := ""
			for _, p := range parts {
				if strings.TrimSpace(p) != "" {
					last = p
				}
			}
			lines[idx] = last
		}
	}
	return strings.Join(lines, "\n")
}

// FoldRepeatedLines folds consecutive identical lines repeated >3 times into a single line with count.
func FoldRepeatedLines(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= 3 {
		return s
	}

	var out []string
	currLine := ""
	repeatCount := 0

	flush := func() {
		if repeatCount == 1 {
			out = append(out, currLine)
		} else if repeatCount <= 3 {
			for k := 0; k < repeatCount; k++ {
				out = append(out, currLine)
			}
		} else {
			out = append(out, currLine)
			out = append(out, fmt.Sprintf("    [... line repeated %d times ...]", repeatCount-1))
		}
	}

	for _, line := range lines {
		if line == currLine && strings.TrimSpace(line) != "" {
			repeatCount++
		} else {
			flush()
			currLine = line
			repeatCount = 1
		}
	}
	flush()

	return strings.Join(out, "\n")
}

// SanitizeTerminal runs the full terminal hygiene pipeline: ANSI stripping,
// progress overwrite resolution, and line folding.
func SanitizeTerminal(s string) (string, bool) {
	if len(s) < 64 {
		return s, false
	}
	origLen := len(s)
	cleaned := StripANSI(s)
	cleaned = CleanCarriageReturns(cleaned)
	cleaned = FoldRepeatedLines(cleaned)

	changed := len(cleaned) != origLen || cleaned != s
	return cleaned, changed
}
