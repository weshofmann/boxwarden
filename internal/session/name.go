package session

import "fmt"

const maxNameLength = 63

type Name string

func ParseName(raw string) (Name, error) {
	if len(raw) == 0 || len(raw) > maxNameLength {
		return "", fmt.Errorf("invalid session name %q", raw)
	}
	if raw[0] < 'a' || raw[0] > 'z' {
		return "", fmt.Errorf("invalid session name %q", raw)
	}
	for _, r := range raw[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return "", fmt.Errorf("invalid session name %q", raw)
		}
	}
	return Name(raw), nil
}
