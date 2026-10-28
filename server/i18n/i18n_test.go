package i18n

import (
	"regexp"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var placeholderPattern = regexp.MustCompile(`\{\{[^}]*\}\}`)

func loadLocale(t *testing.T, file string) map[string]string {
	t.Helper()
	data, err := localeFS.ReadFile(file)
	require.NoError(t, err)
	messages := map[string]string{}
	require.NoError(t, yaml.Unmarshal(data, &messages))
	require.NotEmpty(t, messages)
	return messages
}

func TestRussianLocaleMatchesEnglish(t *testing.T) {
	en := loadLocale(t, "locales/en.yaml")
	ru := loadLocale(t, "locales/ru.yaml")

	for key, enText := range en {
		ruText, ok := ru[key]
		if !assert.Truef(t, ok, "ru.yaml is missing key %q", key) {
			continue
		}
		enPlaceholders := placeholderPattern.FindAllString(enText, -1)
		ruPlaceholders := placeholderPattern.FindAllString(ruText, -1)
		slices.Sort(enPlaceholders)
		slices.Sort(ruPlaceholders)
		assert.Equalf(t, slices.Compact(enPlaceholders), slices.Compact(ruPlaceholders), "placeholders differ for key %q", key)
	}
	for key := range ru {
		_, ok := en[key]
		assert.Truef(t, ok, "ru.yaml has key %q absent from en.yaml", key)
	}
}

func TestLanguageNormalization(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"ru", LangRu},
		{"ru-RU", LangRu},
		{"ru-RU,ru;q=0.9,en;q=0.8", LangRu},
		{"en-US", LangEn},
		{"zh-TW", LangZhTW},
		{"zh-CN", LangZhCN},
		{"fr", LangEn},
		{"", LangEn},
	}
	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseAcceptLanguage(tt.header))
		})
	}
	assert.True(t, IsSupported("ru-RU"))
	assert.Contains(t, SupportedLanguages(), LangRu)
}

func TestTranslateRussian(t *testing.T) {
	require.NoError(t, Init())

	tests := []struct {
		lang string
		key  string
		args map[string]any
		want string
	}{
		{LangRu, MsgNotFound, nil, "Не найдено"},
		{LangEn, MsgNotFound, nil, "Not found"},
		{"ru-RU", MsgTokenCountLimit, map[string]any{"Max": 5}, "Достигнуто максимальное количество токенов (5)"},
		{LangEn, MsgTokenCountLimit, map[string]any{"Max": 5}, "Maximum token count reached (5)"},
	}
	for _, tt := range tests {
		t.Run(tt.lang+"/"+tt.key, func(t *testing.T) {
			assert.Equal(t, tt.want, Translate(tt.lang, tt.key, tt.args))
		})
	}
}

// Texts that were hardcoded in Chinese keep their exact previous text for
// zh-CN and get a real translation for ru.
func TestFormerChineseTextsKeepZhCN(t *testing.T) {
	require.NoError(t, Init())
	quota := map[string]any{"Quota": "$1.00"}
	tests := []struct {
		key        string
		args       map[string]any
		zhCN, ruRU string
	}{
		{MsgIdentityAdminTwoFADisabled, nil, "用户2FA已被强制禁用", "Двухфакторная аутентификация пользователя принудительно отключена"},
		{MsgIdentityBackupCodesRegenerated, nil, "备用码重新生成成功", "Резервные коды успешно созданы заново"},
		{MsgIdentityPasskeyRegistered, nil, "Passkey 注册成功", "Passkey успешно зарегистрирован"},
		{MsgIdentityPasskeyUnbound, nil, "Passkey 已解绑", "Passkey отвязан"},
		{MsgIdentityPasskeyReset, nil, "Passkey 已重置", "Passkey сброшен"},
		{MsgIdentityTwoFADisabled, nil, "两步验证已禁用", "Двухфакторная аутентификация отключена"},
		{MsgIdentityOAuthUnbound, nil, "解绑成功", "Привязка удалена"},
		{MsgIdentityDefaultTokenName, map[string]any{"Username": "alice"}, "alice的初始令牌", "Начальный токен alice"},
		{MsgSecurityProofInvalid, nil, "安全验证状态无效", "Состояние проверки безопасности недействительно"},
		{MsgSecurityProofRequired, nil, "需要安全验证", "Требуется проверка безопасности"},
		{MsgSecurityProofExpired, nil, "安全验证已过期", "Срок проверки безопасности истёк"},
		{MsgSecurityProofScopeMismatch, nil, "安全验证范围不匹配", "Область проверки безопасности не совпадает"},
		{MsgSecurityProofMethodMismatch, nil, "安全验证方式不匹配", "Способ проверки безопасности не совпадает"},
		{MsgTurnstileTokenEmpty, nil, "Turnstile token 为空", "Токен Turnstile не указан"},
		{MsgTurnstileVerifyFailed, nil, "Turnstile 校验失败，请刷新重试！", "Проверка Turnstile не пройдена, обновите страницу и попробуйте снова!"},
		{MsgEmailVerificationRateLimitedWait, map[string]any{"Seconds": int64(17)}, "发送过于频繁，请等待 17 秒后再试", "Слишком частая отправка, подождите 17 с и попробуйте снова"},
		{MsgEmailVerificationRateLimitedNoWait, nil, "发送过于频繁，请稍后再试", "Слишком частая отправка, попробуйте позже"},
		{MsgAccountIDRequired, nil, "id 为空！", "Не указан ID пользователя"},
		{MsgAccountUserGroupLabel, nil, "用户分组", "Группа пользователя"},
		{MsgAccountAutoGroupRatio, nil, "自动", "Авто"},
		{MsgAccountCheckinLog, quota, "用户签到，获得额度 $1.00", "Ежедневная отметка, начислено $1.00"},
		{MsgAccountRegisterLog, quota, "新用户注册赠送 $1.00", "Бонус за регистрацию нового пользователя $1.00"},
		{MsgAccountInviteeLog, quota, "使用邀请码赠送 $1.00", "Бонус за использование кода приглашения $1.00"},
		{MsgAccountInviterLog, quota, "邀请用户赠送 $1.00", "Бонус за приглашённого пользователя $1.00"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			assert.Equal(t, tt.zhCN, Translate(LangZhCN, tt.key, tt.args))
			assert.Equal(t, tt.ruRU, Translate(LangRu, tt.key, tt.args))
		})
	}
}

