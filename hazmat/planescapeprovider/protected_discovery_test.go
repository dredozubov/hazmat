package planescapeprovider

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const protectedBrokerFixtureSHA256 = "f7dbbf59e0740a7239b37f186963a2f1d40b46203fe6e029714e9e342749442e"

type protectedBrokerDiscoveryFixtureV1 struct {
	Schema string `json:"schema"`
	Limits struct {
		HandshakeFrameBytes int `json:"handshake_frame_bytes"`
		RPCFrameBytes       int `json:"rpc_frame_bytes"`
	} `json:"limits"`
	PublicKeys struct {
		BrokerEd25519Base64URL string `json:"broker_ed25519_b64"`
		ClientEd25519Base64URL string `json:"client_ed25519_b64"`
	} `json:"public_keys"`
	BackendIdentity struct {
		BackendInstanceSHA256      string `json:"backend_instance_sha256"`
		ExecutableSHA256           string `json:"executable_sha256"`
		ExecutionEnvironmentSHA256 string `json:"execution_environment_sha256"`
		ProfileSHA256              string `json:"profile_sha256"`
		Epoch                      uint64 `json:"epoch"`
		IdentitySHA256             string `json:"identity_sha256"`
	} `json:"backend_identity"`
	SharedHandshakePublic struct {
		ClientAuthoritySHA256  string `json:"client_authority_sha256"`
		TransportSessionSHA256 string `json:"transport_session_sha256"`
		ClientHelloJSON        string `json:"client_hello_json"`
		ServerChallengeJSON    string `json:"server_challenge_json"`
		ClientFinishJSON       string `json:"client_finish_json"`
		ServerAcceptedJSON     string `json:"server_accepted_json"`
	} `json:"shared_handshake_public"`
	Handshake struct {
		QuiescenceTransportSessionSHA256 string `json:"quiescence_transport_session_sha256"`
		ReconnectTransportSessionSHA256  string `json:"reconnect_transport_session_sha256"`
	} `json:"handshake"`
	ProviderDiscoveryRPC struct {
		Discovery    protectedBrokerDiscoveryRecordFixtureV1 `json:"discovery"`
		Capabilities protectedBrokerDiscoveryRecordFixtureV1 `json:"capabilities"`
		Request      protectedBrokerDiscoveryRPCFixtureV1    `json:"request"`
		Response     protectedBrokerDiscoveryRPCFixtureV1    `json:"response"`
	} `json:"provider_discovery_rpc"`
	ProviderAdmissionRPC struct {
		CompiledPlan protectedBrokerDiscoveryRecordFixtureV1 `json:"compiled_plan"`
		Request      protectedBrokerDiscoveryRPCFixtureV1    `json:"request"`
		Response     protectedBrokerDiscoveryRPCFixtureV1    `json:"response"`
	} `json:"provider_admission_rpc"`
	ProviderQuiescenceRPC protectedBrokerProviderQuiescenceFixtureV1 `json:"provider_quiescence_rpc"`
	ProviderToolRPC       protectedBrokerProviderToolFixtureV1       `json:"provider_tool_rpc"`
}

type protectedBrokerProviderQuiescenceFixtureV1 struct {
	Operation  protectedBrokerDiscoveryRecordFixtureV1 `json:"operation"`
	Quiescence protectedBrokerDiscoveryRecordFixtureV1 `json:"quiescence"`
	Request    struct {
		Wire                 protectedBrokerDiscoveryRPCWireFixtureV1   `json:"wire"`
		OperationPayloadHash protectedBrokerDiscoveryHashFixtureV1      `json:"operation_payload_hash"`
		Hash                 protectedBrokerDiscoveryHashFixtureV1      `json:"hash"`
		Signature            protectedBrokerDiscoverySignatureFixtureV1 `json:"signature"`
	} `json:"request"`
	Response protectedBrokerDiscoveryRPCFixtureV1 `json:"response"`
}

type protectedBrokerProviderToolFixtureV1 struct {
	Operation        protectedBrokerDiscoveryRecordFixtureV1 `json:"operation"`
	NormalizedRecord struct {
		BytesBase64URL string `json:"bytes_b64"`
	} `json:"normalized_record"`
	Result  protectedBrokerDiscoveryRecordFixtureV1 `json:"result"`
	Request struct {
		Wire                  protectedBrokerDiscoveryRPCWireFixtureV1   `json:"wire"`
		OperationPayloadHash  protectedBrokerDiscoveryHashFixtureV1      `json:"operation_payload_hash"`
		NormalizedPayloadHash protectedBrokerDiscoveryHashFixtureV1      `json:"normalized_payload_hash"`
		Hash                  protectedBrokerDiscoveryHashFixtureV1      `json:"hash"`
		Signature             protectedBrokerDiscoverySignatureFixtureV1 `json:"signature"`
	} `json:"request"`
	Response protectedBrokerDiscoveryRPCFixtureV1 `json:"response"`
}

type protectedBrokerDiscoveryRecordFixtureV1 struct {
	CanonicalJSON            string `json:"canonical_json"`
	CanonicalPreimageBase64  string `json:"canonical_preimage_b64"`
	CanonicalSHA256          string `json:"canonical_sha256"`
	CapabilitySetPreimageB64 string `json:"capability_set_preimage_b64"`
	CapabilitySetSHA256      string `json:"capability_set_sha256"`
}

type protectedBrokerDiscoveryRPCFixtureV1 struct {
	Wire        protectedBrokerDiscoveryRPCWireFixtureV1   `json:"wire"`
	PayloadHash protectedBrokerDiscoveryHashFixtureV1      `json:"payload_hash"`
	Hash        protectedBrokerDiscoveryHashFixtureV1      `json:"hash"`
	Signature   protectedBrokerDiscoverySignatureFixtureV1 `json:"signature"`
}

type protectedBrokerDiscoveryRPCWireFixtureV1 struct {
	CanonicalJSON  string `json:"canonical_json"`
	JSONByteCount  int    `json:"json_byte_count"`
	FramedBytesB64 string `json:"framed_bytes_b64"`
}

type protectedBrokerDiscoveryHashFixtureV1 struct {
	DomainBase64   string `json:"domain_b64"`
	PreimageBase64 string `json:"preimage_b64"`
	SHA256         string `json:"sha256"`
}

