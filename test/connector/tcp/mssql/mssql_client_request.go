package mssqltest

import (
	"net"
)

// ephemeralListenerOnPort creates a net.Listener (with a short deadline) on some given
// port. Note that passing in a port of "0" will result in a random port being used.
func ephemeralListenerOnPort(port string) (net.Listener, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return nil, err
	}

	// We generally don't want to wait forever for a connection to come in
	//err = listener.(*net.TCPListener).SetDeadline(time.Now().Add(10 * time.Second))
	//if err != nil {
	//	return nil, err
	//}

	return listener, nil
}

// clientRequest is the request of an MSSQL client making a connection to a database via
// the Secretless proxyService. The fields on the struct are some of the values that the client has
// control over. Credentials are not included in this struct because those will be
// injected
type clientRequest struct {
	database string
	readOnly bool
	query    string
}

// cloneCredentials creates an independent clone of a credentials map. The resulting
// clone will not be affected by any mutations to the original, and vice-versa. The clone
// is useful for passing to a proxyService service, to avoid zeroization of the original.
func cloneCredentials(original map[string][]byte) map[string][]byte {
	credsClone := make(map[string][]byte)

	for key, value := range original {
		// Clone the value
		valueClone := make([]byte, len(value))
		copy(valueClone, value)

		// Set the key, value pair on the credentials clone
		credsClone[key] = valueClone
	}

	return credsClone
}

// proxyRequest issues a client request using a the 'executor' argument to a Secretless
// proxy service configured using the 'credentials' argument.
// proxyRequest uses newInProcessProxyService to creating the in-process proxy service.
// The proxy service exists only for the lifetime of this method call.
func (clientReq clientRequest) proxyRequest(
	executor dbClientExecutor,
	credentials map[string][]byte,
) (string, string, error) {
	// Create in-process proxy service
	proxyService, err := newInProcessProxyService(credentials)
	if err != nil {
		return "", "", err
	}

	// Ensure the proxy service is stopped
	defer proxyService.Stop()
	// Start the proxyService service. Note
	proxyService.Start()

	// Make the client request to the proxy service
	clientResChan := concurrentClientExec(
		executor,
		dbClientConfig{
			Host:     proxyService.host,
			Port:     proxyService.port,
			Username: "dummy",
			Password: "dummy",
			Database: clientReq.database,
			ReadOnly: clientReq.readOnly,
		},
		clientReq.query,
	)

	clientRes := <-clientResChan
	return clientRes.out, proxyService.port, clientRes.err
}

// proxyToCreatedMock issues a client request using a the 'executor' argument to a Secretless
// proxy service configured using the 'credentials' argument.
// proxyRequest uses newInProcessProxyService to creating the in-process proxy service.
// The proxy service exists only for the lifetime of this method call.
//
// NOTE: proxyToCreatedMock proxies the request to a mock server that terminates the request after the handshake
// This can have unintended effects. gomssql in particular does some weird retry, when a query is prepared!
// TODO: find out this weird gomssqldb behavior.
func (clientReq clientRequest) proxyToCreatedMock(
	executor dbClientExecutor,
	credentials map[string][]byte,
) (*mockTargetCapture, string, error) {
	// Create mock target
	mt, err := newMockTarget("0")
	if err != nil {
		return nil, "", err
	}
	defer mt.close()

	// Gather credentials
	baseCredentials := map[string][]byte{
		"host": []byte(mt.host),
		"port": []byte(mt.port),
	}
	for key, value := range credentials {
		baseCredentials[key] = value
	}

	// Accept on mock target
	mtResChan := mt.accept()

	// We don't expect anything useful to come back from the client request.
	// This is a fire and forget
	_, secretlessPort, _ := clientReq.proxyRequest(
		executor,
		baseCredentials,
	)

	mtRes := <-mtResChan

	return mtRes.capture, secretlessPort, mtRes.err
}

func (clientReq clientRequest) proxyToMock(
	executor dbClientExecutor,
	credentials map[string][]byte,
	mt *mockTarget,
) (*mockTargetCapture, string, error) {
	// Gather credentials
	baseCredentials := map[string][]byte{
		"host": []byte(mt.host),
		"port": []byte(mt.port),
	}
	for key, value := range credentials {
		baseCredentials[key] = value
	}

	// Accept on mock target
	mtResChan := mt.accept()

	// We don't expect anything useful to come back from the client request.
	// This is a fire and forget.
	_, secretlessPort, _ := clientReq.proxyRequest(
		executor,
		baseCredentials,
	)

	mtRes := <-mtResChan

	return mtRes.capture, secretlessPort, mtRes.err
}
