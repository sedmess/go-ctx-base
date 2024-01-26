package httpserver

import (
	"github.com/ant0ine/go-json-rest/rest"
	"github.com/spaolacci/murmur3"
	"strings"
)

type AuthenticationResultCode int

const (
	AuthenticationRequired = AuthenticationResultCode(1)
	Forbidden              = AuthenticationResultCode(2)
	Authorized             = AuthenticationResultCode(3)
)

type AuthenticationResult struct {
	Code       AuthenticationResultCode
	Credential int64
}

type Authenticator interface {
	Authenticate(request *rest.Request) AuthenticationResult
}

type bearerAuthenticator struct {
	authFn func(path string, token string) AuthenticationResultCode
}

func (a *bearerAuthenticator) Authenticate(request *rest.Request) AuthenticationResult {
	token := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
	return AuthenticationResult{Code: a.authFn(request.RequestURI, token), Credential: int64(murmur3.Sum64([]byte(token)))}
}

func BearerTokenAuthenticator(authFn func(path string, token string) AuthenticationResultCode) Authenticator {
	return &bearerAuthenticator{authFn: authFn}
}
