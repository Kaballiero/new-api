package complete_oauth

type OAuthUserDeletedError struct{}

func (*OAuthUserDeletedError) Error() string { return "user has been deleted" }

type OAuthRegistrationDisabledError struct{}

func (*OAuthRegistrationDisabledError) Error() string { return "registration is disabled" }

type OAuthEmailAlreadyTakenError struct{}

func (*OAuthEmailAlreadyTakenError) Error() string { return "email is already in use" }
