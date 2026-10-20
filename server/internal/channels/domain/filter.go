package domain

import "strings"

type Filter struct {
	Group        string
	Status, Type int
}
type Sort struct {
	By, Order string
	ID        bool
}

func ParseStatus(value string) int {
	switch strings.ToLower(value) {
	case "enabled", "1":
		return 1
	case "disabled", "0":
		return 0
	default:
		return -1
	}
}
func NormalizeGroup(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "all") || strings.EqualFold(value, "null") {
		return ""
	}
	return value
}
