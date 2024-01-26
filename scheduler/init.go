package scheduler

import (
	"github.com/sedmess/go-ctx/ctx"
	"sync"
)

var defaultServices = sync.OnceValue(func() ctx.ServicePackage {
	return ctx.PackageOf(
		&Scheduler{},
		&Locker{},
	)
})

func Default() ctx.ServicePackage {
	return defaultServices()
}
