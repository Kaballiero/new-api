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
