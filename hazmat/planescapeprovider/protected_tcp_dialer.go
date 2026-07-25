package planescapeprovider

import (
	"context"
	"net"
	"net/netip"
	"time"
)

// ProtectedBrokerTCPDialerV1 dials one exact, pre-resolved TCP endpoint. It
// owns no resolver, address list, proxy, tunnel, or fallback transport.
type ProtectedBrokerTCPDialerV1 struct {
	target      netip.AddrPort
	dialTimeout time.Duration
}

var _ ProtectedBrokerDialerV1 = (*ProtectedBrokerTCPDialerV1)(nil)

// NewProtectedBrokerTCPDialerV1 validates one exact numeric address and a
// finite, positive per-attempt timeout.
func NewProtectedBrokerTCPDialerV1(
	target netip.AddrPort,
	dialTimeout time.Duration,
) (*ProtectedBrokerTCPDialerV1, error) {
	if !validProtectedBrokerTCPTargetV1(target) || dialTimeout <= 0 {
		return nil, protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	return &ProtectedBrokerTCPDialerV1{
		target:      target,
		dialTimeout: dialTimeout,
	}, nil
}

// DialContext creates one fresh stream to the configured endpoint. The caller
// context and configured timeout are both authoritative; whichever ends first
// aborts this attempt.
func (d *ProtectedBrokerTCPDialerV1) DialContext(
	ctx context.Context,
) (ProtectedBrokerStreamV1, error) {
	if ctx == nil {
		return nil, protectedBrokerError(ProtectedBrokerInvalidRequestV1)
	}
	if d == nil || !d.valid() {
		return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	if ctx.Err() != nil {
		return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}

	dialCtx, cancel := context.WithTimeout(ctx, d.dialTimeout)
	defer cancel()

	network := "tcp6"
	if d.target.Addr().Is4() {
		network = "tcp4"
	}
	stream, err := (&net.Dialer{}).DialContext(dialCtx, network, d.target.String())
	if err != nil {
		if dialCtx.Err() != nil || ctx.Err() != nil {
			return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
		}
		return nil, mapProtectedBrokerIOError(err)
	}
	if dialCtx.Err() != nil || ctx.Err() != nil {
		_ = stream.Close()
		return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	return stream, nil
}

func (d *ProtectedBrokerTCPDialerV1) valid() bool {
	return d != nil &&
		validProtectedBrokerTCPTargetV1(d.target) &&
		d.dialTimeout > 0
}

func validProtectedBrokerTCPTargetV1(target netip.AddrPort) bool {
	return target.IsValid() &&
		target.Port() != 0 &&
		!target.Addr().IsUnspecified() &&
		!target.Addr().IsMulticast()
}
