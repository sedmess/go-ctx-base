package httpserver

import (
	"errors"
	"net"
	"reflect"
)

// IsLoopbackOnly reports whether an initialized built-in REST server is bound to a loopback
// address. An error means local-only exposure could not be proven.
func IsLoopbackOnly(server RestServer) (bool, error) {
	if server == nil {
		return false, errors.New("REST server is nil")
	}
	value := reflect.ValueOf(server)
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return false, errors.New("REST server is nil")
	}
	builtIn, ok := server.(*restServer)
	if !ok {
		return false, errors.New("REST server binding cannot be inspected")
	}
	address, ok := builtIn.boundAddress()
	if !ok {
		return false, errors.New("REST server is not initialized")
	}
	tcpAddress, ok := address.(*net.TCPAddr)
	if !ok || tcpAddress.IP == nil {
		return false, errors.New("REST server bound address is not TCP")
	}
	return tcpAddress.IP.IsLoopback(), nil
}
