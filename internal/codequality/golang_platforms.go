package codequality

import (
	"sort"
	"strconv"
	"strings"
)

// Supported Go 1.27 target pairs. This is source analysis, not an OS test claim.
const goBuildPairs = "aix/ppc64 android/386 android/amd64 android/arm android/arm64 darwin/amd64 darwin/arm64 " +
	"dragonfly/amd64 freebsd/386 freebsd/amd64 freebsd/arm freebsd/arm64 illumos/amd64 ios/amd64 ios/arm64 js/wasm " +
	"linux/386 linux/amd64 linux/arm linux/arm64 linux/loong64 linux/mips linux/mips64 linux/mips64le linux/mipsle " +
	"linux/ppc64 linux/ppc64le linux/riscv64 linux/s390x netbsd/386 netbsd/amd64 netbsd/arm netbsd/arm64 " +
	"openbsd/386 openbsd/amd64 openbsd/arm openbsd/arm64 openbsd/ppc64 openbsd/riscv64 plan9/386 plan9/amd64 " +
	"plan9/arm solaris/amd64 wasip1/wasm windows/386 windows/amd64 windows/arm64"

func goReleaseTag(tag string) (int, bool) {
	if !strings.HasPrefix(tag, "go1.") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(tag, "go1."))
	return n, err == nil && n > 0
}

func fixedGoTag(tag string) bool {
	if _, ok := goReleaseTag(tag); ok {
		return true
	}
	if tag == "unix" || tag == "gc" || tag == "gccgo" {
		return true
	}
	for _, pair := range strings.Fields(goBuildPairs) {
		parts := strings.Split(pair, "/")
		if tag == parts[0] || tag == parts[1] {
			return true
		}
	}
	return false
}

func goBuildEnvironments(tags map[string]bool, visit func(map[string]bool) (bool, error)) error {
	fixed := []string{}
	releases := []int{0}
	for tag := range tags {
		if fixedGoTag(tag) {
			fixed = append(fixed, tag)
		}
		if n, ok := goReleaseTag(tag); ok {
			releases = append(releases, n)
		}
	}
	sort.Strings(fixed)
	sort.Ints(releases)
	seen := map[string]bool{}
	for _, pair := range strings.Fields(goBuildPairs) {
		parts := strings.Split(pair, "/")
		for _, compiler := range []string{"gc", "gccgo"} {
			for _, release := range releases {
				env := map[string]bool{}
				key := make([]byte, len(fixed))
				for i, tag := range fixed {
					env[tag] = goFixedTagValue(tag, parts[0], parts[1], compiler, release)
					if env[tag] {
						key[i] = 1
					}
				}
				if seen[string(key)] {
					continue
				}
				seen[string(key)] = true
				stop, err := visit(env)
				if err != nil {
					return err
				}
				if stop {
					return nil
				}
			}
		}
	}
	return nil
}

func goFixedTagValue(tag, os, arch, compiler string, release int) bool {
	if n, ok := goReleaseTag(tag); ok {
		return n <= release
	}
	if tag == os || tag == arch || tag == compiler {
		return true
	}
	if tag == "linux" && os == "android" || tag == "darwin" && os == "ios" || tag == "solaris" && os == "illumos" {
		return true
	}
	return tag == "unix" && goNameIn("aix android darwin dragonfly freebsd hurd illumos ios linux netbsd openbsd solaris", os)
}