type protectedBrokerDiscoverySignatureFixtureV1 struct {
	DomainBase64   string `json:"domain_b64"`
	PreimageBase64 string `json:"preimage_b64"`
	Signature      string `json:"signature"`
}

func TestProtectedBrokerDiscoveryMatchesPublishedFixture(t *testing.T) {
	fixture := loadProtectedBrokerDiscoveryFixture(t)
	interop := loadProtectedBrokerInteropFixture(t)
	brokerKey, clientKey, _, client, clientNonce := protectedBrokerFixtureClient(t, interop)
	if got := base64.RawURLEncoding.EncodeToString(brokerKey.Public().(ed25519.PublicKey)); got != fixture.PublicKeys.BrokerEd25519Base64URL {
		t.Fatalf("broker public key = %q, want published fixture", got)
	}
	if got := base64.RawURLEncoding.EncodeToString(clientKey.Public().(ed25519.PublicKey)); got != fixture.PublicKeys.ClientEd25519Base64URL {
		t.Fatalf("client public key = %q, want published fixture", got)
	}

	session, err := runProtectedBrokerStaticHandshake(
		t,
		client,
		clientNonce,
		fixture.SharedHandshakePublic.ClientHelloJSON,
		fixture.SharedHandshakePublic.ServerChallengeJSON,
		fixture.SharedHandshakePublic.ClientFinishJSON,
		fixture.SharedHandshakePublic.ServerAcceptedJSON,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := session.ClientAuthoritySHA256().String(); got != fixture.SharedHandshakePublic.ClientAuthoritySHA256 {
		t.Fatalf("client authority = %q, want fixture", got)
	}
	if got := session.TransportSessionSHA256().String(); got != fixture.SharedHandshakePublic.TransportSessionSHA256 {
		t.Fatalf("transport session = %q, want fixture", got)
	}

	discoveryJSON := []byte(fixture.ProviderDiscoveryRPC.Discovery.CanonicalJSON)
	request, err := newProtectedBrokerProviderDiscoveryRequestV1(client, session, discoveryJSON)
	if err != nil {
		t.Fatal(err)
	}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(requestJSON); got != fixture.ProviderDiscoveryRPC.Request.Wire.CanonicalJSON {
		t.Fatalf("request wire differs from published fixture\n got: %s\nwant: %s", got, fixture.ProviderDiscoveryRPC.Request.Wire.CanonicalJSON)
	}
	var framedRequest bytes.Buffer
	if err := writeProtectedBrokerRPCJSONFrameV1(&framedRequest, request); err != nil {
		t.Fatal(err)
	}
	requireProtectedBrokerDiscoveryBase64Bytes(
		t,
		framedRequest.Bytes(),
		fixture.ProviderDiscoveryRPC.Request.Wire.FramedBytesB64,
	)
	requireProtectedBrokerDiscoveryHashVector(
		t,
		fixture.ProviderDiscoveryRPC.Request.PayloadHash,
		protectedBrokerProviderDiscoveryRequestPayloadHashDomainV1,
		discoveryJSON,
	)
	requireProtectedBrokerDiscoveryJSONHashVector(
		t,
		fixture.ProviderDiscoveryRPC.Request.Hash,
		protectedBrokerProviderDiscoveryRequestHashDomainV1,
		protectedBrokerProviderDiscoveryRequestSchemaV1,
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.DiscoverySHA256,
		request.RequestPayloadSHA256,
	)
	requireProtectedBrokerDiscoveryJSONSignatureVector(
		t,
		fixture.ProviderDiscoveryRPC.Request.Signature,
		protectedBrokerProviderDiscoveryRequestSignatureDomainV1,
		request.Schema,
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.DiscoverySHA256,
		request.RequestPayloadSHA256,
		request.RequestSHA256,
	)

	responseFrame := decodeProtectedBrokerDiscoveryBase64(
		t,
		fixture.ProviderDiscoveryRPC.Response.Wire.FramedBytesB64,
	)
	var response protectedBrokerProviderDiscoveryResponseWireV1
	if err := readProtectedBrokerRPCJSONFrameV1(bytes.NewReader(responseFrame), &response); err != nil {
		t.Fatal(err)
	}
	capabilitiesJSON, err := response.validate(client, session, request.RequestSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(capabilitiesJSON); got != fixture.ProviderDiscoveryRPC.Capabilities.CanonicalJSON {
		t.Fatalf("returned capabilities differ from published fixture\n got: %s\nwant: %s", got, fixture.ProviderDiscoveryRPC.Capabilities.CanonicalJSON)
	}
	requireProtectedBrokerDiscoveryHashVector(
		t,
		fixture.ProviderDiscoveryRPC.Response.PayloadHash,
		protectedBrokerProviderDiscoveryResponsePayloadHashDomainV1,
		capabilitiesJSON,
	)
	requireProtectedBrokerDiscoveryJSONHashVector(
		t,
		fixture.ProviderDiscoveryRPC.Response.Hash,
		protectedBrokerProviderDiscoveryResponseHashDomainV1,
		response.Schema,
		response.Sequence,
		response.ClientAuthoritySHA256,
		response.TransportSessionSHA256,
		response.BackendIdentitySHA256,
		response.BrokerEpoch,
		response.ProfileSHA256,
		response.RequestSHA256,
		response.CapabilitiesSHA256,
		response.ResponsePayloadSHA256,
	)
	requireProtectedBrokerDiscoveryJSONSignatureVector(
		t,
		fixture.ProviderDiscoveryRPC.Response.Signature,
		protectedBrokerProviderDiscoveryResponseSignatureDomainV1,
		response.Schema,
		response.Sequence,
		response.ClientAuthoritySHA256,
		response.TransportSessionSHA256,
		response.BackendIdentitySHA256,
		response.BrokerEpoch,
		response.ProfileSHA256,
		response.RequestSHA256,
		response.CapabilitiesSHA256,
		response.ResponsePayloadSHA256,
		response.ResponseSHA256,
	)
}

func TestProtectedBrokerDiscoveryIntegratesWithFramedEndpointAndReconnectsFresh(t *testing.T) {
	harness := newProtectedBrokerDiscoveryHarness(t)
	transport := mustProtectedBrokerDiscoveryTransport(t, harness, harness.client)
	endpoint, err := NewFramedEndpoint(FramedEndpointConfig{
		Transport: transport,
		Codec:     ProviderV1FrameCodec{},
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := endpoint.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := endpoint.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalHash() != second.CanonicalHash() {
		t.Fatalf("capabilities changed across reconnect: %s != %s", first.CanonicalHash().String(), second.CanonicalHash().String())
	}

	firstExchange := harness.nextObservation(t)
	secondExchange := harness.nextObservation(t)
	if firstExchange.err != nil {
		t.Fatal(firstExchange.err)
	}
	if secondExchange.err != nil {
		t.Fatal(secondExchange.err)
	}
	if firstExchange.request.Sequence != protectedBrokerProviderDiscoverySequenceV1 ||
		secondExchange.request.Sequence != protectedBrokerProviderDiscoverySequenceV1 {
		t.Fatal("discovery did not restart at sequence 1 on fresh connections")
	}
	if firstExchange.request.ClientAuthoritySHA256 != secondExchange.request.ClientAuthoritySHA256 {
		t.Fatal("stable client authority changed across reconnect")
	}
	if firstExchange.request.TransportSessionSHA256 == secondExchange.request.TransportSessionSHA256 {
		t.Fatal("transport session did not refresh across reconnect")
	}
	harness.requireClosedOnce(t)
}

func TestProtectedBrokerDiscoveryRejectsHostileResponses(t *testing.T) {
	tests := map[string]struct {
		configure func(*protectedBrokerDiscoveryHarness)
		want      ProtectedBrokerTransportErrorClassV1
	}{
		"schema": {
			configure: func(h *protectedBrokerDiscoveryHarness) {
				h.responseMutation = func(response *protectedBrokerProviderDiscoveryResponseWireV1) {
					response.Schema = "execution.protected-broker.provider-discovery-response.v2"
				}
			},
			want: ProtectedBrokerInvalidSchemaV1,
		},
		"sequence": {
			configure: func(h *protectedBrokerDiscoveryHarness) {
				h.responseMutation = func(response *protectedBrokerProviderDiscoveryResponseWireV1) {
					response.Sequence = 3
				}
			},
			want: ProtectedBrokerSequenceGapV1,
		},
		"client authority": {
			configure: func(h *protectedBrokerDiscoveryHarness) {
				h.responseMutation = func(response *protectedBrokerProviderDiscoveryResponseWireV1) {
					response.ClientAuthoritySHA256 = mustProtectedBrokerDiscoveryHash(t, repeatedProtectedBrokerTestHash(0xaa))
				}
			},
			want: ProtectedBrokerClientAuthorityMismatchV1,
		},
		"transport session": {
			configure: func(h *protectedBrokerDiscoveryHarness) {
				h.responseMutation = func(response *protectedBrokerProviderDiscoveryResponseWireV1) {
					response.TransportSessionSHA256 = mustProtectedBrokerDiscoveryHash(t, repeatedProtectedBrokerTestHash(0xbb))
				}
			},
			want: ProtectedBrokerTransportSessionMismatchV1,
		},
		"backend": {
			configure: func(h *protectedBrokerDiscoveryHarness) {
				h.responseMutation = func(response *protectedBrokerProviderDiscoveryResponseWireV1) {
					response.BackendIdentitySHA256 = mustProtectedBrokerDiscoveryHash(t, repeatedProtectedBrokerTestHash(0xcc))
				}
			},
			want: ProtectedBrokerServiceBindingMismatchV1,
		},
		"epoch": {
			configure: func(h *protectedBrokerDiscoveryHarness) {
				h.responseMutation = func(response *protectedBrokerProviderDiscoveryResponseWireV1) {
					response.BrokerEpoch++
				}
			},
			want: ProtectedBrokerServiceBindingMismatchV1,
		},
		"profile": {
			configure: func(h *protectedBrokerDiscoveryHarness) {
				h.responseMutation = func(response *protectedBrokerProviderDiscoveryResponseWireV1) {
					response.ProfileSHA256 = mustProtectedBrokerDiscoveryHash(t, repeatedProtectedBrokerTestHash(0xdd))
				}
			},
			want: ProtectedBrokerServiceBindingMismatchV1,
		},
		"request hash": {
			configure: func(h *protectedBrokerDiscoveryHarness) {
				h.responseMutation = func(response *protectedBrokerProviderDiscoveryResponseWireV1) {
					response.RequestSHA256 = mustProtectedBrokerDiscoveryHash(t, repeatedProtectedBrokerTestHash(0xee))
				}
			},
			want: ProtectedBrokerRequestHashMismatchV1,
		},
		"payload hash": {
			configure: func(h *protectedBrokerDiscoveryHarness) {
				h.responseMutation = func(response *protectedBrokerProviderDiscoveryResponseWireV1) {
					response.ResponsePayloadSHA256 = mustProtectedBrokerDiscoveryHash(t, repeatedProtectedBrokerTestHash(0x11))
				}
			},
			want: ProtectedBrokerResponseHashMismatchV1,
		},
		"response hash": {
			configure: func(h *protectedBrokerDiscoveryHarness) {
				h.responseMutation = func(response *protectedBrokerProviderDiscoveryResponseWireV1) {
					response.ResponseSHA256 = mustProtectedBrokerDiscoveryHash(t, repeatedProtectedBrokerTestHash(0x22))
				}
			},
			want: ProtectedBrokerResponseHashMismatchV1,
		},
		"wrong signer": {
			configure: func(h *protectedBrokerDiscoveryHarness) {
				h.responseSigner = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x33}, ed25519.SeedSize))
			},
			want: ProtectedBrokerInvalidSignatureV1,
		},
		"signature": {
			configure: func(h *protectedBrokerDiscoveryHarness) {
				h.responseMutation = func(response *protectedBrokerProviderDiscoveryResponseWireV1) {
					response.Signature = mutateProtectedBrokerSignature(response.Signature)
				}
			},
			want: ProtectedBrokerInvalidSignatureV1,
		},
		"unknown field": {
			configure: func(h *protectedBrokerDiscoveryHarness) {
				h.rawResponseMutation = func(response []byte) []byte {
					return append(append([]byte(nil), response[:len(response)-1]...), []byte(`,"unexpected":true}`)...)
				}
			},
			want: ProtectedBrokerInvalidFrameV1,
		},
		"malformed": {
			configure: func(h *protectedBrokerDiscoveryHarness) {
				h.rawResponseMutation = func([]byte) []byte {
					return []byte(`{"schema":`)
				}
			},
			want: ProtectedBrokerInvalidFrameV1,
		},
		"oversized": {
			configure: func(h *protectedBrokerDiscoveryHarness) {
				h.oversizedResponse = true
			},
			want: ProtectedBrokerFrameTooLargeV1,
		},
		"capability set": {
			configure: func(h *protectedBrokerDiscoveryHarness) {
				h.capabilitiesJSON = protectedBrokerWrongCapabilitySetJSON(t, h.identity)
			},
			want: ProtectedBrokerServiceBindingMismatchV1,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			harness := newProtectedBrokerDiscoveryHarness(t)
			test.configure(harness)
			transport := mustProtectedBrokerDiscoveryTransport(t, harness, harness.client)
			discovery, err := (ProviderV1FrameCodec{}).EncodeDiscovery()
			if err != nil {
				t.Fatal(err)
			}
			response, err := transport.RoundTrip(context.Background(), discovery)
			if len(response) != 0 {
				t.Fatal("hostile response returned provider bytes")
			}
			requireProtectedBrokerErrorClass(t, err, test.want)
			observation := harness.nextObservation(t)
			if observation.err != nil {
				t.Fatal(observation.err)
			}
			harness.requireClosedOnce(t)
		})
	}
}

