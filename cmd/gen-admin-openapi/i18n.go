package main

import "strings"

// translations is the central locale dictionary for OpenAPI spec generation.
// Keys are stable identifiers (snake/dot-style), values are localized text.
//
// Layered lookup (see translate): requested locale → en (fallback) → key
// literal. Missing translation surfaces as the key itself, making gaps
// visible in the rendered spec.
//
// Locales:
//   - en: default, used by `/openapi.json` without `?lang=` parameter
//   - zh: Simplified Chinese (sources from existing manifest legacy strings)
//   - ru: Russian
//
// Categories of keys:
//   - info.* — top-level spec metadata (title, description)
//   - tag.*  — operation tag labels (from guessTag)
//   - resp.* — HTTP response descriptions
//   - auth.* — security label hints (admin/user/root role)
//   - err.*  — short labels for HTTP error categories (used by injectErrorResponses)
//   - summary.<HandlerName> — per-handler operation summaries. zh sources from
//     godoc comments; en/ru are hand-curated; missing keys fall back to the
//     auto "post /api/x" placeholder produced by enrichFromHandlers.
var translations = map[string]map[string]string{
	"en": {
		"info.title":             "new-api Admin & Dashboard API",
		"info.description":       "Admin and dashboard endpoints exposed by the new-api gateway. Relay (LLM) endpoints live in a separate spec.",
		"tag.subscription":       "Subscription",
		"tag.subscription_admin": "Subscription Management",
		"tag.performance":        "Performance",
		"tag.deployment":         "Deployment",
		"tag.channel":            "Channel Management",
		"tag.user":               "User Management",
		"tag.token":              "Token Management",
		"tag.log":                "Logs",
		"tag.data":               "Data & Statistics",
		"tag.system":             "System Settings",
		"tag.oauth":              "OAuth Providers",
		"tag.topup":              "Top-up / Payments",
		"tag.uncategorized":      "Uncategorized",
		"resp.success":           "Success",
		"auth.admin":             "👨‍💼 Admin role required",
		"auth.user":              "👤 User authentication required",
		"auth.root":              "🛡️ Root role required",
		"err.400":                "Bad request — invalid_params, validation failed, or other client error",
		"err.401":                "Unauthorized — missing or invalid auth credentials",
		"err.403":                "Forbidden — insufficient role/permission or feature disabled",
		"err.404":                "Not found — referenced resource does not exist",
		"err.409":                "Conflict — resource already exists or state precludes the operation",
		"err.422":                "Unprocessable entity — well-formed but semantically invalid input",
		"err.500":                "Internal server error — DB transaction failure or unhandled exception",
		"err.502":                "Bad gateway — upstream provider (LLM API, model registry) failed",
	},
	"zh": {
		"info.title":             "后台管理接口",
		"info.description":       "new-api 网关后台与控制台接口。中继 (LLM) 接口位于独立的 spec。",
		"tag.subscription":       "订阅",
		"tag.subscription_admin": "订阅管理",
		"tag.performance":        "性能",
		"tag.deployment":         "部署",
		"tag.channel":            "渠道管理",
		"tag.user":               "用户管理",
		"tag.token":              "令牌管理",
		"tag.log":                "日志",
		"tag.data":               "数据统计",
		"tag.system":             "系统设置",
		"tag.oauth":              "OAuth 提供商",
		"tag.topup":              "充值与支付",
		"tag.uncategorized":      "未分类",
		"resp.success":           "成功",
		"auth.admin":             "👨‍💼 需要管理员权限（Admin）",
		"auth.user":              "👤 需要登录（User权限）",
		"auth.root":              "🛡️ 需要超级管理员权限（Root）",
		"err.400":                "请求错误 — 参数无效、校验失败或其他客户端错误",
		"err.401":                "未授权 — 缺少或无效的认证凭据",
		"err.403":                "禁止访问 — 角色/权限不足或功能未启用",
		"err.404":                "未找到 — 引用的资源不存在",
		"err.409":                "冲突 — 资源已存在或当前状态不允许此操作",
		"err.422":                "无法处理 — 请求格式正确但语义无效",
		"err.500":                "服务器内部错误 — 数据库事务失败或未处理异常",
		"err.502":                "网关错误 — 上游提供商（LLM API、模型注册表）失败",
	},
	"ru": {
		"info.title":             "API администрирования и панели new-api",
		"info.description":       "Эндпоинты администрирования и панели управления new-api шлюза. LLM relay эндпоинты — в отдельной спецификации.",
		"tag.subscription":       "Подписки",
		"tag.subscription_admin": "Управление подписками",
		"tag.performance":        "Производительность",
		"tag.deployment":         "Развёртывание",
		"tag.channel":            "Каналы (провайдеры)",
		"tag.user":               "Пользователи",
		"tag.token":              "Токены (API-ключи)",
		"tag.log":                "Логи",
		"tag.data":               "Данные и статистика",
		"tag.system":             "Системные настройки",
		"tag.oauth":              "OAuth провайдеры",
		"tag.topup":              "Пополнения и платежи",
		"tag.uncategorized":      "Без категории",
		"resp.success":           "Успех",
		"auth.admin":             "👨‍💼 Требуется роль администратора",
		"auth.user":              "👤 Требуется авторизация пользователя",
		"auth.root":              "🛡️ Требуется роль root",
		"err.400":                "Неверный запрос — invalid_params, провалена валидация или другая клиентская ошибка",
		"err.401":                "Не авторизован — отсутствуют или невалидные учётные данные",
		"err.403":                "Доступ запрещён — недостаточно прав/роли или функция отключена",
		"err.404":                "Не найдено — ресурс не существует",
		"err.409":                "Конфликт — ресурс уже существует или состояние не допускает операцию",
		"err.422":                "Невозможно обработать — корректный синтаксис, но семантически невалидно",
		"err.500":                "Внутренняя ошибка сервера — сбой транзакции БД или необработанное исключение",
		"err.502":                "Ошибка шлюза — upstream провайдер (LLM API, реестр моделей) недоступен",
	},
}

