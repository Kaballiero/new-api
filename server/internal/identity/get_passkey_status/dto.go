package get_passkey_status

import "time"

type Output struct {
	Enabled    bool
	LastUsedAt *time.Time
}
