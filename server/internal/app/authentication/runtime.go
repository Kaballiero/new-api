package authentication

import "sync"

var GatewayKeyAuth = sync.OnceValue(NewGatewayKeyAuth)
var PersonalAccessTokenAuth = sync.OnceValue(NewPATAuth)
