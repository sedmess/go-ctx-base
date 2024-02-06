package actuator

import (
	"github.com/sedmess/go-ctx-base/httpserver"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/logger"
	"net/http"
)

const controllerName = "base.actuator-controller"

type controller struct {
	l logger.Logger `logger:""`

	serverServiceName string

	appContext ctx.AppContext `inject:"CTX"`
}

func (instance *controller) Init(provider ctx.ServiceProvider) {
	server := provider.ByName(instance.serverServiceName).(httpserver.RestServer)

	instance.l.Info("register on server", instance.serverServiceName)

	httpserver.RegisterRoute(server, http.MethodGet, "/actuator/health").Handler(instance.health)
	httpserver.BuildRoute(server).Method(http.MethodGet).Path("/actuator/services").Middleware(httpserver.BasicAuthenticator(func(_ string, username string, password string) httpserver.AuthenticationResultCode {
		if username == "admin" && password == "admin" {
			return httpserver.Authorized
		} else {
			return httpserver.Forbidden
		}
	})).Handler(instance.services)
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
			Name:             descriptor.Name,
			Type:             descriptor.Type.Name(),
			IsLifecycleAware: descriptor.IsLifecycleAware,
		}
		srv.Dependencies = descriptor.Dependencies
		result[descriptor.Name] = srv
	}
	rs.Ok().Content(result)
	return
}
