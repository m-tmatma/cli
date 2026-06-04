package shared

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"

	"github.com/cli/cli/v2/internal/ghrepo"
)

var discussionURLRE = regexp.MustCompile(`^/([^/]+)/([^/]+)/discussions/(\d+)$`)

// ParseDiscussionArg parses a discussion number or URL from a command argument.
// It returns the discussion number and, if the argument was a URL, a repo override.
func ParseDiscussionArg(arg string) (int32, ghrepo.Interface, error) {
	if num, err := strconv.ParseInt(arg, 10, 32); err == nil {
		return int32(num), nil, nil
	}

	if len(arg) > 1 && arg[0] == '#' {
		if num, err := strconv.ParseInt(arg[1:], 10, 32); err == nil {
			return int32(num), nil, nil
		}
	}

	u, err := url.Parse(arg)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return 0, nil, fmt.Errorf("invalid discussion argument: %q", arg)
	}

	// An HTTP URL is also accepted because we only extract the discussion number,
	// repo and host from the URL path; no API calls are made over HTTP.

	m := discussionURLRE.FindStringSubmatch(u.Path)
	if m == nil {
		return 0, nil, fmt.Errorf("invalid discussion URL: %q", arg)
	}

	num, _ := strconv.ParseInt(m[3], 10, 32)
	repo := ghrepo.NewWithHost(m[1], m[2], u.Hostname())
	return int32(num), repo, nil
}
