package codequality

import "strings"

type pythonContinuation struct {
	depth   int
	escaped bool
}

func (c pythonContinuation) active() bool { return c.depth > 0 || c.escaped }

func (c *pythonContinuation) advance(line string) {
	for _, char := range line {
		switch char {
		case '(', '[', '{':
			c.depth++
		case ')', ']', '}':
			c.depth--
		}
	}
	c.escaped = strings.HasSuffix(strings.TrimRight(line, " \t"), `\`)
}
