package httpserver

import (
	"github.com/ant0ine/go-json-rest/rest"
	"github.com/sedmess/go-ctx/logger"
	"net/http"
	"net/url"
)

type RqHandler[Rq any] interface {
	Handle(handler func() (response any, httpStatus int, err error)) func(rest.ResponseWriter, *rest.Request)
	HandleRequest(handler func(request *RequestData) (response any, httpStatus int, err error)) func(rest.ResponseWriter, *rest.Request)
	HandleRequestBody(handler func(request *RequestData, body Rq) (response any, httpStatus int, err error)) func(rest.ResponseWriter, *rest.Request)
	HandleRaw(handler func(request *RequestData, responseWriter rest.ResponseWriter) error) func(rest.ResponseWriter, *rest.Request)
}
type requestHandler[Rq any] struct {
	l logger.Logger
}

type RequestData rest.Request

func (d *RequestData) Path() map[string]string {
	return d.PathParams
}

func (d *RequestData) Query() url.Values {
	return d.URL.Query()
}

func (d *RequestData) Credential() int64 {
	if cred, found := d.Env[credentialEnvKey]; found {
		return cred.(int64)
	} else {
		return 0
	}
}

func RequestHandler[Rq any](logger logger.Logger) RqHandler[Rq] {
	return &requestHandler[Rq]{l: logger}
}

func (h *requestHandler[Rq]) Handle(handler func() (response any, httpStatus int, err error)) func(rest.ResponseWriter, *rest.Request) {
	return func(w rest.ResponseWriter, r *rest.Request) {
		resp, httpStatus, err := handler()
		if err != nil {
			h.l.Error("on handling request:", err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(httpStatus)
		err = w.WriteJson(&resp)
		if err != nil {
			h.l.Error("on writing response:", err.Error())
			return
		}
	}
}

func (h *requestHandler[Rq]) HandleRequest(handler func(request *RequestData) (response any, httpStatus int, err error)) func(rest.ResponseWriter, *rest.Request) {
	return func(w rest.ResponseWriter, r *rest.Request) {
		resp, httpStatus, err := handler((*RequestData)(r))
		if err != nil {
			h.l.Error("on handling request:", err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(httpStatus)
		err = w.WriteJson(&resp)
		if err != nil {
			h.l.Error("on writing response:", err.Error())
			return
		}
	}
}

func (h *requestHandler[Rq]) HandleRequestBody(handler func(request *RequestData, body Rq) (response any, httpStatus int, err error)) func(rest.ResponseWriter, *rest.Request) {
	return func(w rest.ResponseWriter, r *rest.Request) {
		var rq Rq
		err := r.DecodeJsonPayload(&rq)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			err = w.WriteJson(err.Error())
			if err != nil {
				h.l.Error("on writing response:", err.Error())
				return
			}
			return
		}

		resp, httpStatus, err := handler((*RequestData)(r), rq)
		if err != nil {
			h.l.Error("on handling request:", err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(httpStatus)
		err = w.WriteJson(&resp)
		if err != nil {
			h.l.Error("on writing response:", err.Error())
			return
		}
	}
}

func (h *requestHandler[Rq]) HandleRaw(handler func(request *RequestData, responseWriter rest.ResponseWriter) error) func(rest.ResponseWriter, *rest.Request) {
	return func(w rest.ResponseWriter, r *rest.Request) {
		err := handler((*RequestData)(r), w)
		if err != nil {
			h.l.Error("on handling request:", err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
}