// supportedLocales — order matters: en is generated first (also copied to
// docs/openapi/api.json for backward compat with tools that expect that path).
var supportedLocales = []string{"en", "zh", "ru"}

// translate returns the localized text for `key` in `locale`. Fallback chain:
// requested locale → en → key literal. The literal fallback makes missing
// translations visible in the rendered spec (operators see the raw key and
// can add it to the dictionary).
func translate(locale, key string) string {
	if v, ok := translations[locale][key]; ok && v != "" {
		return v
	}
	if v, ok := translations["en"][key]; ok && v != "" {
		return v
	}
	return key
}

// translateSummaryWithFallback resolves a per-handler summary. zh defaults to
// the godoc-extracted text (`fallback`) — Chinese is the source language for
// existing comments. en/ru pull from translations["summary.<Handler>"]; if
// absent, use the godoc fallback as a last resort (better than the auto
// "post /api/x" placeholder).
func translateSummaryWithFallback(locale, handlerName, godocFallback string) string {
	key := "summary." + handlerName
	if locale == "zh" && godocFallback != "" {
		return godocFallback
	}
	if v, ok := translations[locale][key]; ok && v != "" {
		return v
	}
	// en or ru without explicit entry — godoc (likely Chinese) is better than the auto placeholder.
	if godocFallback != "" {
		return godocFallback
	}
	return ""
}

// localeOK reports whether `loc` is a recognized locale. Used by the runtime
// `/openapi.json?lang=` dispatcher to validate input.
func localeOK(loc string) bool {
	for _, l := range supportedLocales {
		if l == loc {
			return true
		}
	}
	return false
}

// trimGodocPrefix strips the "// FuncName " prefix from a godoc comment line,
// returning just the descriptive remainder. Returns empty string if the
// comment doesn't follow the expected `// FuncName description` pattern.
func trimGodocPrefix(comment, funcName string) string {
	line := strings.TrimSpace(strings.TrimPrefix(comment, "//"))
	if strings.HasPrefix(line, funcName+" ") {
		return strings.TrimSpace(strings.TrimPrefix(line, funcName+" "))
	}
	return ""
}
