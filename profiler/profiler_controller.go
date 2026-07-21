package profiler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"runtime/pprof"
	"runtime/trace"
	"strconv"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"github.com/sedmess/go-ctx-base/httpserver"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/ctx/logger"
)

const profilerTokenConfigKey = "PROFILER_HTTP_AUTH_TOKENS"
const defaultProfileDuration = 15 * time.Second
const maximumProfileDuration = 30 * time.Second

var profileAdmission = make(chan struct{}, 1)
var errProfileBusy = errors.New("another profiling operation is active")
var safeProfileName = regexp.MustCompile("^[A-Za-z0-9_.-]+$")

// Runtime function variables keep the process-global profiler boundary deterministic in
// package tests without changing the exported Controller contract.
var runtimeTraceStart = trace.Start
var runtimeTraceStop = trace.Stop
var runtimeCPUProfileStart = pprof.StartCPUProfile
var runtimeCPUProfileStop = pprof.StopCPUProfile

type Controller struct {
	l logger.Logger `ctx:""`

	serverServiceName string
}

// Init keeps its established signature and delegates validation to an error-returning helper so
// go-ctx v0.12.0 can capture configuration failures at the initialization boundary.
func (c *Controller) Init(provider ctx.ServiceProvider) {
	if err := c.initialize(provider); err != nil {
		panic(err)
	}
}

func (c *Controller) initialize(provider ctx.ServiceProvider) error {
	if c.serverServiceName == "" {
		return nil
	}
	server, ok := provider.ByName(c.serverServiceName).(httpserver.RestServer)
	if !ok || server == nil {
		return fmt.Errorf("profiler server %q is unavailable", c.serverServiceName)
	}
	authentication, err := profilerAccessMiddleware(server)
	if err != nil {
		return err
	}

	c.l.Info("register on server", c.serverServiceName)
	traceRoute := httpserver.RegisterRoute(server, http.MethodGet, "/profiler/trace")
	cpuRoute := httpserver.RegisterRoute(server, http.MethodGet, "/profiler/cpu_profile")
	namedRoute := httpserver.RegisterRoute(server, http.MethodGet, "/profiler/named_profile")
	if authentication != nil {
		traceRoute.Middleware(authentication)
		cpuRoute.Middleware(authentication)
		namedRoute.Middleware(authentication)
	}
	traceRoute.HandlerRaw(c.handleTrace)
	cpuRoute.HandlerRaw(c.handleCPUProfile)
	namedRoute.HandlerRaw(c.handleNamedProfile)
	return nil
}

func (c *Controller) handleTrace(request *httpserver.RequestData, responseWriter rest.ResponseWriter) error {
	duration, err := parseProfileDuration(request.Query())
	if err != nil {
		responseWriter.WriteHeader(http.StatusBadRequest)
		return nil
	}
	release, err := tryAcquireProfile()
	if err != nil {
		responseWriter.Header().Set("Retry-After", "1")
		responseWriter.WriteHeader(http.StatusTooManyRequests)
		return nil
	}
	defer release()

	buffer, err := c.traceContext(request.Request.Context(), duration)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	if err != nil {
		return err
	}
	writer := responseWriter.(http.ResponseWriter)
	httpserver.SetBinaryFileHeader(writer.Header(), "trace.pprof")
	_, err = writer.Write(buffer.Bytes())
	return err
}

func (c *Controller) handleCPUProfile(request *httpserver.RequestData, responseWriter rest.ResponseWriter) error {
	duration, err := parseProfileDuration(request.Query())
	if err != nil {
		responseWriter.WriteHeader(http.StatusBadRequest)
		return nil
	}
	release, err := tryAcquireProfile()
	if err != nil {
		responseWriter.Header().Set("Retry-After", "1")
		responseWriter.WriteHeader(http.StatusTooManyRequests)
		return nil
	}
	defer release()

	buffer, err := c.profileContext(request.Request.Context(), duration)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	if err != nil {
		return err
	}
	writer := responseWriter.(http.ResponseWriter)
	httpserver.SetBinaryFileHeader(writer.Header(), "profile.pprof")
	_, err = writer.Write(buffer.Bytes())
	return err
}

func (c *Controller) handleNamedProfile(request *httpserver.RequestData, responseWriter rest.ResponseWriter) error {
	name, debug, err := parseNamedProfile(request.Query())
	if err != nil {
		responseWriter.WriteHeader(http.StatusBadRequest)
		return nil
	}
	release, err := tryAcquireProfile()
	if err != nil {
		responseWriter.Header().Set("Retry-After", "1")
		responseWriter.WriteHeader(http.StatusTooManyRequests)
		return nil
	}
	defer release()

	buffer, err := c.namedProfileContext(request.Request.Context(), name, debug)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	if err != nil {
		return err
	}
	writer := responseWriter.(http.ResponseWriter)
	httpserver.SetBinaryFileHeader(writer.Header(), name+".pprof")
	_, err = writer.Write(buffer.Bytes())
	return err
}

