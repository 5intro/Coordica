package codequality

import (
	"context"
	"regexp"
	"strings"
)

var scriptFieldHeader = regexp.MustCompile(`(?:[A-Za-z_$][\w$]*|0)\s*[!?]?\s*$`)

// Only class-level annotations are types. Skip complete method bodies and value
// initializers while collecting regions, preserving executable initializer arrows.
func scriptPropertyTypeRegions(ctx context.Context, text string, ends map[int]int) []scriptTypeRegion {
	var regions []scriptTypeRegion
	for _, match := range classBlock.FindAllStringSubmatchIndex(text, -1) {
		open := scriptClassBody(text, match[1], ends)
		close, ok := ends[open]
		if !ok {
			continue
		}
		header := open + 1
		for at := open + 1; at < close; {
			if ctx != nil && ctx.Err() != nil {
				return nil
			}
			if end, ok := ends[at]; ok {
				at = end + 1
				continue
			}
			if text[at] == '=' && !strings.HasPrefix(text[at:], "=>") {
				at = max(at+1, scriptExpressionEnd(text, text, scriptSpace(text, at+1), ends)+1)
				header = at
				continue
			}
			if text[at] == ':' && scriptFieldHeader.MatchString(text[header:at]) {
				end := min(close, scriptAnnotationEnd(text, at+1, ends), scriptExpressionEnd(text, text, scriptSpace(text, at+1), ends)+1)
				regions = append(regions, scriptTypeRegion{at + 1, end})
				at = max(at+1, end)
				continue
			}
			if text[at] == ';' || text[at] == '\n' {
				header = at + 1
			}
			at++
		}
	}
	return regions
}
