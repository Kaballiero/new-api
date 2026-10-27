package webauthn

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/model"
	webauthn "github.com/go-webauthn/webauthn/webauthn"
)

type webAuthnUser struct {
	user       *model.User
	credential *model.PasskeyCredential
}

func newWebAuthnUser(user *model.User, credential *model.PasskeyCredential) *webAuthnUser {
	return &webAuthnUser{user: user, credential: credential}
}

func (u *webAuthnUser) WebAuthnID() []byte {
	if u == nil || u.user == nil {
		return nil
	}
	return []byte(strconv.Itoa(u.user.Id))
}

func (u *webAuthnUser) WebAuthnName() string {
	if u == nil || u.user == nil {
		return ""
	}
	name := strings.TrimSpace(u.user.Username)
	if name == "" {
		return fmt.Sprintf("user-%d", u.user.Id)
	}
	return name
}

func (u *webAuthnUser) WebAuthnDisplayName() string {
	if u == nil || u.user == nil {
		return ""
	}
	display := strings.TrimSpace(u.user.DisplayName)
	if display != "" {
		return display
	}
	return u.WebAuthnName()
}

func (u *webAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	if u == nil || u.credential == nil {
		return nil
	}
	cred := u.credential.ToWebAuthnCredential()
	return []webauthn.Credential{cred}
}