func parseProfileDuration(query url.Values) (time.Duration, error) {
	if !query.Has("duration") {
		return defaultProfileDuration, nil
	}
	duration, err := time.ParseDuration(query.Get("duration"))
	if err != nil || duration <= 0 || duration > maximumProfileDuration {
		return 0, errors.New("invalid profile duration")
	}
	return duration, nil
}

func parseNamedProfile(query url.Values) (string, int, error) {
	name := query.Get("name")
	if name == "" || !safeProfileName.MatchString(name) || pprof.Lookup(name) == nil {
		return "", 0, errors.New("invalid profile name")
	}
	debug := 0
	var err error
	if query.Has("debug") {
		debug, err = strconv.Atoi(query.Get("debug"))
		if err != nil {
			return "", 0, errors.New("invalid profile debug value")
		}
	}
	if debug < 0 || debug > 2 {
		return "", 0, errors.New("invalid profile debug value")
	}
	return name, debug, nil
}

func tryAcquireProfile() (func(), error) {
	select {
	case profileAdmission <- struct{}{}:
		return func() { <-profileAdmission }, nil
	default:
		return nil, errProfileBusy
	}
}

func (c *Controller) Trace(duration time.Duration) (*bytes.Buffer, error) {
	release, err := tryAcquireProfile()
	if err != nil {
		return nil, err
	}
	defer release()
	return c.traceContext(context.Background(), duration)
}

func (c *Controller) traceContext(operationContext context.Context, duration time.Duration) (*bytes.Buffer, error) {
	buffer := bytes.NewBuffer(nil)
	if err := runtimeTraceStart(buffer); err != nil {
		return nil, err
	}
	defer runtimeTraceStop()
	if err := waitForProfile(operationContext, duration); err != nil {
		return nil, err
	}
	return buffer, nil
}

func (c *Controller) Profile(duration time.Duration) (*bytes.Buffer, error) {
	release, err := tryAcquireProfile()
	if err != nil {
		return nil, err
	}
	defer release()
	return c.profileContext(context.Background(), duration)
}

func (c *Controller) profileContext(operationContext context.Context, duration time.Duration) (*bytes.Buffer, error) {
	buffer := bytes.NewBuffer(nil)
	if err := runtimeCPUProfileStart(buffer); err != nil {
		return nil, err
	}
	defer runtimeCPUProfileStop()
	if err := waitForProfile(operationContext, duration); err != nil {
		return nil, err
	}
	return buffer, nil
}

func (c *Controller) NamedProfile(name string, debug int) (*bytes.Buffer, error) {
	release, err := tryAcquireProfile()
	if err != nil {
		return nil, err
	}
	defer release()
	return c.namedProfileContext(context.Background(), name, debug)
}

func (c *Controller) namedProfileContext(operationContext context.Context, name string, debug int) (*bytes.Buffer, error) {
	if operationContext == nil {
		return nil, errors.New("profile context is nil")
	}
	if err := operationContext.Err(); err != nil {
		return nil, err
	}
	if !safeProfileName.MatchString(name) || debug < 0 || debug > 2 {
		return nil, errors.New("invalid named profile input")
	}
	profile := pprof.Lookup(name)
	if profile == nil {
		return nil, errors.New("profile \"" + name + "\" not found")
	}
	buffer := bytes.NewBuffer(nil)
	if err := profile.WriteTo(buffer, debug); err != nil {
		return nil, err
	}
	if err := operationContext.Err(); err != nil {
		return nil, err
	}
	return buffer, nil
}

func waitForProfile(operationContext context.Context, duration time.Duration) error {
	if operationContext == nil {
		return errors.New("profile context is nil")
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-operationContext.Done():
		return operationContext.Err()
	}
}

func profilerAccessMiddleware(server httpserver.RestServer) (httpserver.Middleware, error) {
	hashes, configured, err := profilerTokenHashes()
	if err != nil {
		return nil, err
	}
	if !configured {
		loopback, inspectErr := httpserver.IsLoopbackOnly(server)
		if inspectErr != nil || !loopback {
			return nil, fmt.Errorf("profiler exposure is not proven loopback-only; configure %s", profilerTokenConfigKey)
		}
		return nil, nil
	}
	return httpserver.BearerTokenAuthenticator(func(_ string, token string) httpserver.AuthenticationResultCode {
		candidate := sha256.Sum256([]byte(token))
		for _, expected := range hashes {
			if subtle.ConstantTimeCompare(candidate[:], expected[:]) == 1 {
				return httpserver.Authorized
			}
		}
		return httpserver.Forbidden
	}), nil
}

func profilerTokenHashes() ([][sha256.Size]byte, bool, error) {
	value := ctx.GetEnv(profilerTokenConfigKey)
	if !value.IsPresent() {
		return nil, false, nil
	}
	unique := make(map[[sha256.Size]byte]struct{})
	for _, rawToken := range strings.Split(value.AsString(), ",") {
		token := strings.TrimSpace(rawToken)
		if token == "" {
			return nil, true, fmt.Errorf("%s contains an empty token", profilerTokenConfigKey)
		}
		unique[sha256.Sum256([]byte(token))] = struct{}{}
	}
	hashes := make([][sha256.Size]byte, 0, len(unique))
	for hash := range unique {
		hashes = append(hashes, hash)
	}
	return hashes, true, nil
}
