package planescapeprovider

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestProtectedBrokerDiscoveryEndpointUsesFreshTCPConnectionsAndClosesExactly(t *testing.T) {
	harness := newProtectedBrokerDiscoveryHarness(t)
	target, results := startProtectedBrokerLoopbackDiscoveryServer(t, harness, 2)
	endpoint := mustProtectedBrokerLoopbackDiscoveryEndpoint(t, target, harness.client)

	first, err := endpoint.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := endpoint.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalHash() != second.CanonicalHash() {
		t.Fatal("capabilities changed across fresh TCP connections")
	}

	for range 2 {
		observation := harness.nextObservation(t)
		if observation.err != nil {
			t.Fatal(observation.err)
		}
		result := nextProtectedBrokerLoopbackServerResult(t, results)
		if result.err != nil {
			t.Fatal(result.err)
		}
	}
}

func TestProtectedBrokerDiscoveryEndpointRejectsHostileTCPPeers(t *testing.T) {
	t.Run("wrong backend identity", func(t *testing.T) {
		harness := newProtectedBrokerDiscoveryHarness(t)
		fixture := loadProtectedBrokerInteropFixture(t)
		alternateIdentity, err := NewProtectedBrokerBackendIdentityV1(
			ProtectedBrokerBackendIdentityInputV1{
				BackendInstanceSHA256:      fixture.BackendInstanceSHA256,
				ExecutableSHA256:           fixture.ExecutableSHA256,
				ExecutionEnvironmentSHA256: fixture.ExecutionEnvironmentSHA256,
				ProfileSHA256:              repeatedProtectedBrokerTestHash(5),
				BrokerEpoch:                fixture.BrokerEpoch,
			},
			harness.brokerKey.Public().(ed25519.PublicKey),
		)
		if err != nil {
			t.Fatal(err)
		}
		harness.identity = alternateIdentity

		target, results := startProtectedBrokerLoopbackDiscoveryServer(t, harness, 1)
		endpoint := mustProtectedBrokerLoopbackDiscoveryEndpoint(t, target, harness.client)
		capabilities, err := endpoint.Discover(context.Background())
		if capabilities.valid() {
			t.Fatal("wrong backend identity returned capabilities")
		}
		requireStableProtectedBrokerError(
			t,
			err,
			ProtectedBrokerWrongBrokerIdentityV1,
			target.String(),
		)
		_ = harness.nextObservation(t)
		result := nextProtectedBrokerLoopbackServerResult(t, results)
		if result.err != nil {
			t.Fatal(result.err)
		}
	})

	t.Run("hostile discovery signer", func(t *testing.T) {
		harness := newProtectedBrokerDiscoveryHarness(t)
		harness.responseSigner = ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
		target, results := startProtectedBrokerLoopbackDiscoveryServer(t, harness, 1)
		endpoint := mustProtectedBrokerLoopbackDiscoveryEndpoint(t, target, harness.client)

		capabilities, err := endpoint.Discover(context.Background())
		if capabilities.valid() {
			t.Fatal("hostile signer returned capabilities")
		}
		requireStableProtectedBrokerError(
			t,
			err,
			ProtectedBrokerInvalidSignatureV1,
			target.String(),
		)
		observation := harness.nextObservation(t)
		if observation.err != nil {
			t.Fatal(observation.err)
		}
		result := nextProtectedBrokerLoopbackServerResult(t, results)
		if result.err != nil {
			t.Fatal(result.err)
		}
	})
}

func TestProtectedBrokerDiscoveryEndpointRejectsLifecycleBeforeTCPDial(t *testing.T) {
	listener := mustProtectedBrokerLoopbackListener(t)
	target := listener.Addr().(*net.TCPAddr).AddrPort()
	harness := newProtectedBrokerDiscoveryHarness(t)
	endpoint := mustProtectedBrokerLoopbackDiscoveryEndpoint(t, target, harness.client)

	vectors := loadReleasedProviderV1Vectors(t)
	requirement := decodeReleasedProviderV1Record(
		t,
		releasedProviderV1RecordByKind(t, vectors, providerV1KindRequirement),
	).value.(ExecutionRequirement)
	operation := decodeReleasedProviderV1Record(
		t,
		releasedProviderV1RecordByKind(t, vectors, providerV1KindOperation),
	).value.(AgentOperation)
	freeze := decodeReleasedProviderV1Record(
		t,
		releasedProviderV1RecordByKind(t, vectors, providerV1KindFreeze),
	).value.(Freeze)
	cancellation := decodeReleasedProviderV1Record(
		t,
		releasedProviderV1RecordByKind(t, vectors, providerV1KindCancellation),
	).value.(Cancellation)

	_, err := endpoint.Admit(context.Background(), requirement)
	requireStableProtectedBrokerError(t, err, ProtectedBrokerInvalidRequestV1, target.String())
	_, err = endpoint.Operate(context.Background(), operation)
	requireStableProtectedBrokerError(t, err, ProtectedBrokerInvalidRequestV1, target.String())
	_, err = endpoint.Freeze(context.Background(), freeze)
	requireStableProtectedBrokerError(t, err, ProtectedBrokerInvalidRequestV1, target.String())
	_, err = endpoint.Cancel(context.Background(), cancellation)
	requireStableProtectedBrokerError(t, err, ProtectedBrokerInvalidRequestV1, target.String())

	requireNoProtectedBrokerTCPAccept(t, listener)
}