func TestProtectedBrokerDiscoveryRejectsLifecycleBeforeDial(t *testing.T) {
	harness := newProtectedBrokerDiscoveryHarness(t)
	called := false
	dialer := ProtectedBrokerDialFuncV1(func(context.Context) (ProtectedBrokerStreamV1, error) {
		called = true
		return harness.DialContext(context.Background())
	})
	transport := mustProtectedBrokerDiscoveryTransport(t, dialer, harness.client)
	vectors := loadReleasedProviderV1Vectors(t)
	requirement := releasedProviderV1RecordByKind(t, vectors, providerV1KindRequirement)

	response, err := transport.RoundTrip(context.Background(), []byte(requirement.WireJSON))
	if len(response) != 0 {
		t.Fatal("unsupported lifecycle request returned provider bytes")
	}
	requireProtectedBrokerErrorClass(t, err, ProtectedBrokerInvalidRequestV1)
	if called {
		t.Fatal("unsupported lifecycle request reached the dial boundary")
	}

	discovery, err := (ProviderV1FrameCodec{}).EncodeDiscovery()
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(discovery, &fields); err != nil {
		t.Fatal(err)
	}
	reordered, err := json.Marshal(map[string]json.RawMessage{
		"canonical_hash":   fields["canonical_hash"],
		"protocol_version": fields["protocol_version"],
		"schema":           fields["schema"],
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.RoundTrip(context.Background(), reordered)
	requireProtectedBrokerErrorClass(t, err, ProtectedBrokerInvalidRequestV1)
	if called {
		t.Fatal("non-exact discovery request reached the dial boundary")
	}
}

func TestProtectedBrokerDiscoveryContextAndIOBehavior(t *testing.T) {
	t.Run("canceled before dial", func(t *testing.T) {
		harness := newProtectedBrokerDiscoveryHarness(t)
		called := false
		dialer := ProtectedBrokerDialFuncV1(func(context.Context) (ProtectedBrokerStreamV1, error) {
			called = true
			return nil, errors.New("must not dial")
		})
		transport := mustProtectedBrokerDiscoveryTransport(t, dialer, harness.client)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := transport.RoundTrip(ctx, mustProtectedBrokerDiscoveryJSON(t))
		requireProtectedBrokerErrorClass(t, err, ProtectedBrokerUnavailableV1)
		if called {
			t.Fatal("canceled request dialed")
		}
	})

	t.Run("dial error is redacted", func(t *testing.T) {
		harness := newProtectedBrokerDiscoveryHarness(t)
		secret := "raw-dial-peer-material"
		dialer := ProtectedBrokerDialFuncV1(func(context.Context) (ProtectedBrokerStreamV1, error) {
			return nil, errors.New(secret)
		})
		transport := mustProtectedBrokerDiscoveryTransport(t, dialer, harness.client)
		_, err := transport.RoundTrip(context.Background(), mustProtectedBrokerDiscoveryJSON(t))
		requireProtectedBrokerErrorClass(t, err, ProtectedBrokerIOV1)
		if strings.Contains(err.Error(), secret) {
			t.Fatal("dial error exposed raw transport diagnostics")
		}
	})

	t.Run("write error closes once and is redacted", func(t *testing.T) {
		harness := newProtectedBrokerDiscoveryHarness(t)
		stream := &protectedBrokerFailingStream{writeErr: errors.New("raw-write-peer-material")}
		transport := mustProtectedBrokerDiscoveryTransport(
			t,
			ProtectedBrokerDialFuncV1(func(context.Context) (ProtectedBrokerStreamV1, error) {
				return stream, nil
			}),
			harness.client,
		)
		_, err := transport.RoundTrip(context.Background(), mustProtectedBrokerDiscoveryJSON(t))
		requireProtectedBrokerErrorClass(t, err, ProtectedBrokerIOV1)
		if strings.Contains(err.Error(), "raw-write") {
			t.Fatal("write error exposed raw transport diagnostics")
		}
		if stream.closed() != 1 {
			t.Fatalf("close count = %d, want 1", stream.closed())
		}
	})

	t.Run("read error closes once and is redacted", func(t *testing.T) {
		harness := newProtectedBrokerDiscoveryHarness(t)
		stream := &protectedBrokerFailingStream{readErr: errors.New("raw-read-peer-material")}
		transport := mustProtectedBrokerDiscoveryTransport(
			t,
			ProtectedBrokerDialFuncV1(func(context.Context) (ProtectedBrokerStreamV1, error) {
				return stream, nil
			}),
			harness.client,
		)
		_, err := transport.RoundTrip(context.Background(), mustProtectedBrokerDiscoveryJSON(t))
		requireProtectedBrokerErrorClass(t, err, ProtectedBrokerIOV1)
		if strings.Contains(err.Error(), "raw-read") {
			t.Fatal("read error exposed raw transport diagnostics")
		}
		if stream.closed() != 1 {
			t.Fatalf("close count = %d, want 1", stream.closed())
		}
	})

	t.Run("cancellation interrupts read", func(t *testing.T) {
		harness := newProtectedBrokerDiscoveryHarness(t)
		harness.blockAfterHello = true
		transport := mustProtectedBrokerDiscoveryTransport(t, harness, harness.client)
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() {
			_, err := transport.RoundTrip(ctx, mustProtectedBrokerDiscoveryJSON(t))
			result <- err
		}()
		harness.waitUntilBlocked(t)
		cancel()
		select {
		case err := <-result:
			requireProtectedBrokerErrorClass(t, err, ProtectedBrokerUnavailableV1)
		case <-time.After(2 * time.Second):
			t.Fatal("cancellation did not interrupt protected broker read")
		}
		_ = harness.nextObservation(t)
		harness.requireClosedOnce(t)
	})

	t.Run("deadline is applied and interrupts read", func(t *testing.T) {
		harness := newProtectedBrokerDiscoveryHarness(t)
		harness.blockAfterHello = true
		transport := mustProtectedBrokerDiscoveryTransport(t, harness, harness.client)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		wantDeadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("test context has no deadline")
		}
		_, err := transport.RoundTrip(ctx, mustProtectedBrokerDiscoveryJSON(t))
		requireProtectedBrokerErrorClass(t, err, ProtectedBrokerUnavailableV1)
		_ = harness.nextObservation(t)
		streams := harness.snapshotStreams()
		if len(streams) != 1 {
			t.Fatalf("stream count = %d, want 1", len(streams))
		}
		deadlines := streams[0].snapshotDeadlines()
		if len(deadlines) == 0 || !deadlines[0].Equal(wantDeadline) {
			t.Fatalf("initial stream deadline = %v, want %v", deadlines, wantDeadline)
		}
		harness.requireClosedOnce(t)
	})

	t.Run("close failure withholds validated response", func(t *testing.T) {
		harness := newProtectedBrokerDiscoveryHarness(t)
		harness.clientCloseErr = errors.New("raw-close-peer-material")
		transport := mustProtectedBrokerDiscoveryTransport(t, harness, harness.client)
		response, err := transport.RoundTrip(context.Background(), mustProtectedBrokerDiscoveryJSON(t))
		if len(response) != 0 {
			t.Fatal("close failure returned capabilities")
		}
		requireProtectedBrokerErrorClass(t, err, ProtectedBrokerIOV1)
		if strings.Contains(err.Error(), "raw-close") {
			t.Fatal("close error exposed raw transport diagnostics")
		}
		observation := harness.nextObservation(t)
		if observation.err != nil {
			t.Fatal(observation.err)
		}
		harness.requireClosedOnce(t)
	})
}

type protectedBrokerDiscoveryHarness struct {
	client           *ProtectedBrokerClientV1
	brokerKey        ed25519.PrivateKey
	clientPublicKey  ed25519.PublicKey
	identity         ProtectedBrokerBackendIdentityV1
	capabilitiesJSON []byte

	mu                  sync.Mutex
	nextServerNonce     byte
	streams             []*protectedBrokerTrackingStream
	responseMutation    func(*protectedBrokerProviderDiscoveryResponseWireV1)
	rawResponseMutation func([]byte) []byte
	responseSigner      ed25519.PrivateKey
	oversizedResponse   bool
	blockAfterHello     bool
	clientCloseErr      error
	observations        chan protectedBrokerDiscoveryObservation
	blocked             chan struct{}
}

type protectedBrokerDiscoveryObservation struct {
	request protectedBrokerProviderDiscoveryRequestWireV1
	err     error
}

func newProtectedBrokerDiscoveryHarness(t *testing.T) *protectedBrokerDiscoveryHarness {
	t.Helper()
	interop := loadProtectedBrokerInteropFixture(t)
	brokerKey, clientKey, identity, client, _ := protectedBrokerFixtureClient(t, interop)
	fixture := loadProtectedBrokerDiscoveryFixture(t)
	return &protectedBrokerDiscoveryHarness{
		client:           client,
		brokerKey:        brokerKey,
		clientPublicKey:  clientKey.Public().(ed25519.PublicKey),
		identity:         identity,
		capabilitiesJSON: []byte(fixture.ProviderDiscoveryRPC.Capabilities.CanonicalJSON),
		nextServerNonce:  0x40,
		observations:     make(chan protectedBrokerDiscoveryObservation, 4),
		blocked:          make(chan struct{}),
	}
}

func (h *protectedBrokerDiscoveryHarness) DialContext(
	context.Context,
) (ProtectedBrokerStreamV1, error) {
	client, server := net.Pipe()
	h.mu.Lock()
	h.nextServerNonce++
	serverNonce := h.nextServerNonce
	stream := &protectedBrokerTrackingStream{
		Conn:     client,
		closeErr: h.clientCloseErr,
	}
	h.streams = append(h.streams, stream)
	h.mu.Unlock()
	go h.serve(server, serverNonce)
	return stream, nil
}

func (h *protectedBrokerDiscoveryHarness) serve(
	stream net.Conn,
	serverNonce byte,
) {
	observation := protectedBrokerDiscoveryObservation{}
	defer func() {
		_ = stream.Close()
		h.observations <- observation
	}()
	_ = stream.SetDeadline(time.Now().Add(5 * time.Second))
	handshakeCodec := ProtectedBrokerFrameCodecV1{}

	var hello protectedBrokerClientHelloWireV1
	if err := handshakeCodec.ReadJSONFrame(stream, &hello); err != nil {
		observation.err = err
		return
	}
	if err := validateProtectedBrokerClientHelloForTest(hello, h.client.clientKeySHA256); err != nil {
		observation.err = err
		return
	}
	if h.blockAfterHello {
		close(h.blocked)
		var one [1]byte
		_, observation.err = stream.Read(one[:])
		return
	}
	clientAuthority, err := protectedBrokerClientAuthorityHashV1(
		h.client.clientKeySHA256,
		h.identity,
	)
	if err != nil {
		observation.err = err
		return
	}
	challenge, err := newProtectedBrokerServerChallengeForTest(
		hello,
		repeatedProtectedBrokerNonce(serverNonce),
		h.identity,
		clientAuthority,
		h.brokerKey,
	)
	if err != nil {
		observation.err = err
		return
	}
	if err := handshakeCodec.WriteJSONFrame(stream, challenge); err != nil {
		observation.err = err
		return
	}

	var finish protectedBrokerClientFinishWireV1
	if err := handshakeCodec.ReadJSONFrame(stream, &finish); err != nil {
		observation.err = err
		return
	}
	transportSession, err := protectedBrokerTransportSessionHashV1(
		hello,
		challenge,
		clientAuthority,
		h.client.clientKeySHA256,
	)
	if err != nil {
		observation.err = err
		return
	}
	if err := validateProtectedBrokerClientFinishForTest(
		finish,
		challenge,
		h.client.clientKeySHA256,
		clientAuthority,
		transportSession,
		h.clientPublicKey,
	); err != nil {
		observation.err = err
		return
	}
	accepted, err := newProtectedBrokerServerAcceptedForTest(
		finish,
		clientAuthority,
		transportSession,
		h.brokerKey,
	)
	if err != nil {
		observation.err = err
		return
	}
	if err := handshakeCodec.WriteJSONFrame(stream, accepted); err != nil {
		observation.err = err
		return
	}

	var request protectedBrokerProviderDiscoveryRequestWireV1
	if err := readProtectedBrokerRPCJSONFrameV1(stream, &request); err != nil {
		observation.err = err
		return
	}
	observation.request = request
	if err := validateProtectedBrokerDiscoveryRequestForTest(
		request,
		clientAuthority,
		transportSession,
		h.identity,
		h.clientPublicKey,
	); err != nil {
		observation.err = err
		return
	}
	signer := h.responseSigner
	if signer == nil {
		signer = h.brokerKey
	}
	response, err := newProtectedBrokerDiscoveryResponseForTest(
		request,
		h.capabilitiesJSON,
		signer,
	)
	if err != nil {
		observation.err = err
		return
	}
	if h.responseMutation != nil {
		h.responseMutation(&response)
	}
	if h.oversizedResponse {
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], MaxProtectedBrokerRPCFrameBytesV1+1)
		observation.err = writeProtectedBrokerBytes(stream, header[:])
		return
	}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		observation.err = err
		return
	}
	if h.rawResponseMutation != nil {
		responseJSON = h.rawResponseMutation(responseJSON)
	}
	observation.err = writeProtectedBrokerRPCRawFrameForTest(stream, responseJSON)
}

