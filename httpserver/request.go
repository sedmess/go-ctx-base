package httpserver

import (
	"github.com/ant0ine/go-json-rest/rest"
	"net/url"
)

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
