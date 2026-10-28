package domain

// DashboardPermissions describes the dashboard sections available to the role.
// Only roles.Admin and roles.Root are read.
func DashboardPermissions(userRole int, roles AdminRoles) map[string]any {
	permissions := map[string]any{}

	// Permissions are derived from the user role.
	if userRole == roles.Root {
		// The root user does not need sidebar settings.
		permissions["sidebar_settings"] = false
		permissions["sidebar_modules"] = map[string]any{}
	} else if userRole == roles.Admin {
		// An administrator configures the sidebar except system settings.
		permissions["sidebar_settings"] = true
		permissions["sidebar_modules"] = map[string]any{
			"admin": map[string]any{
				"setting": false, // Administrators have no access to system settings.
			},
		}
	} else {
		// A common user configures only personal sections, without admin sections.
		permissions["sidebar_settings"] = true
		permissions["sidebar_modules"] = map[string]any{
			"admin": false, // Common users have no access to admin sections.
		}
	}

	return permissions
}

// DashboardSidebarConfig is the default sidebar of the role. Only roles.Admin
// and roles.Root are read.
func DashboardSidebarConfig(userRole int, roles AdminRoles) map[string]any {
	defaultConfig := map[string]any{}

	// The chat section is available to all users.
	defaultConfig["chat"] = map[string]any{
		"enabled":    true,
		"playground": true,
		"chat":       true,
	}

	// The console section is available to all users.
	defaultConfig["console"] = map[string]any{
		"enabled":    true,
		"detail":     true,
		"token":      true,
		"log":        true,
		"midjourney": true,
		"task":       true,
	}

	// The personal section is available to all users.
	defaultConfig["personal"] = map[string]any{
		"enabled":  true,
		"topup":    true,
		"personal": true,
	}

	// The admin section depends on the role.
	if userRole == roles.Admin {
		// Administrators get admin sections except system settings.
		defaultConfig["admin"] = map[string]any{
			"enabled":    true,
			"channel":    true,
			"models":     true,
			"redemption": true,
			"user":       true,
			"setting":    false, // Administrators have no access to system settings.
		}
	} else if userRole == roles.Root {
		// The root user gets every section.
		defaultConfig["admin"] = map[string]any{
			"enabled":    true,
			"channel":    true,
			"models":     true,
			"redemption": true,
			"user":       true,
			"setting":    true,
		}
	}
	// Common users get no admin section.

	return defaultConfig
}