func (h *protectedBrokerDiscoveryHarness) nextObservation(
	t *testing.T,
) protectedBrokerDiscoveryObservation {
	t.Helper()
	select {
	case observation := <-h.observations:
		return observation
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for protected broker test server")
		return protectedBrokerDiscoveryObservation{}
	}
}

func (h *protectedBrokerDiscoveryHarness) waitUntilBlocked(t *testing.T) {
	t.Helper()
	select {
	case <-h.blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("protected broker test server did not reach blocking read")
	}
}

func (h *protectedBrokerDiscoveryHarness) snapshotStreams() []*protectedBrokerTrackingStream {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*protectedBrokerTrackingStream(nil), h.streams...)
}

func (h *protectedBrokerDiscoveryHarness) requireClosedOnce(t *testing.T) {
	t.Helper()
	streams := h.snapshotStreams()
	if len(streams) == 0 {
		t.Fatal("protected broker was never dialed")
	}
	for index, stream := range streams {
		if got := stream.closed(); got != 1 {
			t.Fatalf("stream[%d] close count = %d, want 1", index, got)
		}
	}
}

type protectedBrokerTrackingStream struct {
	net.Conn
	mu         sync.Mutex
	closeCount int
	closeErr   error
	deadlines  []time.Time
}

func (s *protectedBrokerTrackingStream) Close() error {
	s.mu.Lock()
	s.closeCount++
	closeErr := s.closeErr
	s.mu.Unlock()
	underlyingErr := s.Conn.Close()
	if closeErr != nil {
		return closeErr
	}
	return underlyingErr
}

