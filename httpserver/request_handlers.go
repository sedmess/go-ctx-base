package httpserver

import (
	"github.com/ant0ine/go-json-rest/rest"
	"golang.org/x/exp/maps"
	"net/http"
)

const routePathEnvKey = "httpserver.route_path"

type TypedRequestHandler[T any] interface {
	Path(path string) TypedRequestHandler[T]
	Method(method string) TypedRequestHandler[T]
	Middleware(middleware Middleware) TypedRequestHandler[T]
	Handler(handler func(request *RequestData, body T) (rs Response))
}

type RequestHandler interface {
	Path(path string) RequestHandler
	Method(method string) RequestHandler
	Middleware(middleware Middleware) RequestHandler
	Handler(handler func(request *RequestData) (rs Response))
	HandlerRaw(handler func(request *RequestData, responseWriter rest.ResponseWriter) error)
}

type rqHandlerBase struct {
	server     RestServer
	path       string
	methods    map[string]bool
	middleware Middleware
}

func (r *rqHandlerBase) addMethod(method string) {
	if r.methods == nil {
		r.methods = map[string]bool{}
	}
	r.methods[method] = true
}

type typedRqHandler[T any] struct {
	rqHandlerBase
}

func (r *typedRqHandler[T]) Path(path string) TypedRequestHandler[T] {
	r.path = path
	return r
}

func (r *typedRqHandler[T]) Method(method string) TypedRequestHandler[T] {
	r.addMethod(method)
	return r
}

func (r *typedRqHandler[T]) Middleware(middleware Middleware) TypedRequestHandler[T] {
	r.middleware = middleware
	return r
}

func (r *typedRqHandler[T]) Handler(handler func(request *RequestData, body T) Response) {
	logger := r.server.logger()
	routes := defineRoutes(r.methods)
	if len(routes) == 0 {
		logger.Fatal("unsupported http method set:", maps.Keys(r.methods))
	}
	handlerFunc := func(w rest.ResponseWriter, r *rest.Request) {
		var rq T
		err := r.DecodeJsonPayload(&rq)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			err = w.WriteJson(err.Error())
			if err != nil {
				logger.Error("on writing response:", err.Error())
				return
			}
			return
		}

		resp := handler((*RequestData)(r), rq)
		if resp.err != nil {
			logger.Error("on handling request:", resp.err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(resp.httpStatus)
		if resp.content != nil {
			err = w.WriteJson(&resp.content)
			if err != nil {
				logger.Error("on writing response:", err.Error())
				return
			}
		}
	}

	if r.middleware != nil {
		innerHandlerFunc := handlerFunc
		handlerFunc = func(w rest.ResponseWriter, rq *rest.Request) {
			if err := r.middleware(innerHandlerFunc, w, rq); err != nil {
				logger.Error("on middleware:", err)
				w.WriteHeader(http.StatusInternalServerError)
			}
		}
	}

	for _, route := range routes {
		r.server.registerRoute(withRouteMetadata(route(r.path, handlerFunc)))
	}
}

type rqHandler struct {
	rqHandlerBase
}

func (r *rqHandler) Path(path string) RequestHandler {
	r.path = path
	return r
}

func (r *rqHandler) Method(method string) RequestHandler {
	r.addMethod(method)
	return r
}

func (r *rqHandler) Middleware(middleware Middleware) RequestHandler {
	r.middleware = middleware
	return r
}

func (r *rqHandler) Handler(handler func(request *RequestData) Response) {
	logger := r.server.logger()
	r.HandlerRaw(func(request *RequestData, w rest.ResponseWriter) error {
		resp := handler(request)
		if resp.err != nil {
			logger.Error("on handling request:", resp.err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return nil
		}

		w.WriteHeader(resp.httpStatus)
		if resp.content != nil {
			err := w.WriteJson(&resp.content)
			if err != nil {
				logger.Error("on writing response:", err.Error())
				return nil
			}
		}
		return nil
	})
}

func (r *rqHandler) HandlerRaw(handler func(request *RequestData, responseWriter rest.ResponseWriter) error) {
	logger := r.server.logger()
	routes := defineRoutes(r.methods)
	if len(routes) == 0 {
		logger.Fatal("unsupported http method set:", maps.Keys(r.methods))
	}
	handlerFunc := func(w rest.ResponseWriter, r *rest.Request) {
		err := handler((*RequestData)(r), w)
		if err != nil {
			logger.Error("on handling request:", err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	if r.middleware != nil {
		innerHandlerFunc := handlerFunc
		handlerFunc = func(w rest.ResponseWriter, rq *rest.Request) {
			if err := r.middleware(innerHandlerFunc, w, rq); err != nil {
				logger.Error("on middleware:", err)
				w.WriteHeader(http.StatusInternalServerError)
			}
		}
	}

	for _, route := range routes {
		r.server.registerRoute(withRouteMetadata(route(r.path, handlerFunc)))
	}
}

// withRouteMetadata attaches the finite registered expression before any
// route-specific middleware or handler runs. Outer server middleware can read
// it after handling without using request-controlled URL data.
func withRouteMetadata(route *rest.Route) *rest.Route {
	pathExpression := route.PathExp
	handler := route.Func
	route.Func = func(writer rest.ResponseWriter, request *rest.Request) {
		if request.Env == nil {
			request.Env = make(map[string]interface{})
		}
		request.Env[routePathEnvKey] = pathExpression
		handler(writer, request)
	}
	return route
}

func defineRoutes(methods map[string]bool) (res []func(path string, handler rest.HandlerFunc) *rest.Route) {
	if len(methods) == 0 {
		res = []func(path string, handler rest.HandlerFunc) *rest.Route{rest.Get, rest.Post}
		return
	}
	for method := range methods {
		switch method {
		case http.MethodGet:
			res = append(res, rest.Get)
		case http.MethodHead:
			res = append(res, rest.Head)
		case http.MethodPost:
			res = append(res, rest.Post)
		case http.MethodPut:
			res = append(res, rest.Put)
		case http.MethodPatch:
			res = append(res, rest.Patch)
		case http.MethodDelete:
			res = append(res, rest.Delete)
		case http.MethodOptions:
			res = append(res, rest.Options)
		default:
			res = nil
			return
		}
	}
	return
}
