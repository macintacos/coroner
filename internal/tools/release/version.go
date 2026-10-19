package main

import (
	"cmp"
	"fmt"
	"regexp"
	"strconv"
)

// versionTag matches the only tags the tool does version math on: vX.Y.Z and vX.Y.Z-rc.N.
var versionTag = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)(?:-rc\.(\d+))?$`)

type version struct {
	major, minor, patch int
	rc                  int // -1 on a final version
}

func parseTag(tag string) (version, bool) {
	m := versionTag.FindStringSubmatch(tag)
	if m == nil {
		return version{}, false
	}

	v := version{rc: -1}
	v.major, _ = strconv.Atoi(m[1])
	v.minor, _ = strconv.Atoi(m[2])
	v.patch, _ = strconv.Atoi(m[3])
	if m[4] != "" {
		v.rc, _ = strconv.Atoi(m[4])
	}

	return v, true
}

func (v version) String() string {
	s := fmt.Sprintf("v%d.%d.%d", v.major, v.minor, v.patch)
	if v.rc >= 0 {
		s += fmt.Sprintf("-rc.%d", v.rc)
	}

	return s
}

func (v version) core() version { return version{v.major, v.minor, v.patch, -1} }

func compareCore(a, b version) int {
	return cmp.Or(cmp.Compare(a.major, b.major), cmp.Compare(a.minor, b.minor), cmp.Compare(a.patch, b.patch))
}

// latestFinal returns the highest final version among tags, and false when there is none.
func latestFinal(tags []string) (version, bool) {
	var latest version
	found := false
	for _, tag := range tags {
		v, ok := parseTag(tag)
		if ok && v.rc < 0 && (!found || compareCore(v, latest) > 0) {
			latest, found = v, true
		}
	}

	return latest, found
}

// nextTag bumps the latest final tag among tags, or v0.0.0 when there is none; with rc it
// appends -rc.N, one past the highest existing rc of that version.
func nextTag(tags []string, bump string, rc bool) (string, error) {
	latest, _ := latestFinal(tags)

	var next version
	switch bump {
	case "major":
		next = version{latest.major + 1, 0, 0, -1}
	case "minor":
		next = version{latest.major, latest.minor + 1, 0, -1}
	case "patch":
		next = version{latest.major, latest.minor, latest.patch + 1, -1}
	default:
		return "", fmt.Errorf("unknown bump %q", bump)
	}

	if rc {
		next.rc = 0
		for _, tag := range tags {
			v, ok := parseTag(tag)
			if ok && v.rc >= next.rc && v.core() == next.core() {
				next.rc = v.rc + 1
			}
		}
	}

	return next.String(), nil
}