func (s *protectedBrokerTrackingStream) SetDeadline(deadline time.Time) error {
	s.mu.Lock()
	s.deadlines = append(s.deadlines, deadline)
	s.mu.Unlock()
	return s.Conn.SetDeadline(deadline)
}

func (s *protectedBrokerTrackingStream) closed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeCount
}

func (s *protectedBrokerTrackingStream) snapshotDeadlines() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.deadlines...)
}

type protectedBrokerFailingStream struct {
	mu         sync.Mutex
	writeErr   error
	readErr    error
	closeErr   error
	closeCount int
}

func (s *protectedBrokerFailingStream) Read([]byte) (int, error) {
	return 0, s.readErr
}

func (s *protectedBrokerFailingStream) Write(value []byte) (int, error) {
	if s.writeErr != nil {
		return 0, s.writeErr
	}
	return len(value), nil
}

func (s *protectedBrokerFailingStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeCount++
	return s.closeErr
}

func (*protectedBrokerFailingStream) SetDeadline(time.Time) error {
	return nil
}

func (s *protectedBrokerFailingStream) closed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeCount
}

func validateProtectedBrokerClientHelloForTest(
	hello protectedBrokerClientHelloWireV1,
	expectedClientKey protectedBrokerHashV1,
) error {
	if hello.Schema != protectedBrokerClientHelloSchemaV1 ||
		hello.ClientKeySHA256 != expectedClientKey {
		return errors.New("invalid client hello binding")
	}
	if _, err := parseProtectedBrokerNonceV1(hello.ClientNonce); err != nil {
		return err
	}
	expectedHash, err := hashProtectedBrokerJSONV1(
		protectedBrokerClientHelloHashDomainV1,
		hello.Schema,
		hello.ClientKeySHA256,
		hello.ClientNonce,
	)
	if err != nil {
		return err
	}
	if hello.HelloSHA256 != expectedHash {
		return errors.New("invalid client hello hash")
	}
	return nil
}

