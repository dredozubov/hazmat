package planescapeprovider

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestProtectedBrokerTCPDialerValidatesExactTargetAndTimeout(t *testing.T) {
	validTarget := netip.MustParseAddrPort("127.0.0.1:443")
	tests := map[string]struct {
		target  netip.AddrPort
		timeout time.Duration
	}{
		"invalid address": {
			timeout: time.Second,
		},
		"zero port": {
			target:  netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), 0),
			timeout: time.Second,
		},
		"unspecified address": {
			target:  netip.MustParseAddrPort("0.0.0.0:443"),
			timeout: time.Second,
		},
		"multicast address": {
			target:  netip.MustParseAddrPort("[ff02::1]:443"),
			timeout: time.Second,
		},
		"zero timeout": {
			target: validTarget,
		},
		"negative timeout": {
			target:  validTarget,
			timeout: -time.Second,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			dialer, err := NewProtectedBrokerTCPDialerV1(test.target, test.timeout)
			if dialer != nil {
				t.Fatal("invalid configuration returned a dialer")
			}
			requireStableProtectedBrokerError(
				t,
				err,
				ProtectedBrokerInvalidRequestV1,
				test.target.String(),
			)
		})
	}

	timeout := 250 * time.Millisecond
	dialer, err := NewProtectedBrokerTCPDialerV1(validTarget, timeout)
	if err != nil {
		t.Fatal(err)
	}
	if dialer.target != validTarget || dialer.dialTimeout != timeout {
		t.Fatal("dialer did not retain the exact target and timeout")
	}
}

func TestProtectedBrokerTCPDialerCreatesFreshLoopbackStreams(t *testing.T) {
	listener := mustProtectedBrokerLoopbackListener(t)
	dialer, err := NewProtectedBrokerTCPDialerV1(listener.Addr().(*net.TCPAddr).AddrPort(), time.Second)
	if err != nil {
		t.Fatal(err)
	}

	accepted := make(chan *net.TCPConn, 2)
	acceptErrors := make(chan error, 1)
	go func() {
		for range 2 {
			connection, acceptErr := listener.AcceptTCP()
			if acceptErr != nil {
				acceptErrors <- acceptErr
				return
			}
			accepted <- connection
		}
	}()

	first, err := dialer.DialContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := dialer.DialContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if first == second {
		t.Fatal("consecutive dials returned the same stream")
	}

	firstTCP, ok := first.(*net.TCPConn)
	if !ok {
		t.Fatalf("first stream type = %T, want *net.TCPConn", first)
	}
	secondTCP, ok := second.(*net.TCPConn)
	if !ok {
		t.Fatalf("second stream type = %T, want *net.TCPConn", second)
	}
	if firstTCP.LocalAddr().String() == secondTCP.LocalAddr().String() {
		t.Fatal("fresh streams reused one local TCP endpoint")
	}

	for range 2 {
		select {
		case connection := <-accepted:
			_ = connection.Close()
		case acceptErr := <-acceptErrors:
			t.Fatal(acceptErr)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for fresh loopback connection")
		}
	}
}

func TestProtectedBrokerTCPDialerCancellationAndUnavailableAreRedacted(t *testing.T) {
	t.Run("canceled context does not connect", func(t *testing.T) {
		listener := mustProtectedBrokerLoopbackListener(t)
		target := listener.Addr().(*net.TCPAddr).AddrPort()
		dialer, err := NewProtectedBrokerTCPDialerV1(target, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		stream, err := dialer.DialContext(ctx)
		if stream != nil {
			_ = stream.Close()
			t.Fatal("canceled dial returned a stream")
		}
		requireStableProtectedBrokerError(t, err, ProtectedBrokerUnavailableV1, target.String())
		requireNoProtectedBrokerTCPAccept(t, listener)
	})

	t.Run("unavailable exact target does not fall back", func(t *testing.T) {
		decoy := mustProtectedBrokerLoopbackListener(t)
		unavailable := mustProtectedBrokerLoopbackListener(t)
		target := unavailable.Addr().(*net.TCPAddr).AddrPort()
		if err := unavailable.Close(); err != nil {
			t.Fatal(err)
		}

		dialer, err := NewProtectedBrokerTCPDialerV1(target, 250*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		stream, err := dialer.DialContext(context.Background())
		if stream != nil {
			_ = stream.Close()
			t.Fatal("unavailable target returned a stream")
		}
		requireStableProtectedBrokerError(t, err, ProtectedBrokerUnavailableV1, target.String())
		requireNoProtectedBrokerTCPAccept(t, decoy)
	})

	t.Run("nil context is invalid without dialing", func(t *testing.T) {
		listener := mustProtectedBrokerLoopbackListener(t)
		target := listener.Addr().(*net.TCPAddr).AddrPort()
		dialer, err := NewProtectedBrokerTCPDialerV1(target, time.Second)
		if err != nil {
			t.Fatal(err)
		}

		stream, err := dialer.DialContext(nil)
		if stream != nil {
			_ = stream.Close()
			t.Fatal("nil-context dial returned a stream")
		}
		requireStableProtectedBrokerError(t, err, ProtectedBrokerInvalidRequestV1, target.String())
		requireNoProtectedBrokerTCPAccept(t, listener)
	})
}

func mustProtectedBrokerLoopbackListener(t *testing.T) *net.TCPListener {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{
		IP: net.IPv4(127, 0, 0, 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})
	return listener
}

func requireNoProtectedBrokerTCPAccept(t *testing.T, listener *net.TCPListener) {
	t.Helper()
	if err := listener.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	connection, err := listener.AcceptTCP()
	if err == nil {
		_ = connection.Close()
		t.Fatal("unexpected fallback TCP connection")
	}
	var networkError net.Error
	if !errors.As(err, &networkError) || !networkError.Timeout() {
		t.Fatalf("accept error = %v, want timeout", err)
	}
}

func requireStableProtectedBrokerError(
	t *testing.T,
	err error,
	want ProtectedBrokerTransportErrorClassV1,
	forbidden ...string,
) {
	t.Helper()
	requireProtectedBrokerErrorClass(t, err, want)
	if got, expected := err.Error(), "protected broker transport failure: "+string(want); got != expected {
		t.Fatalf("error = %q, want stable %q", got, expected)
	}
	for _, value := range forbidden {
		if value != "" && strings.Contains(err.Error(), value) {
			t.Fatalf("error exposed transport detail %q", value)
		}
	}
}
