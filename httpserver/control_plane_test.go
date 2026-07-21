package httpserver

import (
	"testing"

	"github.com/ant0ine/go-json-rest/rest"
	"github.com/sedmess/go-ctx/ctx/logger"
)

type opaqueRestServer struct {
	RestServer
}

func (*opaqueRestServer) AddMiddleware(Middleware) RestServer { return nil }
func (*opaqueRestServer) registerRoute(*rest.Route)           {}
func (*opaqueRestServer) logger() logger.Logger               { return logger.New("opaque") }

func TestControlPlaneBindingClassification(t *testing.T) {
	t.Run("IPv4 loopback port zero", func(t *testing.T) {
		server := newLifecycleTestServer(t, "scope-loopback")
		if err := server.Init(); err != nil {
			t.Fatal(err)
		}
		defer server.Dispose()
		loopback, err := IsLoopbackOnly(server)
		if err != nil || !loopback {
			t.Fatalf("loopback = %v, err = %v", loopback, err)
		}
	})

	t.Run("wildcard is not loopback", func(t *testing.T) {
		t.Setenv("SCOPE_WILDCARD_HTTP_LISTEN", "0.0.0.0:0")
		server := NewRestServerSilent("scope-wildcard", "SCOPE_WILDCARD", 0).(*restServer)
		server.l = logger.New("scope-wildcard")
		if err := server.Init(); err != nil {
			t.Fatal(err)
		}
		defer server.Dispose()
		loopback, err := IsLoopbackOnly(server)
		if err != nil {
			t.Fatal(err)
		}
		if loopback {
			t.Fatal("wildcard listener classified as loopback")
		}
	})

	t.Run("IPv6 loopback", func(t *testing.T) {
		t.Setenv("SCOPE_IPV6_HTTP_LISTEN", "[::1]:0")
		server := NewRestServerSilent("scope-ipv6", "SCOPE_IPV6", 0).(*restServer)
		server.l = logger.New("scope-ipv6")
		if err := server.Init(); err != nil {
			t.Skipf("IPv6 loopback is unavailable: %v", err)
		}
		defer server.Dispose()
		loopback, err := IsLoopbackOnly(server)
		if err != nil || !loopback {
			t.Fatalf("loopback = %v, err = %v", loopback, err)
		}
	})
}

func TestControlPlaneBindingClassificationFailures(t *testing.T) {
	if _, err := IsLoopbackOnly(nil); err == nil {
		t.Fatal("nil server classification succeeded")
	}
	var typedNil *restServer
	if _, err := IsLoopbackOnly(typedNil); err == nil {
		t.Fatal("typed nil server classification succeeded")
	}
	uninitialized := NewRestServerSilent("uninitialized", "UNINITIALIZED", 0)
	if _, err := IsLoopbackOnly(uninitialized); err == nil {
		t.Fatal("uninitialized server classification succeeded")
	}
	if _, err := IsLoopbackOnly(&opaqueRestServer{}); err == nil {
		t.Fatal("custom server classification succeeded")
	}
}