func TestProtectedBrokerDiscoveryEndpointRejectsInvalidComposition(t *testing.T) {
	harness := newProtectedBrokerDiscoveryHarness(t)
	endpoint, err := NewProtectedBrokerDiscoveryEndpointV1(
		ProtectedBrokerDiscoveryEndpointConfigV1{Client: harness.client},
	)
	if endpoint != nil {
		t.Fatal("nil dialer returned an endpoint")
	}
	requireStableProtectedBrokerError(t, err, ProtectedBrokerUnavailableV1)

	listener := mustProtectedBrokerLoopbackListener(t)
	dialer, err := NewProtectedBrokerTCPDialerV1(
		listener.Addr().(*net.TCPAddr).AddrPort(),
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err = NewProtectedBrokerDiscoveryEndpointV1(
		ProtectedBrokerDiscoveryEndpointConfigV1{Dialer: dialer},
	)
	if endpoint != nil {
		t.Fatal("nil protected client returned an endpoint")
	}
	requireStableProtectedBrokerError(t, err, ProtectedBrokerWrongBrokerIdentityV1)
}

type protectedBrokerLoopbackServerResult struct {
	err error
}

type protectedBrokerDeferredServerClose struct {
	net.Conn
	closeCount int
}

func (c *protectedBrokerDeferredServerClose) Close() error {
	c.closeCount++
	return nil
}

func startProtectedBrokerLoopbackDiscoveryServer(
	t *testing.T,
	harness *protectedBrokerDiscoveryHarness,
	connectionCount int,
) (target netip.AddrPort, results <-chan protectedBrokerLoopbackServerResult) {
	t.Helper()
	listener := mustProtectedBrokerLoopbackListener(t)
	resultChannel := make(chan protectedBrokerLoopbackServerResult, connectionCount)
	go func() {
		defer close(resultChannel)
		for range connectionCount {
			connection, err := listener.AcceptTCP()
			if err != nil {
				resultChannel <- protectedBrokerLoopbackServerResult{err: err}
				return
			}
			harness.mu.Lock()
			harness.nextServerNonce++
			serverNonce := harness.nextServerNonce
			harness.mu.Unlock()

			deferredClose := &protectedBrokerDeferredServerClose{Conn: connection}
			harness.serve(deferredClose, serverNonce)
			result := protectedBrokerLoopbackServerResult{}
			if deferredClose.closeCount != 1 {
				result.err = fmt.Errorf(
					"server handler close count = %d, want 1",
					deferredClose.closeCount,
				)
			} else {
				if deadlineErr := connection.SetReadDeadline(time.Now().Add(2 * time.Second)); deadlineErr != nil {
					result.err = deadlineErr
				} else {
					var trailing [1]byte
					read, readErr := connection.Read(trailing[:])
					if read != 0 || !errors.Is(readErr, io.EOF) {
						result.err = fmt.Errorf(
							"client connection remained open: read=%d error=%v",
							read,
							readErr,
						)
					}
				}
			}
			_ = connection.Close()
			resultChannel <- result
		}
	}()
	return listener.Addr().(*net.TCPAddr).AddrPort(), resultChannel
}

func mustProtectedBrokerLoopbackDiscoveryEndpoint(
	t *testing.T,
	target netip.AddrPort,
	client *ProtectedBrokerClientV1,
) Endpoint {
	t.Helper()
	dialer, err := NewProtectedBrokerTCPDialerV1(target, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := NewProtectedBrokerDiscoveryEndpointV1(
		ProtectedBrokerDiscoveryEndpointConfigV1{
			Dialer: dialer,
			Client: client,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return endpoint
}

func nextProtectedBrokerLoopbackServerResult(
	t *testing.T,
	results <-chan protectedBrokerLoopbackServerResult,
) protectedBrokerLoopbackServerResult {
	t.Helper()
	select {
	case result, ok := <-results:
		if !ok {
			t.Fatal("protected broker loopback server stopped early")
		}
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for protected broker loopback server")
		return protectedBrokerLoopbackServerResult{}
	}
}
