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

// Credential returns the request-scoped numeric identity established by successful
// authentication middleware. It is not authorization proof for another request or policy;
// absent and invalid request-local state returns zero.
func (d *RequestData) Credential() int64 {
	if d == nil || d.Env == nil {
		return 0
	}
	credential, found := d.Env[credentialEnvKey]
	if !found {
		return 0
	}
	numericCredential, valid := credential.(int64)
	if !valid {
		return 0
	}
	return numericCredential
}
