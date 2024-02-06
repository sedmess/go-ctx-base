package httpserver

import (
	"errors"
	"github.com/ant0ine/go-json-rest/rest"
	"github.com/spaolacci/murmur3"
	"net/http"
	"strconv"
	"strings"
)

type AuthenticationResultCode int

const (
	AuthenticationRequired = AuthenticationResultCode(1)
	Forbidden              = AuthenticationResultCode(2)
	Authorized             = AuthenticationResultCode(3)
)

func BearerTokenAuthenticator(authFn func(path string, token string) AuthenticationResultCode) Middleware {
	return func(chain rest.HandlerFunc, writer rest.ResponseWriter, request *rest.Request) error {
		token := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		result := authFn(request.RequestURI, token)
		switch result {
		case Authorized:
			request.Env[credentialEnvKey] = int64(murmur3.Sum64([]byte(token)))
			chain(writer, request)
			return nil
		case Forbidden:
			writer.WriteHeader(http.StatusForbidden)
			return nil
		case AuthenticationRequired:
			writer.WriteHeader(http.StatusUnauthorized)
			return nil
		default:
			return errors.New("unknown authenticator result: " + strconv.Itoa(int(result)))
		}
	}
}

func BasicAuthenticator(authFn func(path string, username string, password string) AuthenticationResultCode) Middleware {
	return func(chain rest.HandlerFunc, writer rest.ResponseWriter, request *rest.Request) error {
		username, password, ok := request.BasicAuth()
		if !ok {
			writer.Header().Set("WWW-Authenticate", "Basic")
			writer.WriteHeader(http.StatusUnauthorized)
			return nil
		}
		result := authFn(request.RequestURI, username, password)

		switch result {
		case Authorized:
			request.Env[credentialEnvKey] = username
			chain(writer, request)
			return nil
		case Forbidden:
			writer.WriteHeader(http.StatusForbidden)
			return nil
		case AuthenticationRequired:
			writer.Header().Set("WWW-Authenticate", "Basic")
			writer.WriteHeader(http.StatusUnauthorized)
			return nil
		default:
			return errors.New("unknown authenticator result: " + strconv.Itoa(int(result)))
		}
	}
}
