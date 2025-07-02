package profiler

import (
	"bytes"
	"errors"
	"github.com/ant0ine/go-json-rest/rest"
	"github.com/sedmess/go-ctx-base/httpserver"
	"github.com/sedmess/go-ctx-base/utils/values"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/ctx/logger"
	"net/http"
	"runtime/pprof"
	"runtime/trace"
	"strconv"
	"time"
)

type Controller struct {
	l logger.Logger `ctx:""`

	serverServiceName string
}

func (c *Controller) Init(provider ctx.ServiceProvider) {
	if c.serverServiceName != "" {
		server := provider.ByName(c.serverServiceName).(httpserver.RestServer)

		c.l.Info("register on server", c.serverServiceName)

		httpserver.RegisterRoute(server, http.MethodGet, "/profiler/trace").HandlerRaw(func(request *httpserver.RequestData, responseWriter rest.ResponseWriter) error {
			duration := values.IfError(time.ParseDuration(request.Query().Get("duration"))).OrDefault(time.Second * 15)
			c.l.Info("tracing for", duration)
			buffer, err := c.Trace(duration)
			if err != nil {
				return err
			}
			w := responseWriter.(http.ResponseWriter)
			httpserver.SetBinaryFileHeader(w.Header(), "trace.pprof")
			_, err = w.Write(buffer.Bytes())
			return err
		})
		httpserver.RegisterRoute(server, http.MethodGet, "/profiler/cpu_profile").HandlerRaw(func(request *httpserver.RequestData, responseWriter rest.ResponseWriter) error {
			duration := values.IfError(time.ParseDuration(request.Query().Get("duration"))).OrDefault(time.Second * 15)
			c.l.Info("cpu profiling for", duration)
			buffer, err := c.Profile(duration)
			if err != nil {
				return err
			}
			w := responseWriter.(http.ResponseWriter)
			httpserver.SetBinaryFileHeader(w.Header(), "profile.pprof")
			_, err = w.Write(buffer.Bytes())
			return err
		})
		httpserver.RegisterRoute(server, http.MethodGet, "/profiler/named_profile").HandlerRaw(func(request *httpserver.RequestData, responseWriter rest.ResponseWriter) error {
			name := request.Query().Get("name")
			debug := values.IfError(strconv.Atoi(request.Query().Get("debug"))).OrDefault(0)

			c.l.Info("named profile", name, "debug =", debug)
			buffer, err := c.NamedProfile(name, debug)
			if err != nil {
				return err
			}
			w := responseWriter.(http.ResponseWriter)
			httpserver.SetBinaryFileHeader(w.Header(), name+".pprof")
			_, err = w.Write(buffer.Bytes())
			return err
		})
	}
}

func (c *Controller) Trace(duration time.Duration) (*bytes.Buffer, error) {
	buffer := bytes.NewBuffer(make([]byte, 0))
	err := trace.Start(buffer)
	if err != nil {
		return nil, err
	}
	<-time.After(duration)
	trace.Stop()
	return buffer, nil
}

func (c *Controller) Profile(duration time.Duration) (*bytes.Buffer, error) {
	buffer := bytes.NewBuffer(make([]byte, 0))
	err := pprof.StartCPUProfile(buffer)
	if err != nil {
		return nil, err
	}
	<-time.After(duration)
	pprof.StopCPUProfile()
	return buffer, nil
}

func (c *Controller) NamedProfile(name string, debug int) (*bytes.Buffer, error) {
	profile := pprof.Lookup(name)
	if profile == nil {
		return nil, errors.New("profile \"" + name + "\" not found")
	}
	buffer := bytes.NewBuffer(make([]byte, 0))
	err := profile.WriteTo(buffer, debug)
	if err != nil {
		return nil, err
	}
	return buffer, nil
}
