package db

import (
	"github.com/sedmess/go-ctx/ctx"
	"github.com/sedmess/go-ctx/u"
	"sync"
)

var defaultServices = sync.OnceValue(func() ctx.ServicePackage {
	return ctx.PackageOf(
		NewConnection(u.GetInterfaceName[Connection](), "BASE", true, true),
	)
})

func Default() ctx.ServicePackage {
	return defaultServices()
}
