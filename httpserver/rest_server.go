package httpserver

import (
	"context"
	"errors"
	"github.com/ant0ine/go-json-rest/rest"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/logger"
	"github.com/sedmess/go-ctx/u"
	"net/http"
	"strings"
	"time"
)

const serverListenKey = "HTTP_LISTEN"
const serverRequestSizeLimitKey = "HTTP_MAX_REQUEST_SIZE"
const serverMaxHeaderSizeKey = "HTTP_MAX_HEADER_SIZE"
const serverReadTimeoutKey = "HTTP_READ_TIMEOUT"
const serverWriteTimeoutKey = "HTTP_WRITE_TIMEOUT"
const serverAuthBearerTokensKey = "HTTP_AUTH_BEARER_TOKENS"

const credentialEnvKey = "credential"

const serverRequestSizeLimitDefault = 1048576 // 1 MB
const serverMaxHeaderSizeDefault = 1048576    // 1 MB
var serverReadTimeoutDefault = 60 * time.Second
var serverWriteTimeoutDefault = 60 * time.Second

func NewRestServer(name string, configPrefix string) RestServer {
	return &restServer{name: name, prefix: strings.ToUpper(configPrefix)}
}

type RestServer interface {
	SetupAuthentication(authenticator Authenticator)

	registerRoute(route *rest.Route)
	logger() logger.Logger
}

type restServer struct {
	name   string
	prefix string

	l logger.Logger `logger:""`

	server           *http.Server
	api              *rest.Api
	authenticator    Authenticator
	routes           []*rest.Route
	requestSizeLimit int64
}

func (instance *restServer) Init() {
	instance.server = &http.Server{
		Addr:           instance.getEnv(serverListenKey).AsStringDefault("127.0.0.1:8088"),
		MaxHeaderBytes: instance.getEnv(serverMaxHeaderSizeKey).AsIntDefault(serverMaxHeaderSizeDefault),
		ReadTimeout:    instance.getEnv(serverReadTimeoutKey).AsDurationDefault(serverReadTimeoutDefault),
		WriteTimeout:   instance.getEnv(serverWriteTimeoutKey).AsDurationDefault(serverWriteTimeoutDefault),
	}
	instance.requestSizeLimit = int64(ctx.GetEnv(serverRequestSizeLimitKey).AsIntDefault(serverRequestSizeLimitDefault))

	instance.api = rest.NewApi()
	logFormat := "[" + instance.name + "] %h %l %u \"%r\" %s %b"
	instance.api.Use(
		&rest.AccessLogApacheMiddleware{
			Logger: logger.GetLogger(logger.DEBUG),
			Format: rest.AccessLogFormat(logFormat),
		},
		&rest.TimerMiddleware{},
		&rest.RecorderMiddleware{},
		&rest.RecoverMiddleware{},
	)

	authTokens := instance.getEnv(serverAuthBearerTokensKey).AsStringArrayDefault(nil)
	if authTokens != nil {
		authTokenSet := make(map[string]bool)
		for _, token := range authTokens {
			authTokenSet["Bearer "+token] = true
		}
		instance.l.Info("use Bearer tokens:", len(authTokenSet))
		instance.api.Use(
			rest.MiddlewareSimple(func(handler rest.HandlerFunc) rest.HandlerFunc {
				return func(writer rest.ResponseWriter, request *rest.Request) {
					authHeader := request.Header.Get("Authorization")
					if _, found := authTokenSet[authHeader]; found {
						handler(writer, request)
					} else if authHeader == "" {
						writer.WriteHeader(http.StatusUnauthorized)
					} else {
						writer.WriteHeader(http.StatusForbidden)
					}
				}
			}),
		)
	}
}

func (instance *restServer) Name() string {
	return instance.name
}

func (instance *restServer) logger() logger.Logger {
	return instance.l
}

func (instance *restServer) SetupAuthentication(authenticator Authenticator) {
	instance.authenticator = authenticator
}

func (instance *restServer) registerRoute(route *rest.Route) {
	instance.routes = append(instance.routes, route)
}

func (instance *restServer) AfterStart() {
	if instance.authenticator != nil {
		instance.api.Use(rest.MiddlewareSimple(func(handler rest.HandlerFunc) rest.HandlerFunc {
			return func(writer rest.ResponseWriter, request *rest.Request) {
				result := instance.authenticator.Authenticate(request)
				switch result.Code {
				case Authorized:
					request.Env[credentialEnvKey] = result.Credential
					handler(writer, request)
				case Forbidden:
					writer.WriteHeader(http.StatusForbidden)
				case AuthenticationRequired:
					writer.WriteHeader(http.StatusUnauthorized)
				default:
					instance.l.Error("unknown authenticator result:", result)
					writer.WriteHeader(http.StatusInternalServerError)
				}
			}
		}))
	}
	instance.api.SetApp(u.Must2(rest.MakeRouter(instance.routes...)))
	instance.server.Handler = &requestSizeLimitHandlerWrapper{
		handler:        instance.api.MakeHandler(),
		maxRequestSize: instance.requestSizeLimit,
	}
	go func() {
		instance.l.Info("http server started on", instance.server.Addr)
		if err := instance.server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			instance.l.Fatal(err)
		} else {
			instance.l.Debug("http server stopped")
		}
	}()
}

func (instance *restServer) BeforeStop() {
	timeoutContext, cancelFunc := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFunc()
	if err := instance.server.Shutdown(timeoutContext); err != nil {
		instance.l.Error("error on http server shutdown", err)
	}
}

func (instance *restServer) getEnv(name string) *ctx.EnvValue {
	return ctx.GetEnvCustomOrDefault(instance.prefix, name)
}

type requestSizeLimitHandlerWrapper struct {
	handler        http.Handler
	maxRequestSize int64
}

func (instance *requestSizeLimitHandlerWrapper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, instance.maxRequestSize)
	}
	instance.handler.ServeHTTP(w, r)
}

func RegisterTypedRoute[T any](server RestServer, method string, path string) TypedRequestHandler[T] {
	return &typedRqHandler[T]{rqHandlerBase{server: server, method: method, path: path}}
}

func BuildTypedRoute[T any](server RestServer) TypedRequestHandler[T] {
	return &typedRqHandler[T]{rqHandlerBase{server: server}}
}

func RegisterRoute(server RestServer, method string, path string) RequestHandler {
	return &rqHandler{rqHandlerBase{server: server, method: method, path: path}}
}

func BuildRoute(server RestServer) RequestHandler {
	return &rqHandler{rqHandlerBase{server: server}}
}