func validateProtectedBrokerClientFinishForTest(
	finish protectedBrokerClientFinishWireV1,
	challenge protectedBrokerServerChallengeWireV1,
	clientKeySHA256 protectedBrokerHashV1,
	clientAuthoritySHA256 protectedBrokerHashV1,
	transportSessionSHA256 protectedBrokerHashV1,
	clientKey ed25519.PublicKey,
) error {
	if finish.Schema != protectedBrokerClientFinishSchemaV1 ||
		finish.ChallengeSHA256 != challenge.ChallengeSHA256 ||
		finish.ClientKeySHA256 != clientKeySHA256 ||
		finish.ClientAuthoritySHA256 != clientAuthoritySHA256 ||
		finish.TransportSessionSHA256 != transportSessionSHA256 {
		return errors.New("invalid client finish binding")
	}
	expectedHash, err := hashProtectedBrokerJSONV1(
		protectedBrokerClientFinishHashDomainV1,
		finish.Schema,
		finish.ChallengeSHA256,
		finish.ClientKeySHA256,
		finish.ClientAuthoritySHA256,
		finish.TransportSessionSHA256,
	)
	if err != nil {
		return err
	}
	if finish.FinishSHA256 != expectedHash {
		return errors.New("invalid client finish hash")
	}
	return verifyProtectedBrokerJSONV1(
		protectedBrokerClientFinishSigDomainV1,
		finish.Signature,
		clientKey,
		finish.Schema,
		finish.ChallengeSHA256,
		finish.ClientKeySHA256,
		finish.ClientAuthoritySHA256,
		finish.TransportSessionSHA256,
		finish.FinishSHA256,
	)
}

