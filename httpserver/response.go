package httpserver

import (
	"fmt"
	"net/http"
)

type Response struct {
	httpStatus int
	content    any
	err        error
}

func (h *Response) VerifyNotEmpty(strs ...string) bool {
	for _, str := range strs {
		if str == "" {
			h.BadRequest()
			return false
		}
	}
	return true
}

func (h *Response) Ok() *Response {
	h.httpStatus = http.StatusOK
	h.err = nil
	return h
}

func (h *Response) Status(status int) *Response {
	h.httpStatus = status
	h.err = nil
	return h
}

func (h *Response) BadRequest() *Response {
	h.httpStatus = http.StatusBadRequest
	h.err = nil
	return h
}

func (h *Response) BadRequestReason(reason ...string) *Response {
	h.httpStatus = http.StatusBadRequest
	h.content = fmt.Sprint(reason)
	return h
}

func (h *Response) Content(content any) *Response {
	if h.httpStatus == 0 {
		h.httpStatus = http.StatusOK
	}
	h.content = content
	return h
}

func (h *Response) Error(err error) *Response {
	h.err = err
	return h
}
