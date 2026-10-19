package main

import (
	"cmp"
	"fmt"
	"regexp"
	"strconv"
)

// versionTag matches the only tags the tool does version math on: vX.Y.Z and vX.Y.Z-rc.N,
// without leading zeros, as semver and goreleaser require.
var versionTag = regexp.MustCompile(`^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-rc\.(0|[1-9]\d*))?$`)

// version is vX.Y.Z, or vX.Y.Z-rc.N when isRC; the zero value is v0.0.0.
type version struct {
	major, minor, patch int
	isRC                bool
	rc                  int
}

func parseTag(tag string) (version, bool) {
	m := versionTag.FindStringSubmatch(tag)
	if m == nil {
		return version{}, false
	}

	var v version
	v.major, _ = strconv.Atoi(m[1])
	v.minor, _ = strconv.Atoi(m[2])
	v.patch, _ = strconv.Atoi(m[3])
	if m[4] != "" {
		v.isRC = true
		v.rc, _ = strconv.Atoi(m[4])
	}

	return v, true
}

func (v version) String() string {
	s := fmt.Sprintf("v%d.%d.%d", v.major, v.minor, v.patch)
	if v.isRC {
		s += fmt.Sprintf("-rc.%d", v.rc)
	}

	return s
}

func (v version) core() version { return version{major: v.major, minor: v.minor, patch: v.patch} }

func compareCore(a, b version) int {
	return cmp.Or(cmp.Compare(a.major, b.major), cmp.Compare(a.minor, b.minor), cmp.Compare(a.patch, b.patch))
}

// latestFinal returns the highest final version among tags, and false when there is none.
func latestFinal(tags []string) (version, bool) {
	var latest version
	found := false
	for _, tag := range tags {
		v, ok := parseTag(tag)
		if ok && !v.isRC && (!found || compareCore(v, latest) > 0) {
			latest, found = v, true
		}
	}

	return latest, found
}

// bump turns a final version into the next one.
type bump func(version) version

func parseBump(name string) (bump, error) {
	switch name {
	case "major":
		return func(v version) version { return version{major: v.major + 1} }, nil
	case "minor":
		return func(v version) version { return version{major: v.major, minor: v.minor + 1} }, nil
	case "patch":
		return func(v version) version { return version{major: v.major, minor: v.minor, patch: v.patch + 1} }, nil
	}

	return nil, fmt.Errorf("unknown bump %q", name)
}

// nextTag bumps the latest final tag among tags, or v0.0.0 when there is none; with rc it
// appends -rc.N, one past the highest existing rc of that version.
func nextTag(tags []string, next bump, rc bool) string {
	latest, _ := latestFinal(tags)
	v := next(latest)

	if rc {
		v.isRC = true
		for _, tag := range tags {
			t, ok := parseTag(tag)
			if ok && t.isRC && t.rc >= v.rc && t.core() == v.core() {
				v.rc = t.rc + 1
			}
		}
	}

	return v.String()
}
