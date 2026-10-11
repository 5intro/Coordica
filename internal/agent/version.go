package agent

import (
	"regexp"
	"strconv"
)

var semverText = regexp.MustCompile(`(?:^|[^0-9])([0-9]+)\.([0-9]+)\.([0-9]+)(?:[^0-9]|$)`)

func versionAtLeast(version, minimum string) bool {
	v, m := semverText.FindStringSubmatch(version), semverText.FindStringSubmatch(minimum)
	if len(v) != 4 || len(m) != 4 {
		return false
	}
	for i := 1; i < 4; i++ {
		a, ea := strconv.Atoi(v[i])
		b, eb := strconv.Atoi(m[i])
		if ea != nil || eb != nil {
			return false
		}
		if a != b {
			return a > b
		}
	}
	return true
}
