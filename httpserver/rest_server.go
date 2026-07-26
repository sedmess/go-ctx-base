package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/ctx/logger"
	"github.com/sedmess/go-ctx/u"
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

func NewRestServer(name string, configPrefix string, defPort int) RestServer {
	return &restServer{name: name, prefix: strings.ToUpper(configPrefix), silent: false, defPort: defPort}
}

func NewRestServerSilent(name string, configPrefix string, defPort int) RestServer {
	return &restServer{name: name, prefix: strings.ToUpper(configPrefix), silent: true, defPort: defPort}

}

type RestServer interface {
	AddMiddleware(middleware Middleware) RestServer

	registerRoute(route *rest.Route)
	logger() logger.Logger
}

type Middleware func(chain rest.HandlerFunc, writer rest.ResponseWriter, request *rest.Request) error

// go-ctx invokes this service's lifecycle phases serially within one application run
// and permits restart only after Stop().Join(). Operational request state lives in the
// immutable generation, so lifecycle fields do not need their own mutex.
type restServer struct {
	name    string
	prefix  string
	silent  bool
	defPort int

	l           logger.Logger   `ctx:""`
	rootContext context.Context `ctx:"context"`

	persistentMiddlewares []Middleware
	persistentRoutes      []*rest.Route
	generation            *restServerGeneration
}

type restServerGeneration struct {
	server           *http.Server
	listener         net.Listener
	requestContext   context.Context
	cancelRequests   context.CancelFunc
	requestSizeLimit int64
	middlewares      []Middleware
	routes           []*rest.Route
	serveDone        chan struct{}
	started          bool
	stopOnce         sync.Once
	stopErr          error
}

func (instance *restServer) Init() error {
	if instance.generation != nil {
		return fmt.Errorf("http server %q is already initialized", instance.name)
	}
	if instance.l == nil {
		instance.l = logger.New(instance.name)
	}

	serverAddress := instance.getEnv(serverListenKey).AsStringDefault("127.0.0.1:" + strconv.Itoa(instance.defPort))
	parentContext := instance.rootContext
	if parentContext == nil {
		parentContext = context.Background()
	}
	requestContext, cancelRequests := context.WithCancel(parentContext)

	server := &http.Server{
		Addr:           serverAddress,
		MaxHeaderBytes: instance.getEnv(serverMaxHeaderSizeKey).AsIntDefault(serverMaxHeaderSizeDefault),
		ReadTimeout:    instance.getEnv(serverReadTimeoutKey).AsDurationDefault(serverReadTimeoutDefault),
		WriteTimeout:   instance.getEnv(serverWriteTimeoutKey).AsDurationDefault(serverWriteTimeoutDefault),
		BaseContext: func(net.Listener) context.Context {
			return requestContext
		},
	}

	bindAddress := effectiveBindAddress(serverAddress)
	listener, err := net.Listen("tcp", bindAddress)
	if err != nil {
		cancelRequests()
		return fmt.Errorf("http server %q cannot listen on %q: %w", instance.name, bindAddress, err)
	}

	instance.generation = &restServerGeneration{
		server:           server,
		listener:         listener,
		requestContext:   requestContext,
		cancelRequests:   cancelRequests,
		requestSizeLimit: int64(ctx.GetEnv(serverRequestSizeLimitKey).AsIntDefault(serverRequestSizeLimitDefault)),
		middlewares:      make([]Middleware, 0),
		routes:           make([]*rest.Route, 0),
		serveDone:        make(chan struct{}),
	}
	return nil
}

func (instance *restServer) Name() string {
	return instance.name
}

func (instance *restServer) logger() logger.Logger {
	return instance.l
}

func (instance *restServer) AddMiddleware(middleware Middleware) RestServer {
	if instance.generation == nil {
		instance.persistentMiddlewares = append(instance.persistentMiddlewares, middleware)
		return instance
	}
	if instance.generation.started {
		panic(fmt.Sprintf("http server %q middleware registration after start", instance.name))
	}
	instance.generation.middlewares = append(instance.generation.middlewares, middleware)
	return instance
}

func (instance *restServer) registerRoute(route *rest.Route) {
	if instance.generation != nil && instance.generation.started {
		panic(fmt.Sprintf("http server %q route registration after start", instance.name))
	}

	routes := make([]*rest.Route, 0, len(instance.persistentRoutes)+1)
	routes = append(routes, instance.persistentRoutes...)
	if instance.generation != nil {
		routes = append(routes, instance.generation.routes...)
	}
	routes = append(routes, route)
	if _, err := rest.MakeRouter(routes...); err != nil {
		panic(fmt.Errorf("http server %q invalid route registration: %w", instance.name, err))
	}

	if instance.generation == nil {
		instance.persistentRoutes = append(instance.persistentRoutes, route)
	} else {
		instance.generation.routes = append(instance.generation.routes, route)
	}
}

