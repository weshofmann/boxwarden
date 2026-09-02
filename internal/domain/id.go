package domain

import "fmt"

const maxIDLength = 63

type ID string

func Parse(raw string) (ID, error) {
	if len(raw) == 0 || len(raw) > maxIDLength {
		return "", fmt.Errorf("invalid domain identifier %q", raw)
	}
	if raw[0] < 'a' || raw[0] > 'z' {
		return "", fmt.Errorf("invalid domain identifier %q", raw)
	}
	for _, r := range raw[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return "", fmt.Errorf("invalid domain identifier %q", raw)
		}
	}
	return ID(raw), nil
}
