package engine

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"

	"github.com/Rethinger/squoze/internal/distill"
	"github.com/Rethinger/squoze/internal/profile"
	"github.com/tidwall/gjson"
)

type replacement struct {
	start int
	end   int
	val   []byte
}

type toolTarget struct {
	turnIndex  int
	startIndex int
	rawLen     int
	content    string
	isUser     bool
}

// applyReplacements stitches original body and replacement slices into a new buffer.
// If replacements is empty, it returns the original slice untouched.
func applyReplacements(body []byte, reps []replacement) []byte {
	if len(reps) == 0 {
		return body
	}
	// Sort by start index defensively to ensure sequential stitching
	sort.Slice(reps, func(i, j int) bool {
		return reps[i].start < reps[j].start
	})

	var buf bytes.Buffer
	buf.Grow(len(body))
	last := 0
	for _, r := range reps {
		if r.start < last || r.end > len(body) || r.start > r.end {
			continue // defensive sanity check
		}
		buf.Write(body[last:r.start])
		buf.Write(r.val)
		last = r.end
	}
	buf.Write(body[last:])
	return buf.Bytes()
}

// processOpenAIChatFast scans messages in a single pass, runs cross-turn dedup,
// applies multi-stage distillers, and stitches the output in O(N) time.
func (e *Engine) processOpenAIChatFast(f profile.Family, body []byte, res Result) ([]byte, Result) {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return body, res
	}

	var targets []toolTarget
	turn := 0
	messages.ForEach(func(_, msg gjson.Result) bool {
		turn++
		role := msg.Get("role").String()
		if role != "tool" && role != "user" {
			return true
		}
		c := msg.Get("content")
		if c.Type == gjson.String && c.Index > 0 {
			targets = append(targets, toolTarget{
				turnIndex:  turn,
				startIndex: c.Index,
				rawLen:     len(c.Raw),
				content:    c.String(),
				isUser:     role == "user",
			})
		}
		return true
	})

	if len(targets) == 0 {
		return body, res
	}

	// Cross-Turn Stale Read Deduplication (only for tool targets)
	var toolBlocks []distill.BlockRecord
	var toolIndices []int
	for i, t := range targets {
		if !t.isUser {
			toolBlocks = append(toolBlocks, distill.BlockRecord{
				TurnIndex: t.turnIndex,
				Content:   t.content,
				Hash:      distill.ComputeFastHash(t.content),
				Ref:       distill.ComputeRef(t.content),
			})
			toolIndices = append(toolIndices, i)
		}
	}
	if len(toolBlocks) > 1 {
		deduped, changed := distill.DeduplicateHistoricalReads(toolBlocks)
		if changed {
			for j, origIdx := range toolIndices {
				targets[origIdx].content = deduped[j]
			}
		}
	}

	var reps []replacement
	for _, t := range targets {
		before := e.memo.Len()
		var out string
		if t.isUser {
			if distilled, ok := e.distillUserContent(f, t.content); ok {
				out = distilled
			}
		} else {
			out = e.distillText(f, t.content)
			if out == "" && strings.Contains(t.content, "[... squoze: earlier view") {
				out = t.content // dedup reference marker
			}
		}
		if out != "" && out != t.content {
			rawJSON, err := json.Marshal(out)
			if err == nil {
				reps = append(reps, replacement{
					start: t.startIndex,
					end:   t.startIndex + t.rawLen,
					val:   rawJSON,
				})
				if e.memo.Len() == before {
					res.MemoHits++
				}
				res.BlocksSqueezed++
			}
		}
	}

	if len(reps) > 0 {
		body = applyReplacements(body, reps)
	}
	res.Transforms = append(res.Transforms, "squoze_v2_distiller")
	return body, res
}

// processAnthropicFast scans messages in a single pass for tool_result blocks.
func (e *Engine) processAnthropicFast(f profile.Family, body []byte, res Result) ([]byte, Result) {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return body, res
	}

	var targets []toolTarget
	turn := 0
	messages.ForEach(func(_, msg gjson.Result) bool {
		turn++
		content := msg.Get("content")
		if !content.IsArray() {
			return true
		}
		content.ForEach(func(_, blk gjson.Result) bool {
			if blk.Get("type").String() != "tool_result" {
				return true
			}
			inner := blk.Get("content")
			switch {
			case inner.Type == gjson.String && inner.Index > 0:
				targets = append(targets, toolTarget{
					turnIndex:  turn,
					startIndex: inner.Index,
					rawLen:     len(inner.Raw),
					content:    inner.String(),
				})
			case inner.IsArray():
				inner.ForEach(func(_, item gjson.Result) bool {
					if item.Get("type").String() != "text" {
						return true
					}
					t := item.Get("text")
					if t.Type == gjson.String && t.Index > 0 {
						targets = append(targets, toolTarget{
							turnIndex:  turn,
							startIndex: t.Index,
							rawLen:     len(t.Raw),
							content:    t.String(),
						})
					}
					return true
				})
			}
			return true
		})
		return true
	})

	if len(targets) == 0 {
		return body, res
	}

	// Cross-Turn Stale Read Deduplication
	if len(targets) > 1 {
		blocks := make([]distill.BlockRecord, len(targets))
		for i, t := range targets {
			blocks[i] = distill.BlockRecord{
				TurnIndex: t.turnIndex,
				Content:   t.content,
				Hash:      distill.ComputeFastHash(t.content),
				Ref:       distill.ComputeRef(t.content),
			}
		}
		deduped, changed := distill.DeduplicateHistoricalReads(blocks)
		if changed {
			for i := range targets {
				targets[i].content = deduped[i]
			}
		}
	}

	var reps []replacement
	for _, t := range targets {
		before := e.memo.Len()
		out := e.distillText(f, t.content)
		if out == "" && strings.Contains(t.content, "[... squoze: earlier view") {
			out = t.content // dedup reference marker
		}
		if out != "" {
			rawJSON, err := json.Marshal(out)
			if err == nil {
				reps = append(reps, replacement{
					start: t.startIndex,
					end:   t.startIndex + t.rawLen,
					val:   rawJSON,
				})
				if e.memo.Len() == before {
					res.MemoHits++
				}
				res.BlocksSqueezed++
			}
		}
	}

	if len(reps) > 0 {
		body = applyReplacements(body, reps)
	}
	res.Transforms = append(res.Transforms, "squoze_v2_distiller")
	return body, res
}