func (instance *restServer) AfterStart() {
	generation := instance.generation
	if generation == nil {
		panic(fmt.Sprintf("http server %q has not been initialized", instance.name))
	}
	if generation.started {
		return
	}

	api := instance.newAPI()
	middlewares := make([]Middleware, 0, len(instance.persistentMiddlewares)+len(generation.middlewares))
	middlewares = append(middlewares, instance.persistentMiddlewares...)
	middlewares = append(middlewares, generation.middlewares...)
	for _, middleware := range middlewares {
		api.Use(rest.MiddlewareSimple(func(handler rest.HandlerFunc) rest.HandlerFunc {
			return func(writer rest.ResponseWriter, request *rest.Request) {
				if err := middleware(handler, writer, request); err != nil {
					instance.l.Error("on middleware:", err)
					writer.WriteHeader(http.StatusInternalServerError)
				}
			}
		}))
	}

	routes := make([]*rest.Route, 0, len(instance.persistentRoutes)+len(generation.routes))
	routes = append(routes, instance.persistentRoutes...)
	routes = append(routes, generation.routes...)
	api.SetApp(u.Must2(rest.MakeRouter(routes...)))
	generation.server.Handler = &requestSizeLimitHandlerWrapper{
		handler:        api.MakeHandler(),
		maxRequestSize: generation.requestSizeLimit,
	}
	generation.started = true

	go func() {
		defer close(generation.serveDone)
		instance.l.Info("http server started on", generation.listener.Addr().String())
		if err := generation.server.Serve(generation.listener); err != nil &&
			!errors.Is(err, http.ErrServerClosed) &&
			!errors.Is(err, net.ErrClosed) {
			instance.l.Error("http server stopped unexpectedly:", err)
		} else {
			instance.l.Debug("http server stopped")
		}
	}()
}

func (instance *restServer) BeforeStop() {
	generation := instance.generation
	if generation == nil {
		return
	}
	if err := instance.stopGeneration(generation); err != nil {
		instance.l.Error("error on http server shutdown:", err)
	}
}

func (instance *restServer) Dispose() error {
	generation := instance.generation
	if generation == nil {
		return nil
	}

	err := instance.stopGeneration(generation)
	if instance.generation == generation {
		instance.generation = nil
	}
	return err
}

func (instance *restServer) stopGeneration(generation *restServerGeneration) error {
	generation.stopOnce.Do(func() {
		generation.cancelRequests()
		if generation.started {
			timeoutContext, cancelFunc := context.WithTimeout(context.Background(), 5*time.Second)
			generation.stopErr = generation.server.Shutdown(timeoutContext)
			cancelFunc()
			if generation.stopErr != nil {
				_ = generation.server.Close()
			}
			<-generation.serveDone
		} else if err := generation.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			generation.stopErr = err
		}
	})
	return generation.stopErr
}

func (instance *restServer) newAPI() *rest.Api {
	api := rest.NewApi()
	logFormat := "[" + instance.name + "] %h %l %u \"%r\" %s %b"
	debugLoggerAdapter := log.New(&logAdapter{loggingFn: func(msg string) {
		instance.l.Debug(msg)
	}}, "", 0)
	errorLoggerAdapter := log.New(&logAdapter{loggingFn: func(msg string) {
		instance.l.Error(msg)
	}}, "", 0)

	if instance.silent {
		api.Use(&rest.RecoverMiddleware{Logger: errorLoggerAdapter})
	} else {
		api.Use(
			&rest.AccessLogApacheMiddleware{
				Logger: debugLoggerAdapter,
				Format: rest.AccessLogFormat(logFormat),
			},
			createPrometheusMiddleware(instance.name),
			&rest.TimerMiddleware{},
			&rest.RecorderMiddleware{},
			&rest.RecoverMiddleware{Logger: errorLoggerAdapter},
		)
	}
	return api
}

func (instance *restServer) boundAddress() (net.Addr, bool) {
	if instance.generation == nil || instance.generation.listener == nil {
		return nil, false
	}
	return instance.generation.listener.Addr(), true
}

func effectiveBindAddress(address string) string {
	if address == "" {
		return ":http"
	}
	return address
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
	return &typedRqHandler[T]{rqHandlerBase{server: server, methods: map[string]bool{method: true}, path: path}}
}

func BuildTypedRoute[T any](server RestServer) TypedRequestHandler[T] {
	return &typedRqHandler[T]{rqHandlerBase{server: server}}
}

func RegisterRoute(server RestServer, method string, path string) RequestHandler {
	return &rqHandler{rqHandlerBase{server: server, methods: map[string]bool{method: true}, path: path}}
}

func BuildRoute(server RestServer) RequestHandler {
	return &rqHandler{rqHandlerBase{server: server}}
}

type logAdapter struct {
	loggingFn func(msg string)
}

func (a *logAdapter) Write(p []byte) (n int, err error) {
	a.loggingFn(string(p))
	return len(p), nil
}
