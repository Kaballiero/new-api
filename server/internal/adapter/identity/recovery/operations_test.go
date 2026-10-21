package recovery

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The zh-CN e-mails are byte-for-byte the previous hardcoded HTML.
func TestEmailMessagesKeepPreviousChineseHTML(t *testing.T) {
	require.NoError(t, i18n.Init())
	const systemName, code, link, minutes = "New API", "Ab12_<x>", "https://example.com/user/reset?email=a+b@example.com&token=t0k", 10

	subject, content := VerificationEmailMessage(i18n.LangZhCN, systemName, code, minutes)
	assert.Equal(t, fmt.Sprintf("%s邮箱验证邮件", systemName), subject)
	assert.Equal(t, fmt.Sprintf("<p>您好，你正在进行%s邮箱验证。</p>"+"<p>您的验证码为: <strong>%s</strong></p>"+"<p>验证码 %d 分钟内有效，如果不是本人操作，请忽略。</p>", systemName, code, minutes), content)

	subject, content = PasswordResetMessage(i18n.LangZhCN, systemName, link, minutes)
	assert.Equal(t, fmt.Sprintf("%s密码重置", systemName), subject)
	assert.Equal(t, fmt.Sprintf("<p>您好，你正在进行%s密码重置。</p>"+"<p>点击 <a href='%s'>此处</a> 进行密码重置。</p>"+"<p>如果链接无法点击，请尝试点击下面的链接或将其复制到浏览器中打开：<br> %s </p>"+"<p>重置链接 %d 分钟内有效，如果不是本人操作，请忽略。</p>", systemName, link, link, minutes), content)
}

func TestEmailMessagesUseRequestLanguage(t *testing.T) {
	require.NoError(t, i18n.Init())

	subject, content := VerificationEmailMessage(i18n.LangRu, "New API", "123456", 10)
	assert.Equal(t, "Подтверждение электронной почты New API", subject)
	assert.Equal(t, "<p>Здравствуйте! Вы подтверждаете адрес электронной почты в New API.</p><p>Ваш код подтверждения: <strong>123456</strong></p><p>Код действует 10 мин. Если вы не запрашивали его, просто проигнорируйте это письмо.</p>", content)

	subject, content = PasswordResetMessage("", "New API", "https://example.com/r", 15)
	assert.Equal(t, "New API password reset", subject, "a request without a supported language gets English")
	assert.Contains(t, content, "<a href='https://example.com/r'>here</a>")
	assert.Contains(t, content, "valid for 15 minutes")
}
