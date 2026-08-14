package detect

import (
	"regexp"
	"strings"

	"github.com/Oswald-Hao/Osverse/internal/platform"
)

type CommandSpec struct {
	ID              string
	Name            string
	ExecutableNames []string
	VersionArgs     []string
	VersionPattern  *regexp.Regexp
	MinimumOS       string
}

func parseCommandVersion(pattern *regexp.Regexp, result platform.CommandResult) (string, bool) {
	if pattern == nil {
		return "", false
	}
	for _, output := range []string{strings.TrimSpace(result.Stdout), strings.TrimSpace(result.Stderr)} {
		matches := pattern.FindStringSubmatch(output)
		if len(matches) > 1 && matches[1] != "" {
			return matches[1], true
		}
	}
	return "", false
}
