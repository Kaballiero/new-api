package domain

import "strings"

func MidjourneyModelName(action string) string {
	if action == "SWAP_FACE" {
		return "swap_face"
	}
	return "mj_" + strings.ToLower(action)
}