func validateProtectedBrokerDiscoveryRequestForTest(
	request protectedBrokerProviderDiscoveryRequestWireV1,
	clientAuthoritySHA256 protectedBrokerHashV1,
	transportSessionSHA256 protectedBrokerHashV1,
	identity ProtectedBrokerBackendIdentityV1,
	clientKey ed25519.PublicKey,
) error {
	if request.Schema != protectedBrokerProviderDiscoveryRequestSchemaV1 ||
		request.Sequence != protectedBrokerProviderDiscoverySequenceV1 ||
		request.ClientAuthoritySHA256 != clientAuthoritySHA256 ||
		request.TransportSessionSHA256 != transportSessionSHA256 ||
		request.BackendIdentitySHA256 != identity.identitySHA256 ||
		request.BrokerEpoch != identity.brokerEpoch ||
		request.ProfileSHA256 != identity.profileSHA256 {
		return errors.New("invalid provider discovery request binding")
	}
	discoveryJSON, err := decodeProtectedBrokerProviderRecordV1(request.DiscoveryJSONBase64URL)
	if err != nil {
		return err
	}
	if err := requireExactProviderDiscoveryV1(discoveryJSON); err != nil {
		return err
	}
	record, err := decodeProviderV1Record(discoveryJSON)
	if err != nil {
		return err
	}
	if request.DiscoverySHA256.String() != providerV1CanonicalHash(record.canonicalPreimage) {
		return errors.New("invalid provider discovery canonical hash")
	}
	expectedPayloadHash := hashProtectedBrokerBytesV1(
		protectedBrokerProviderDiscoveryRequestPayloadHashDomainV1,
		discoveryJSON,
	)
	if request.RequestPayloadSHA256 != expectedPayloadHash {
		return errors.New("invalid provider discovery payload hash")
	}
	expectedRequestHash, err := hashProtectedBrokerJSONV1(
		protectedBrokerProviderDiscoveryRequestHashDomainV1,
		request.Schema,
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.DiscoverySHA256,
		request.RequestPayloadSHA256,
	)
	if err != nil {
		return err
	}
	if request.RequestSHA256 != expectedRequestHash {
		return errors.New("invalid provider discovery request hash")
	}
	return verifyProtectedBrokerJSONV1(
		protectedBrokerProviderDiscoveryRequestSignatureDomainV1,
		request.Signature,
		clientKey,
		request.Schema,
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.DiscoverySHA256,
		request.RequestPayloadSHA256,
		request.RequestSHA256,
	)
}

func newProtectedBrokerDiscoveryResponseForTest(
	request protectedBrokerProviderDiscoveryRequestWireV1,
	capabilitiesJSON []byte,
	brokerKey ed25519.PrivateKey,
) (protectedBrokerProviderDiscoveryResponseWireV1, error) {
	capabilities, err := (ProviderV1FrameCodec{}).DecodeCapabilities(capabilitiesJSON)
	if err != nil {
		return protectedBrokerProviderDiscoveryResponseWireV1{}, err
	}
	capabilitiesSHA256, err := parseProtectedBrokerHashV1(
		capabilities.CanonicalHash().String(),
	)
	if err != nil {
		return protectedBrokerProviderDiscoveryResponseWireV1{}, err
	}
	responsePayloadSHA256 := hashProtectedBrokerBytesV1(
		protectedBrokerProviderDiscoveryResponsePayloadHashDomainV1,
		capabilitiesJSON,
	)
	responseSHA256, err := hashProtectedBrokerJSONV1(
		protectedBrokerProviderDiscoveryResponseHashDomainV1,
		protectedBrokerProviderDiscoveryResponseSchemaV1,
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.RequestSHA256,
		capabilitiesSHA256,
		responsePayloadSHA256,
	)
	if err != nil {
		return protectedBrokerProviderDiscoveryResponseWireV1{}, err
	}
	signature, err := signProtectedBrokerJSONV1(
		protectedBrokerProviderDiscoveryResponseSignatureDomainV1,
		brokerKey,
		protectedBrokerProviderDiscoveryResponseSchemaV1,
		request.Sequence,
		request.ClientAuthoritySHA256,
		request.TransportSessionSHA256,
		request.BackendIdentitySHA256,
		request.BrokerEpoch,
		request.ProfileSHA256,
		request.RequestSHA256,
		capabilitiesSHA256,
		responsePayloadSHA256,
		responseSHA256,
	)
	if err != nil {
		return protectedBrokerProviderDiscoveryResponseWireV1{}, err
	}
	return protectedBrokerProviderDiscoveryResponseWireV1{
		Schema:                    protectedBrokerProviderDiscoveryResponseSchemaV1,
		Sequence:                  request.Sequence,
		ClientAuthoritySHA256:     request.ClientAuthoritySHA256,
		TransportSessionSHA256:    request.TransportSessionSHA256,
		BackendIdentitySHA256:     request.BackendIdentitySHA256,
		BrokerEpoch:               request.BrokerEpoch,
		ProfileSHA256:             request.ProfileSHA256,
		RequestSHA256:             request.RequestSHA256,
		CapabilitiesSHA256:        capabilitiesSHA256,
		ResponsePayloadSHA256:     responsePayloadSHA256,
		CapabilitiesJSONBase64URL: base64.RawURLEncoding.EncodeToString(capabilitiesJSON),
		ResponseSHA256:            responseSHA256,
		Signature:                 signature,
	}, nil
}

