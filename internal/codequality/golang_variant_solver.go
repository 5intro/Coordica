package codequality

import (
	"context"
	"fmt"
	"go/build/constraint"
	"sort"
)

const maxGoBuildEvaluations = 1000000

type buildMethodGroup struct {
	condition constraint.Expr
	count     int
}
type receiverVariantSolver struct {
	ctx               context.Context
	before, after     []buildMethodGroup
	variables         []string
	remaining, best   int
	regression, found bool
}

func groupedReceiverMethods(methods []receiverMethod) []buildMethodGroup {
	groups := []buildMethodGroup{}
	indexes := map[string]int{}
	for _, method := range methods {
		key := ""
		if method.condition != nil {
			key = method.condition.String()
		}
		index, ok := indexes[key]
		if !ok {
			index = len(groups)
			indexes[key] = index
			groups = append(groups, buildMethodGroup{condition: method.condition})
		}
		groups[index].count++
	}
	return groups
}

func solveReceiverVariants(ctx context.Context, before, after []receiverMethod, regression bool) (int, bool, error) {
	if len(after) <= MaxMethods {
		return len(after), false, nil
	}
	s := receiverVariantSolver{ctx: ctx, before: groupedReceiverMethods(before), after: groupedReceiverMethods(after),
		remaining: maxGoBuildEvaluations, regression: regression}
	if regression && receiverConditionsDominate(s.before, s.after) {
		return 0, false, nil
	}
	tags := map[string]bool{}
	for _, group := range append(append([]buildMethodGroup{}, s.before...), s.after...) {
		buildConditionTags(group.condition, tags)
	}
	for tag := range tags {
		if !fixedGoTag(tag) {
			s.variables = append(s.variables, tag)
		}
	}
	sort.Strings(s.variables)
	err := goBuildEnvironments(tags, func(env map[string]bool) (bool, error) {
		if err := s.heuristics(env); err != nil {
			return false, err
		}
		if s.found || !regression && s.best == len(after) {
			return true, nil
		}
		if err := s.walk(env, 0); err != nil {
			return false, err
		}
		return s.found, nil
	})
	return s.best, s.found, err
}

func receiverConditionsDominate(before, after []buildMethodGroup) bool {
	counts := map[string]int{}
	key := func(expr constraint.Expr) string {
		if expr == nil {
			return ""
		}
		return expr.String()
	}
	for _, group := range before {
		counts[key(group.condition)] += group.count
	}
	for _, group := range after {
		if group.count > counts[key(group.condition)] {
			return false
		}
	}
	return true
}

func (s *receiverVariantSolver) heuristics(env map[string]bool) error {
	for _, value := range []bool{true, false} {
		for _, tag := range s.variables {
			env[tag] = value
		}
		_, _, err := s.bounds(env)
		for _, tag := range s.variables {
			delete(env, tag)
		}
		if err != nil {
			return err
		}
		if s.found {
			return nil
		}
	}
	return nil
}

func (s *receiverVariantSolver) walk(env map[string]bool, index int) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	lower, upper, err := s.bounds(env)
	if err != nil || s.found {
		return err
	}
	if upper <= s.best && !s.regression || s.regression && upper <= MaxMethods {
		return nil
	}
	if lower == upper || index == len(s.variables) {
		return nil
	}
	tag := s.variables[index]
	defer delete(env, tag)
	for _, value := range []bool{true, false} {
		env[tag] = value
		if err := s.walk(env, index+1); err != nil {
			return err
		}
		if s.found {
			return nil
		}
	}
	return nil
}

func (s *receiverVariantSolver) bounds(env map[string]bool) (int, int, error) {
	aLow, aHigh, err := s.count(s.after, env)
	if err != nil {
		return 0, 0, err
	}
	if !s.regression {
		if aLow == aHigh {
			s.best = max(s.best, aLow)
		}
		return aLow, aHigh, nil
	}
	bLow, bHigh, err := s.count(s.before, env)
	if err != nil {
		return 0, 0, err
	}
	if aHigh <= MaxMethods || aHigh <= bLow {
		return 0, 0, nil
	}
	if aLow == aHigh && bLow == bHigh && aLow > MaxMethods && aLow > bLow {
		s.best, s.found = aLow, true
	}
	return 0, aHigh, nil
}

func (s *receiverVariantSolver) count(groups []buildMethodGroup, env map[string]bool) (int, int, error) {
	lower, upper := 0, 0
	for _, group := range groups {
		value, err := s.eval(group.condition, env)
		if err != nil {
			return 0, 0, err
		}
		if value >= 0 {
			upper += group.count
		}
		if value > 0 {
			lower += group.count
		}
	}
	return lower, upper, nil
}

func (s *receiverVariantSolver) eval(expr constraint.Expr, env map[string]bool) (int, error) {
	if err := s.ctx.Err(); err != nil {
		return 0, err
	}
	s.remaining--
	if s.remaining < 0 {
		return 0, &SourceError{Cause: fmt.Errorf("Go build variant analysis exceeds %d evaluations", maxGoBuildEvaluations)}
	}
	switch x := expr.(type) {
	case nil:
		return 1, nil
	case *constraint.TagExpr:
		value, known := env[x.Tag]
		if !known {
			return 0, nil
		}
		if value {
			return 1, nil
		}
		return -1, nil
	case *constraint.NotExpr:
		value, err := s.eval(x.X, env)
		return -value, err
	case *constraint.AndExpr:
		left, err := s.eval(x.X, env)
		if err != nil || left < 0 {
			return left, err
		}
		right, err := s.eval(x.Y, env)
		return min(left, right), err
	case *constraint.OrExpr:
		left, err := s.eval(x.X, env)
		if err != nil || left > 0 {
			return left, err
		}
		right, err := s.eval(x.Y, env)
		return max(left, right), err
	}
	return 0, &SourceError{Cause: fmt.Errorf("unsupported Go build constraint")}
}
