package httpserver

import (
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/u"
	"sync"
)

var defaultServices = sync.OnceValue(func() ctx.ServicePackage {
	return ctx.PackageOf(
		NewRestServer(u.GetInterfaceName[RestServer](), "BASE"),
	)
})

func Default() ctx.ServicePackage {
	return defaultServices()
}
