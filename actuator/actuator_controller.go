package actuator

import (
	"github.com/ant0ine/go-json-rest/rest"
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

	server.RegisterRoute(rest.Get("/actuator/health", httpserver.RequestHandler[any](instance.l).Handle(instance.health)))
	server.RegisterRoute(rest.Get("/actuator/services", httpserver.RequestHandler[any](instance.l).Handle(instance.services)))
}

func (instance *controller) Name() string {
	return controllerName
}

func (instance *controller) health() (any, int, error) {
	return instance.appContext.Health().Aggregate(), http.StatusOK, nil
}

func (instance *controller) services() (any, int, error) {
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
	return result, http.StatusOK, nil
}
