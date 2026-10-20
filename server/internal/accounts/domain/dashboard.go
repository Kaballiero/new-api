package domain

import "github.com/QuantumNous/new-api/common"

// DashboardPermissions описывает доступные этой роли разделы панели.
func DashboardPermissions(userRole int) map[string]any {
	permissions := map[string]any{}

	// Рассчитываем права по роли пользователя.
	if userRole == common.RoleRootUser {
		// Суперадминистратору не нужна настройка боковой панели.
		permissions["sidebar_settings"] = false
		permissions["sidebar_modules"] = map[string]any{}
	} else if userRole == common.RoleAdminUser {
		// Администратор может настраивать боковую панель, кроме системных настроек.
		permissions["sidebar_settings"] = true
		permissions["sidebar_modules"] = map[string]any{
			"admin": map[string]any{
				"setting": false, // Администратор не имеет доступа к системным настройкам.
			},
		}
	} else {
		// Обычный пользователь настраивает только личные функции без разделов администратора.
		permissions["sidebar_settings"] = true
		permissions["sidebar_modules"] = map[string]any{
			"admin": false, // Обычный пользователь не имеет доступа к разделам администратора.
		}
	}

	return permissions
}

func DashboardSidebarConfig(userRole int) map[string]any {
	defaultConfig := map[string]any{}

	// Раздел чата доступен всем пользователям.
	defaultConfig["chat"] = map[string]any{
		"enabled":    true,
		"playground": true,
		"chat":       true,
	}

	// Раздел консоли доступен всем пользователям.
	defaultConfig["console"] = map[string]any{
		"enabled":    true,
		"detail":     true,
		"token":      true,
		"log":        true,
		"midjourney": true,
		"task":       true,
	}

	// Личный кабинет доступен всем пользователям.
	defaultConfig["personal"] = map[string]any{
		"enabled":  true,
		"topup":    true,
		"personal": true,
	}

	// Раздел администратора зависит от роли.
	if userRole == common.RoleAdminUser {
		// Администратору доступны административные разделы, кроме системных настроек.
		defaultConfig["admin"] = map[string]any{
			"enabled":    true,
			"channel":    true,
			"models":     true,
			"redemption": true,
			"user":       true,
			"setting":    false, // Администратор не имеет доступа к системным настройкам.
		}
	} else if userRole == common.RoleRootUser {
		// Суперадминистратору доступны все функции.
		defaultConfig["admin"] = map[string]any{
			"enabled":    true,
			"channel":    true,
			"models":     true,
			"redemption": true,
			"user":       true,
			"setting":    true,
		}
	}
	// Для обычного пользователя раздел admin не включён.

	return defaultConfig
}
