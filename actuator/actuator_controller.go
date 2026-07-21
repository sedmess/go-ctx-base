package actuator

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"github.com/ant0ine/go-json-rest/rest"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sedmess/go-ctx-base/httpserver"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/ctx/logger"
	"net/http"
	"strings"
)

const controllerName = "base.actuator-controller"
const actuatorTokenConfigKey = "ACTUATOR_HTTP_AUTH_TOKENS"

type controller struct {
	l logger.Logger `ctx:""`

	serverServiceName string

	appContext ctx.AppContext `ctx:"CTX"`

	promHandler http.Handler
}

func (c *controller) Init(provider ctx.ServiceProvider) error {
	server := provider.ByName(c.serverServiceName).(httpserver.RestServer)
	authentication, err := actuatorAccessMiddleware(server)
	if err != nil {
		return err
	}

	c.l.Info("register on server", c.serverServiceName)

	c.promHandler = promhttp.Handler()

	healthRoute := httpserver.RegisterRoute(server, http.MethodGet, "/actuator/health")
	healthPlainRoute := httpserver.RegisterRoute(server, http.MethodGet, "/actuator/health/plain")
	servicesRoute := httpserver.RegisterRoute(server, http.MethodGet, "/actuator/services")
	metricsRoute := httpserver.RegisterRoute(server, http.MethodGet, "/actuator/metrics")
	if authentication != nil {
		healthRoute.Middleware(authentication)
		healthPlainRoute.Middleware(authentication)
		servicesRoute.Middleware(authentication)
		metricsRoute.Middleware(authentication)
	}
	healthRoute.Handler(c.health)
	healthPlainRoute.HandlerRaw(c.healthPlainText)
	servicesRoute.Handler(c.services)
	metricsRoute.HandlerRaw(c.metrics)
	return nil
}

func (c *controller) Name() string {
	return controllerName
}

func (c *controller) health(*httpserver.RequestData) (rs httpserver.Response) {
	rs.Ok().Content(c.appContext.Health().Aggregate())
	return
}

func (c *controller) healthPlainText(_ *httpserver.RequestData, responseWriter rest.ResponseWriter) error {
	statusString := fmt.Sprint(c.appContext.Health().Aggregate().Status)
	w := responseWriter.(http.ResponseWriter)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write([]byte(statusString))
	return err
}

func (c *controller) services(*httpserver.RequestData) (rs httpserver.Response) {
	services := c.appContext.Stats().Services()
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

func (c *controller) metrics(request *httpserver.RequestData, responseWriter rest.ResponseWriter) error {
	c.promHandler.ServeHTTP(responseWriter.(http.ResponseWriter), request.Request)
	return nil
}

func actuatorAccessMiddleware(server httpserver.RestServer) (httpserver.Middleware, error) {
	tokenHashes, configured, err := configuredTokenHashes(actuatorTokenConfigKey)
	if err != nil {
		return nil, err
	}
	if !configured {
		loopback, inspectErr := httpserver.IsLoopbackOnly(server)
		if inspectErr != nil || !loopback {
			return nil, fmt.Errorf("actuator exposure is not proven loopback-only; configure %s", actuatorTokenConfigKey)
		}
		return nil, nil
	}
	return httpserver.BearerTokenAuthenticator(func(_ string, token string) httpserver.AuthenticationResultCode {
		candidate := sha256.Sum256([]byte(token))
		for _, expected := range tokenHashes {
			if subtle.ConstantTimeCompare(candidate[:], expected[:]) == 1 {
				return httpserver.Authorized
			}
		}
		return httpserver.Forbidden
	}), nil
}

func configuredTokenHashes(key string) ([][sha256.Size]byte, bool, error) {
	value := ctx.GetEnv(key)
	if !value.IsPresent() {
		return nil, false, nil
	}
	unique := make(map[[sha256.Size]byte]struct{})
	for _, rawToken := range strings.Split(value.AsString(), ",") {
		token := strings.TrimSpace(rawToken)
		if token == "" {
			return nil, true, fmt.Errorf("%s contains an empty token", key)
		}
		unique[sha256.Sum256([]byte(token))] = struct{}{}
	}
	hashes := make([][sha256.Size]byte, 0, len(unique))
	for hash := range unique {
		hashes = append(hashes, hash)
	}
	return hashes, true, nil
}
