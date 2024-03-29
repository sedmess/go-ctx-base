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
	"sync"
	"time"
)

const serverListenKey = "HTTP_LISTEN"
const serverRequestSizeLimitKey = "HTTP_MAX_REQUEST_SIZE"
const serverMaxHeaderSizeKey = "HTTP_MAX_HEADER_SIZE"
const serverReadTimeoutKey = "HTTP_READ_TIMEOUT"
const serverWriteTimeoutKey = "HTTP_WRITE_TIMEOUT"

const credentialEnvKey = "credential"

const serverRequestSizeLimitDefault = 1048576 // 1 MB
const serverMaxHeaderSizeDefault = 1048576    // 1 MB
var serverReadTimeoutDefault = 60 * time.Second
var serverWriteTimeoutDefault = 60 * time.Second

func NewRestServer(name string, configPrefix string) RestServer {
	return &restServer{name: name, prefix: strings.ToUpper(configPrefix)}
}

type RestServer interface {
	AddMiddleware(middleware Middleware) RestServer

	registerRoute(route *rest.Route)
	logger() logger.Logger
}

type Middleware func(chain rest.HandlerFunc, writer rest.ResponseWriter, request *rest.Request) error

type restServer struct {
	sync.Mutex

	name   string
	prefix string

	l logger.Logger `logger:""`

	server           *http.Server
	api              *rest.Api
	middlewares      []Middleware
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
}

func (instance *restServer) Name() string {
	return instance.name
}

func (instance *restServer) logger() logger.Logger {
	return instance.l
}

func (instance *restServer) AddMiddleware(middleware Middleware) RestServer {
	instance.Lock()

	instance.middlewares = append(instance.middlewares, middleware)

	instance.Unlock()

	return instance
}

func (instance *restServer) registerRoute(route *rest.Route) {
	instance.Lock()

	instance.routes = append(instance.routes, route)

	instance.Unlock()
}

func (instance *restServer) AfterStart() {
	instance.Lock()
	defer instance.Unlock()

	for _, middleware := range instance.middlewares {
		instance.api.Use(rest.MiddlewareSimple(func(handler rest.HandlerFunc) rest.HandlerFunc {
			return func(writer rest.ResponseWriter, request *rest.Request) {
				if err := middleware(handler, writer, request); err != nil {
					instance.l.Error("on middleware:", err)
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
