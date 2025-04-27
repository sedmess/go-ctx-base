package actuator

import (
	"github.com/ant0ine/go-json-rest/rest"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sedmess/go-ctx-base/httpserver"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/ctx/logger"
	"net/http"
)

const controllerName = "base.actuator-controller"

type controller struct {
	l logger.Logger `ctx:""`

	serverServiceName string

	appContext ctx.AppContext `ctx:"CTX"`

	promHandler http.Handler
}

func (instance *controller) Init(provider ctx.ServiceProvider) {
	server := provider.ByName(instance.serverServiceName).(httpserver.RestServer)

	instance.l.Info("register on server", instance.serverServiceName)

	instance.promHandler = promhttp.Handler()

	httpserver.RegisterRoute(server, http.MethodGet, "/actuator/health").Handler(instance.health)
	httpserver.RegisterRoute(server, http.MethodGet, "/actuator/services").Handler(instance.services)
	httpserver.RegisterRoute(server, http.MethodGet, "/actuator/metrics").HandlerRaw(instance.metrics)
}

func (instance *controller) Name() string {
	return controllerName
}

func (instance *controller) health(*httpserver.RequestData) (rs httpserver.Response) {
	rs.Ok().Content(instance.appContext.Health().Aggregate())
	return
}

func (instance *controller) services(*httpserver.RequestData) (rs httpserver.Response) {
	services := instance.appContext.Stats().Services()
	result := make(map[string]ServiceDescription)
	for _, descriptor := range services {
		srv := ServiceDescription{
			Name:         descriptor.Name,
			Type:         descriptor.Type.Name(),
			IsStartAware: descriptor.IsStartAware,
			IsStopAware:  descriptor.IsStopAware,
		}
		srv.Dependencies = descriptor.Dependencies
		result[descriptor.Name] = srv
	}
	rs.Ok().Content(result)
	return
}

func (instance *controller) metrics(request *httpserver.RequestData, responseWriter rest.ResponseWriter) error {
	instance.promHandler.ServeHTTP(responseWriter.(http.ResponseWriter), request.Request)
	return nil
}
