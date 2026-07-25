package planescapeprovider

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type frameTransportStub struct {
	response []byte
	err      error
	request  []byte
}

func (s *frameTransportStub) RoundTrip(_ context.Context, request []byte) ([]byte, error) {
	s.request = append([]byte(nil), request...)
	return append([]byte(nil), s.response...), s.err
}

type frameCodecStub struct {
	capabilities ProviderCapabilities
	encode       []byte
}

func (s frameCodecStub) EncodeDiscovery() ([]byte, error) { return s.encode, nil }
func (s frameCodecStub) DecodeCapabilities([]byte) (ProviderCapabilities, error) {
	return s.capabilities, nil
}
func (frameCodecStub) EncodeCompiledContainmentPlan(CompiledContainmentPlan) ([]byte, error) {
	return nil, errors.New("unused")
}
func (frameCodecStub) DecodeAdmission([]byte) (SessionAdmission, error) {
	return SessionAdmission{}, errors.New("unused")
}
func (frameCodecStub) EncodeOperation(AgentOperation) ([]byte, error) {
	return nil, errors.New("unused")
}
func (frameCodecStub) DecodeOperation([]byte) (OperationResponse, error) {
	return nil, errors.New("unused")
}
func (frameCodecStub) EncodeFreeze(Freeze) ([]byte, error) {
	return nil, errors.New("unused")
}
func (frameCodecStub) DecodeFreezeAck([]byte) (FreezeAck, error) {
	return FreezeAck{}, errors.New("unused")
}
func (frameCodecStub) EncodeCancellation(Cancellation) ([]byte, error) {
	return nil, errors.New("unused")
}
func (frameCodecStub) DecodeCancellationAck([]byte) (CancellationAck, error) {
	return CancellationAck{}, errors.New("unused")
}

func TestFramedEndpointValidatesBoundedJSON(t *testing.T) {
	capabilities := mustCapabilities(t, ProfilePortable, []Capability{CapabilityToolExecute, CapabilityWorkspaceRead})
	for name, response := range map[string]struct {
		frame []byte
		class ErrorClass
	}{
		"valid":     {frame: []byte(`{}`)},
		"malformed": {frame: []byte(`{`), class: ErrorConflict},
		"oversized": {frame: []byte("\"" + strings.Repeat("x", MaxRecordBytes) + "\""), class: ErrorConflict},
	} {
		t.Run(name, func(t *testing.T) {
			transport := &frameTransportStub{response: response.frame}
			endpoint, err := NewFramedEndpoint(FramedEndpointConfig{
				Transport:      transport,
				Codec:          frameCodecStub{capabilities: capabilities, encode: []byte(`{}`)},
				BackendBinding: testBackendIdentityBinding(),
			})
			if err != nil {
				t.Fatal(err)
			}
			client := mustClient(t, endpoint)
			_, err = client.Discover(context.Background())
			if name == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				if got := string(transport.request); got != "{}" {
					t.Fatalf("request = %q", got)
				}
				return
			}
			requireClass(t, err, response.class)
		})
	}
}

func TestFramedEndpointMapsTransportFailureToUnavailable(t *testing.T) {
	capabilities := mustCapabilities(t, ProfilePortable, []Capability{CapabilityToolExecute, CapabilityWorkspaceRead})
	endpoint, err := NewFramedEndpoint(FramedEndpointConfig{
		Transport:      &frameTransportStub{err: context.DeadlineExceeded},
		Codec:          frameCodecStub{capabilities: capabilities, encode: []byte(`{}`)},
		BackendBinding: testBackendIdentityBinding(),
	})
	if err != nil {
		t.Fatal(err)
	}
	client := mustClient(t, endpoint)
	_, err = client.Discover(context.Background())
	requireClass(t, err, ErrorUnavailable)
}

func TestFramedEndpointRejectsZeroCompiledPlanBeforeTransport(t *testing.T) {
	transport := &frameTransportStub{response: []byte(`{}`)}
	endpoint, err := NewFramedEndpoint(FramedEndpointConfig{
		Transport: transport,
		Codec:     frameCodecStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := endpoint.Admit(
		context.Background(),
		CompiledContainmentPlan{},
	); err == nil {
		t.Fatal("zero compiled plan reached framed admission")
	} else {
		var failure framingError
		if !errors.As(err, &failure) || failure.class != ErrorInvalid {
			t.Fatalf("zero compiled plan error = %v, want invalid", err)
		}
	}
	if transport.request != nil {
		t.Fatal("zero compiled plan reached transport")
	}
}

func TestFramedEndpointRejectsInvalidOutboundFrame(t *testing.T) {
	capabilities := mustCapabilities(t, ProfilePortable, []Capability{CapabilityToolExecute, CapabilityWorkspaceRead})
	endpoint, err := NewFramedEndpoint(FramedEndpointConfig{
		Transport:      &frameTransportStub{response: []byte(`{}`)},
		Codec:          frameCodecStub{capabilities: capabilities, encode: []byte(`not-json`)},
		BackendBinding: testBackendIdentityBinding(),
	})
	if err != nil {
		t.Fatal(err)
	}
	client := mustClient(t, endpoint)
	_, err = client.Discover(context.Background())
	requireClass(t, err, ErrorInvalid)
}
