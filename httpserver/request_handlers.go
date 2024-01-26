package httpserver

import (
	"github.com/ant0ine/go-json-rest/rest"
	"net/http"
)

type TypedRequestHandler[T any] interface {
	Path(path string) TypedRequestHandler[T]
	Method(method string) TypedRequestHandler[T]
	Handler(handler func(request *RequestData, body T) Response)
}

type RequestHandler interface {
	Path(path string) RequestHandler
	Method(method string) RequestHandler
	Handler(handler func(request *RequestData) Response)
	HandlerRaw(handler func(request *RequestData, responseWriter rest.ResponseWriter) error)
}

type rqHandlerBase struct {
	server RestServer
	path   string
	method string
}

type typedRqHandler[T any] struct {
	rqHandlerBase
}

func (r *typedRqHandler[T]) Path(path string) TypedRequestHandler[T] {
	r.path = path
	return r
}

func (r *typedRqHandler[T]) Method(method string) TypedRequestHandler[T] {
	r.method = method
	return r
}

func (r *typedRqHandler[T]) Handler(handler func(request *RequestData, body T) Response) {
	logger := r.server.logger()
	routeFunc := defineRouteFunc(r.method)
	if routeFunc == nil {
		logger.Fatal("unsupported http method:", r.method)
	}
	r.server.registerRoute(routeFunc(r.path, func(w rest.ResponseWriter, r *rest.Request) {
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
	}))
}

type rqHandler struct {
	rqHandlerBase
}

func (r *rqHandler) Path(path string) RequestHandler {
	r.path = path
	return r
}

func (r *rqHandler) Method(method string) RequestHandler {
	r.method = method
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
	routeFunc := defineRouteFunc(r.method)
	if routeFunc == nil {
		logger.Fatal("unsupported http method:", r.method)
	}
	r.server.registerRoute(routeFunc(r.path, func(w rest.ResponseWriter, r *rest.Request) {
		err := handler((*RequestData)(r), w)
		if err != nil {
			logger.Error("on handling request:", err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}))
}

func defineRouteFunc(method string) func(path string, handler rest.HandlerFunc) *rest.Route {
	switch method {
	case http.MethodGet:
		return rest.Get
	case http.MethodHead:
		return rest.Head
	case http.MethodPost:
		return rest.Post
	case http.MethodPut:
		return rest.Put
	case http.MethodPatch:
		return rest.Patch
	case http.MethodDelete:
		return rest.Delete
	case http.MethodOptions:
		return rest.Options
	default:
		return nil
	}
}
