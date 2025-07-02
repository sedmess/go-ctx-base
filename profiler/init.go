package profiler

import (
	"github.com/sedmess/go-ctx-base/httpserver"
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/u"
	"sync"
)

const defaultServerName = "base.profiler-http-server"

func AddToDefaultHttpServer() any {
	return &Controller{serverServiceName: u.GetInterfaceName[httpserver.RestServer]()}
}

func AddToHttpServer(serverServiceName string) any {
	return &Controller{serverServiceName: serverServiceName}
}

var independentServerServices = sync.OnceValue(func() ctx.ServicePackage {
	return ctx.PackageOf(
		httpserver.NewRestServerSilent(defaultServerName, "PROFILER", 8099),
		&Controller{serverServiceName: defaultServerName},
	)
})

func RunAsIndependentServer() ctx.ServicePackage {
	return independentServerServices()
}

func Default() ctx.ServicePackage {
	return ctx.PackageOf(&Controller{})
}