func protectedBrokerWrongCapabilitySetJSON(
	t *testing.T,
	identity ProtectedBrokerBackendIdentityV1,
) []byte {
	t.Helper()
	dto := providerV1CapabilitiesDTO{
		Schema:          providerV1SchemaCapabilities,
		ProviderID:      identity.identitySHA256.String(),
		ProviderEpoch:   uint64(identity.brokerEpoch),
		ProfileID:       string(ProfileStockLinux),
		ProtocolVersion: ProtocolVersionV1,
		Capabilities: []string{
			string(CapabilityArtifactRead),
			string(CapabilityWorkspaceRead),
			string(CapabilityWorkspaceWrite),
		},
		ResourceDimensions: []string{
			string(ResourceCPUTime),
			string(ResourceMemoryBytes),
			string(ResourceOpenFiles),
			string(ResourceProcessCount),
			string(ResourceWorkspaceBytes),
			string(ResourceWorkspaceEntries),
		},
	}
	capabilityPreimage, err := dto.capabilitySetPreimage()
	if err != nil {
		t.Fatal(err)
	}
	dto.CapabilityHash = providerV1CanonicalHash(capabilityPreimage)
	canonicalPreimage, err := dto.canonicalPreimage()
	if err != nil {
		t.Fatal(err)
	}
	dto.CanonicalHash = providerV1CanonicalHash(canonicalPreimage)
	encoded, err := encodeProviderV1DTO(providerV1KindCapabilities, dto)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func loadProtectedBrokerDiscoveryFixture(
	t *testing.T,
) protectedBrokerDiscoveryFixtureV1 {
	t.Helper()
	encoded, err := os.ReadFile("testdata/protected_broker.v1/protected_broker_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	if got := fmt.Sprintf("%x", digest); got != protectedBrokerFixtureSHA256 {
		t.Fatalf("published fixture sha256 = %s, want %s", got, protectedBrokerFixtureSHA256)
	}
	var fixture protectedBrokerDiscoveryFixtureV1
	if err := json.Unmarshal(encoded, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Schema != "execution.protected-broker.conformance-vectors.v1" {
		t.Fatalf("published fixture schema = %q", fixture.Schema)
	}
	if fixture.Limits.HandshakeFrameBytes != MaxProtectedBrokerFrameBytesV1 {
		t.Fatalf("published handshake frame limit = %d, want %d", fixture.Limits.HandshakeFrameBytes, MaxProtectedBrokerFrameBytesV1)
	}
	if fixture.Limits.RPCFrameBytes != MaxProtectedBrokerRPCFrameBytesV1 {
		t.Fatalf("published RPC frame limit = %d, want %d", fixture.Limits.RPCFrameBytes, MaxProtectedBrokerRPCFrameBytesV1)
	}
	return fixture
}

func mustProtectedBrokerDiscoveryTransport(
	t *testing.T,
	dialer ProtectedBrokerDialerV1,
	client *ProtectedBrokerClientV1,
) *ProtectedBrokerDiscoveryTransportV1 {
	t.Helper()
	transport, err := NewProtectedBrokerDiscoveryTransportV1(
		ProtectedBrokerDiscoveryTransportConfigV1{
			Dialer: dialer,
			Client: client,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return transport
}

func mustProtectedBrokerDiscoveryJSON(t *testing.T) []byte {
	t.Helper()
	discovery, err := (ProviderV1FrameCodec{}).EncodeDiscovery()
	if err != nil {
		t.Fatal(err)
	}
	return discovery
}

func mustProtectedBrokerDiscoveryHash(
	t *testing.T,
	value string,
) protectedBrokerHashV1 {
	t.Helper()
	hash, err := parseProtectedBrokerHashV1(value)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func writeProtectedBrokerRPCRawFrameForTest(
	writer io.Writer,
	payload []byte,
) error {
	if len(payload) == 0 || len(payload) > MaxProtectedBrokerRPCFrameBytesV1 {
		return errors.New("invalid test RPC payload")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if err := writeProtectedBrokerBytes(writer, header[:]); err != nil {
		return err
	}
	return writeProtectedBrokerBytes(writer, payload)
}

func requireProtectedBrokerDiscoveryHashVector(
	t *testing.T,
	vector protectedBrokerDiscoveryHashFixtureV1,
	domain string,
	payload []byte,
) {
	t.Helper()
	requireProtectedBrokerDiscoveryBase64Bytes(t, []byte(domain), vector.DomainBase64)
	preimage := append(append([]byte(nil), domain...), payload...)
	requireProtectedBrokerDiscoveryBase64Bytes(t, preimage, vector.PreimageBase64)
	if got := hashProtectedBrokerBytesV1(domain, payload).String(); got != vector.SHA256 {
		t.Fatalf("published hash = %q, derived %q", vector.SHA256, got)
	}
}

func requireProtectedBrokerDiscoveryJSONHashVector(
	t *testing.T,
	vector protectedBrokerDiscoveryHashFixtureV1,
	domain string,
	values ...any,
) {
	t.Helper()
	requireProtectedBrokerDiscoveryBase64Bytes(t, []byte(domain), vector.DomainBase64)
	preimage, err := protectedBrokerJSONMessageV1(domain, values...)
	if err != nil {
		t.Fatal(err)
	}
	requireProtectedBrokerDiscoveryBase64Bytes(t, preimage, vector.PreimageBase64)
	hash, err := hashProtectedBrokerJSONV1(domain, values...)
	if err != nil {
		t.Fatal(err)
	}
	if hash.String() != vector.SHA256 {
		t.Fatalf("published hash = %q, derived %q", vector.SHA256, hash.String())
	}
}

func requireProtectedBrokerDiscoveryJSONSignatureVector(
	t *testing.T,
	vector protectedBrokerDiscoverySignatureFixtureV1,
	domain string,
	values ...any,
) {
	t.Helper()
	requireProtectedBrokerDiscoveryBase64Bytes(t, []byte(domain), vector.DomainBase64)
	preimage, err := protectedBrokerJSONMessageV1(domain, values...)
	if err != nil {
		t.Fatal(err)
	}
	requireProtectedBrokerDiscoveryBase64Bytes(t, preimage, vector.PreimageBase64)
}

func requireProtectedBrokerDiscoveryBase64Bytes(
	t *testing.T,
	got []byte,
	wantBase64 string,
) {
	t.Helper()
	want := decodeProtectedBrokerDiscoveryBase64(t, wantBase64)
	if !bytes.Equal(got, want) {
		t.Fatal("bytes differ from published base64 fixture")
	}
}

func decodeProtectedBrokerDiscoveryBase64(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}