func TestManagementMessagesKeepLocaleBytes(t *testing.T) {
	require.NoError(t, Init())
	for _, tc := range []struct{ key, zh, en, ru string }{
		{MsgDeploymentManageNotConfigured, "io.net model deployment is not enabled or api key missing", "io.net model deployment is not enabled or api key missing", "Развёртывание моделей io.net отключено или отсутствует API-ключ"},
		{MsgDeploymentManageNameEmpty, "deployment name cannot be empty", "deployment name cannot be empty", "Название развёртывания не может быть пустым"},
		{MsgDeploymentManageNameUnavailable, "deployment name is not available, please choose a different name", "deployment name is not available, please choose a different name", "Название развёртывания недоступно, выберите другое"},
		{MsgDeploymentManageIDRequired, "deployment ID is required", "deployment ID is required", "Необходимо указать ID развёртывания"},
		{MsgDeploymentManageContainerIDRequired, "container ID is required", "container ID is required", "Необходимо указать ID контейнера"},
		{MsgDeploymentManageInvalidPayload, "invalid request payload", "invalid request payload", "Недопустимое тело запроса"},
		{MsgDeploymentManageAPIKeyRequired, "api_key is required", "api_key is required", "Необходимо указать api_key"},
		{MsgDeploymentManageValidateKey, "failed to validate api key", "failed to validate api key", "Не удалось проверить API-ключ"},
		{MsgDeploymentManageTerminated, "Deployment termination requested successfully", "Deployment termination requested successfully", "Запрос на завершение развёртывания отправлен"},
		{MsgDeploymentManageCreated, "Deployment created successfully", "Deployment created successfully", "Развёртывание создано"},
		{MsgDeploymentManageNameParameter, "name parameter is required", "name parameter is required", "Необходимо указать параметр name"},
		{MsgDeploymentManageContainerParameter, "container_id parameter is required", "container_id parameter is required", "Необходимо указать параметр container_id"},
		{MsgDeploymentManageHardwareParameter, "hardware_id parameter is required", "hardware_id parameter is required", "Необходимо указать параметр hardware_id"},
		{MsgDeploymentManageInvalidHardware, "invalid hardware_id parameter", "invalid hardware_id parameter", "Недопустимый параметр hardware_id"},
		{MsgDeploymentManageContainerNotFound, "container details not found", "container details not found", "Сведения о контейнере не найдены"},
		{MsgPrefillNameType, "组名称和类型不能为空", "Group name and type are required", "Необходимо указать название и тип группы"},
		{MsgPrefillDuplicate, "组名称已存在", "Group name already exists", "Группа с таким названием уже существует"},
		{MsgPrefillIDRequired, "缺少组 ID", "Group ID is required", "Необходимо указать ID группы"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			assert.Equal(t, tc.en, T(nil, tc.key))
			for _, item := range []struct{ lang, want string }{{"en", tc.en}, {"ru", tc.ru}, {"zh-CN", tc.zh}, {"zh-TW", tc.zh}} {
				assert.Equal(t, item.want, Translate(item.lang, tc.key))
			}
		})
	}
}
