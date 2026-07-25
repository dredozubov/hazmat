package planescapeprovider

// ProtectedBrokerDiscoveryEndpointConfigV1 composes one protected broker
// dialer with immutable authenticated-client authority.
type ProtectedBrokerDiscoveryEndpointConfigV1 struct {
	Dialer ProtectedBrokerDialerV1
	Client *ProtectedBrokerClientV1
}

// NewProtectedBrokerDiscoveryEndpointV1 composes the authenticated discovery
// transport and provider-v1 codec into the existing product Endpoint. The
// discovery-only transport rejects lifecycle frames before dialing.
func NewProtectedBrokerDiscoveryEndpointV1(
	config ProtectedBrokerDiscoveryEndpointConfigV1,
) (Endpoint, error) {
	transport, err := NewProtectedBrokerDiscoveryTransportV1(
		ProtectedBrokerDiscoveryTransportConfigV1{
			Dialer: config.Dialer,
			Client: config.Client,
		},
	)
	if err != nil {
		return nil, err
	}
	endpoint, err := NewFramedEndpoint(FramedEndpointConfig{
		Transport: transport,
		Codec:     ProviderV1FrameCodec{},
	})
	if err != nil {
		return nil, protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	return endpoint, nil
}
