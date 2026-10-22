package codequality

import (
	"context"
	"regexp"
	"sort"
	"strings"
)

func scriptClassName(ctx context.Context, original, text string, match []int, open int, ends map[int]int) string {
	if match[2] >= 0 && text[match[2]:match[3]] != "extends" {
		return text[match[2]:match[3]]
	}
	return "anonymous:" + scriptIdentity(ctx, original, text, match[0], open, ends)
}

type scriptTypeRegion struct{ start, end int }

var typeAlias = regexp.MustCompile(`\btype\s+[A-Za-z_$][\w$]*`)
var typeInterface = regexp.MustCompile(`\binterface\s+[A-Za-z_$][\w$]*[^;{}]*\{`)
var variableType = regexp.MustCompile(`\b(?:const|let|var)\s+[A-Za-z_$][\w$]*\s*:`)

// Preserve executable arrows inside values while excluding explicitly typed
// declarations. A type region ends before a variable's value initializer.
func scriptTypeRegions(ctx context.Context, text string, ends map[int]int) []scriptTypeRegion {
	var regions []scriptTypeRegion
	for _, match := range typeAlias.FindAllStringIndex(text, -1) {
		if ctx != nil && ctx.Err() != nil {
			return nil
		}
		at := scriptSpace(text, match[1])
		if at < len(text) && text[at] == '<' {
			at = scriptTypeArguments(text, at, ends)
			if at < 0 {
				continue
			}
			at = scriptSpace(text, at)
		}
		if at < len(text) && text[at] == '=' {
			regions = append(regions, scriptTypeRegion{at, scriptExpressionEnd(text, text, scriptSpace(text, at+1), ends)})
		}
	}
	for _, match := range typeInterface.FindAllStringIndex(text, -1) {
		if ctx != nil && ctx.Err() != nil {
			return nil
		}
		if end, ok := ends[match[1]-1]; ok {
			regions = append(regions, scriptTypeRegion{match[0], end})
		}
	}
	for _, match := range variableType.FindAllStringIndex(text, -1) {
		if ctx != nil && ctx.Err() != nil {
			return nil
		}
		regions = append(regions, scriptTypeRegion{match[1], scriptAnnotationEnd(text, match[1], ends)})
	}
	sort.Slice(regions, func(i, j int) bool { return regions[i].start < regions[j].start })
	merged := []scriptTypeRegion{}
	for _, region := range regions {
		if len(merged) > 0 && region.start <= merged[len(merged)-1].end {
			merged[len(merged)-1].end = max(merged[len(merged)-1].end, region.end)
		} else {
			merged = append(merged, region)
		}
	}
	return merged
}

func scriptAnnotationEnd(text string, start int, ends map[int]int) int {
	for at := start; at < len(text); at++ {
		if close, ok := ends[at]; ok {
			at = close
		} else if text[at] == ';' || text[at] == '=' && !strings.HasPrefix(text[at:], "=>") {
			return at
		}
	}
	return len(text)
}

func inScriptType(regions []scriptTypeRegion, at int) bool {
	i := sort.Search(len(regions), func(i int) bool { return regions[i].end >= at })
	return i < len(regions) && regions[i].start <= at
}
