// Package jlpt provides Word-level JLPT vocab lookup and N2+ eligibility for extract.
package jlpt

import "strings"

// Level is a community JLPT-ish vocab band (not an official syllabus).
type Level string

const (
	N5 Level = "n5"
	N4 Level = "n4"
	N3 Level = "n3"
	N2 Level = "n2"
	N1 Level = "n1"
)

// ParseLevel accepts n5..n1 (case-insensitive). Empty/invalid → false.
func ParseLevel(s string) (Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "n5":
		return N5, true
	case "n4":
		return N4, true
	case "n3":
		return N3, true
	case "n2":
		return N2, true
	case "n1":
		return N1, true
	default:
		return "", false
	}
}

// IsN2Plus reports whether level is review-eligible (N2 or N1).
func IsN2Plus(l Level) bool {
	return l == N2 || l == N1
}

// Rank returns higher number for easier levels (n5=5 … n1=1). Unknown = 0.
func Rank(l Level) int {
	switch l {
	case N5:
		return 5
	case N4:
		return 4
	case N3:
		return 3
	case N2:
		return 2
	case N1:
		return 1
	default:
		return 0
	}
}
