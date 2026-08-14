package selfupdate

import (
	"errors"
	"strconv"
	"strings"
)

type version struct {
	major, minor, patch int
	pre                 []string
}

func parseVersion(raw string) (version, error) {
	raw = strings.TrimPrefix(raw, "v")
	if raw == "" || strings.Contains(raw, "+") {
		return version{}, errors.New("invalid version")
	}
	parts := strings.SplitN(raw, "-", 2)
	core := strings.Split(parts[0], ".")
	if len(core) != 3 {
		return version{}, errors.New("invalid version")
	}
	values := make([]int, 3)
	for index, value := range core {
		if value == "" || (len(value) > 1 && value[0] == '0') {
			return version{}, errors.New("invalid version")
		}
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			return version{}, errors.New("invalid version")
		}
		values[index] = parsed
	}
	result := version{major: values[0], minor: values[1], patch: values[2]}
	if len(parts) == 2 {
		if parts[1] == "" {
			return version{}, errors.New("invalid version")
		}
		result.pre = strings.Split(parts[1], ".")
		for _, identifier := range result.pre {
			if identifier == "" {
				return version{}, errors.New("invalid version")
			}
			for _, character := range identifier {
				if !((character >= '0' && character <= '9') || (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') || character == '-') {
					return version{}, errors.New("invalid version")
				}
			}
			if numeric(identifier) && len(identifier) > 1 && identifier[0] == '0' {
				return version{}, errors.New("invalid version")
			}
		}
	}
	return result, nil
}

func numeric(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func compareVersions(left, right version) int {
	for _, pair := range [][2]int{{left.major, right.major}, {left.minor, right.minor}, {left.patch, right.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if len(left.pre) == 0 && len(right.pre) == 0 {
		return 0
	}
	if len(left.pre) == 0 {
		return 1
	}
	if len(right.pre) == 0 {
		return -1
	}
	limit := len(left.pre)
	if len(right.pre) < limit {
		limit = len(right.pre)
	}
	for index := 0; index < limit; index++ {
		leftPart, rightPart := left.pre[index], right.pre[index]
		if leftPart == rightPart {
			continue
		}
		leftNumeric, rightNumeric := numeric(leftPart), numeric(rightPart)
		if leftNumeric && rightNumeric {
			leftValue, _ := strconv.Atoi(leftPart)
			rightValue, _ := strconv.Atoi(rightPart)
			if leftValue < rightValue {
				return -1
			}
			return 1
		}
		if leftNumeric {
			return -1
		}
		if rightNumeric {
			return 1
		}
		if leftPart < rightPart {
			return -1
		}
		return 1
	}
	if len(left.pre) < len(right.pre) {
		return -1
	}
	if len(left.pre) > len(right.pre) {
		return 1
	}
	return 0
}
