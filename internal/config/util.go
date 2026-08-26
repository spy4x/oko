package config

import "strings"

// splitAndTrim splits s on sep and trims whitespace from each piece.
// Empty pieces are dropped.
func splitAndTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	// Avoid allocating an extra slice if everything is already clean.
	allEmpty := true
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			allEmpty = false
			break
		}
	}
	if allEmpty {
		return nil
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
