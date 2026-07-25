package planescapeprovider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const releasedProviderV1VectorsSHA256 = "75eb632d5178bbb5454dd93ee9f476b61173002f7bddca8d432c639cdf67faf3"

type releasedProviderV1Vectors struct {
	Schema          string `json:"schema"`
	ProtocolVersion string `json:"protocol_version"`
	GeneratedBy     string `json:"generated_by"`
	CapabilitySet   struct {
		CanonicalPreimageBase64URL string `json:"canonical_preimage_base64url"`
		CanonicalHash              string `json:"canonical_hash"`
	} `json:"capability_set"`
	Records []releasedProviderV1Record `json:"records"`
}

type releasedProviderV1Record struct {
	Kind                       string `json:"kind"`
	Schema                     string `json:"schema"`
	WireJSON                   string `json:"wire_json"`
	CanonicalPreimageBase64URL string `json:"canonical_preimage_base64url"`
	CanonicalHash              string `json:"canonical_hash"`
}

func TestProviderV1CodecMatchesReleasedVectors(t *testing.T) {
	vectors := loadReleasedProviderV1Vectors(t)
	codec := ProviderV1FrameCodec{}
	expectedKinds := []string{
		providerV1KindDiscovery,
		providerV1KindCapabilities,
		providerV1KindRequirement,
		providerV1KindAdmission,
		providerV1KindOperation,
		providerV1KindOperationResult,
		providerV1KindQuiescence,
		providerV1KindFreeze,
		providerV1KindFreezeAck,
		providerV1KindCloseout,
		providerV1KindCancellation,
		providerV1KindCancellationAck,
		providerV1KindError,
	}
	if len(vectors.Records) != len(expectedKinds) {
		t.Fatalf("released record count = %d, want %d", len(vectors.Records), len(expectedKinds))
	}

	for index, vector := range vectors.Records {
		t.Run(vector.Kind, func(t *testing.T) {
			if vector.Kind != expectedKinds[index] {
				t.Fatalf("released record kind[%d] = %q, want %q", index, vector.Kind, expectedKinds[index])
			}
			record, err := decodeProviderV1Record([]byte(vector.WireJSON))
			if err != nil {
				t.Fatal(err)
			}
			if record.kind != vector.Kind {
				t.Fatalf("decoded kind = %q, want %q", record.kind, vector.Kind)
			}
			preimage, err := base64.RawURLEncoding.DecodeString(vector.CanonicalPreimageBase64URL)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(record.canonicalPreimage, preimage) {
				t.Fatalf("canonical preimage does not match released vector")
			}
			if got := providerV1CanonicalHash(record.canonicalPreimage); got != vector.CanonicalHash {
				t.Fatalf("canonical hash = %q, want %q", got, vector.CanonicalHash)
			}

			if encoded, ok := encodeReleasedProviderV1Record(t, codec, record); ok &&
				!bytes.Equal(encoded, []byte(vector.WireJSON)) {
				t.Fatalf("encoded wire JSON does not match released vector\n got: %s\nwant: %s", encoded, vector.WireJSON)
			}
			decodeReleasedProviderV1Response(t, codec, vector, record)
		})
	}

	capabilitiesVector := releasedProviderV1RecordByKind(t, vectors, providerV1KindCapabilities)
	var capabilitiesDTO providerV1CapabilitiesDTO
	if err := json.Unmarshal([]byte(capabilitiesVector.WireJSON), &capabilitiesDTO); err != nil {
		t.Fatal(err)
	}
	capabilityPreimage, err := capabilitiesDTO.capabilitySetPreimage()
	if err != nil {
		t.Fatal(err)
	}
	releasedCapabilityPreimage, err := base64.RawURLEncoding.DecodeString(
		vectors.CapabilitySet.CanonicalPreimageBase64URL,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(capabilityPreimage, releasedCapabilityPreimage) {
		t.Fatal("capability-set canonical preimage does not match released vector")
	}
	if got := providerV1CanonicalHash(capabilityPreimage); got != vectors.CapabilitySet.CanonicalHash {
		t.Fatalf("capability-set hash = %q, want %q", got, vectors.CapabilitySet.CanonicalHash)
	}
}

func TestProviderV1CodecIntegratesWithFramedEndpoint(t *testing.T) {
	vectors := loadReleasedProviderV1Vectors(t)
	discovery := releasedProviderV1RecordByKind(t, vectors, providerV1KindDiscovery)
	capabilities := releasedProviderV1RecordByKind(t, vectors, providerV1KindCapabilities)
	transport := &frameTransportStub{response: []byte(capabilities.WireJSON)}
	endpoint, err := NewFramedEndpoint(FramedEndpointConfig{
		Transport: transport,
		Codec:     ProviderV1FrameCodec{},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := mustClient(t, endpoint)
	if _, err := client.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := string(transport.request); got != discovery.WireJSON {
		t.Fatalf("discovery request = %s, want released wire %s", got, discovery.WireJSON)
	}
}

func TestProviderV1CodecRejectsReleasedVectorMutations(t *testing.T) {
	vectors := loadReleasedProviderV1Vectors(t)
	capabilities := releasedProviderV1RecordByKind(t, vectors, providerV1KindCapabilities)
	result := releasedProviderV1RecordByKind(t, vectors, providerV1KindOperationResult)

	mutations := map[string][]byte{
		"canonical hash": mutateReleasedProviderV1Record(t, result, func(fields map[string]json.RawMessage) {
			fields["canonical_hash"] = releasedProviderV1RawJSON(t, "sha256:"+strings.Repeat("0", 64))
		}),
		"unknown field": mutateReleasedProviderV1Record(t, capabilities, func(fields map[string]json.RawMessage) {
			fields["secret_diagnostic"] = releasedProviderV1RawJSON(t, "must-not-leak")
		}),
		"unknown version": mutateReleasedProviderV1Record(t, capabilities, func(fields map[string]json.RawMessage) {
			fields["protocol_version"] = releasedProviderV1RawJSON(t, "v2")
		}),
		"unknown schema version": mutateReleasedProviderV1Record(t, result, func(fields map[string]json.RawMessage) {
			fields["schema"] = releasedProviderV1RawJSON(t, "planescape.provider.operation_result.v2")
		}),
		"unknown enum": mutateReleasedProviderV1Record(t, result, func(fields map[string]json.RawMessage) {
			fields["result_kind"] = releasedProviderV1RawJSON(t, "invented")
		}),
		"unsorted capabilities": mutateReleasedProviderV1Record(t, capabilities, func(fields map[string]json.RawMessage) {
			var values []string
			if err := json.Unmarshal(fields["capabilities"], &values); err != nil {
				t.Fatal(err)
			}
			values[0], values[1] = values[1], values[0]
			fields["capabilities"] = releasedProviderV1RawJSON(t, values)
		}),
		"capability-set mismatch": mutateReleasedProviderV1Record(t, capabilities, func(fields map[string]json.RawMessage) {
			var values []string
			if err := json.Unmarshal(fields["capabilities"], &values); err != nil {
				t.Fatal(err)
			}
			fields["capabilities"] = releasedProviderV1RawJSON(t, values[:len(values)-1])
		}),
		"identifier bound": mutateReleasedProviderV1Record(t, capabilities, func(fields map[string]json.RawMessage) {
			fields["provider_id"] = releasedProviderV1RawJSON(t, strings.Repeat("x", maxIdentifierBytes+1))
		}),
		"null required array": mutateReleasedProviderV1Record(t, capabilities, func(fields map[string]json.RawMessage) {
			fields["capabilities"] = json.RawMessage("null")
		}),
		"missing required field": mutateReleasedProviderV1Record(t, result, func(fields map[string]json.RawMessage) {
			delete(fields, "evidence_hash")
		}),
		"signed integer": mutateReleasedProviderV1Record(t, capabilities, func(fields map[string]json.RawMessage) {
			fields["provider_epoch"] = json.RawMessage("-1")
		}),
		"fractional integer": mutateReleasedProviderV1Record(t, capabilities, func(fields map[string]json.RawMessage) {
			fields["provider_epoch"] = json.RawMessage("7.0")
		}),
		"duplicate field": duplicateReleasedProviderV1Schema(t, result),
		"oversized record": append(
			[]byte(capabilities.WireJSON),
			bytes.Repeat([]byte(" "), MaxRecordBytes-len(capabilities.WireJSON)+1)...,
		),
	}

	for name, frame := range mutations {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeProviderV1Record(frame); !errors.Is(err, errProviderV1Frame) {
				t.Fatalf("decode error = %v, want stable provider-v1 rejection", err)
			} else if strings.Contains(err.Error(), "must-not-leak") {
				t.Fatalf("decode error leaked provider bytes: %v", err)
			}
		})
	}
}

func TestProviderV1CodecAcceptsInsignificantJSONMemberOrder(t *testing.T) {
	vectors := loadReleasedProviderV1Vectors(t)
	result := releasedProviderV1RecordByKind(t, vectors, providerV1KindOperationResult)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(result.WireJSON), &fields); err != nil {
		t.Fatal(err)
	}
	reordered, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(reordered, []byte(result.WireJSON)) {
		t.Fatal("test did not change JSON member order")
	}
	if _, err := (ProviderV1FrameCodec{}).DecodeOperation(reordered); err != nil {
		t.Fatal(err)
	}
}

func TestProviderV1CodecRejectsMismatchedOutboundHashes(t *testing.T) {
	vectors := loadReleasedProviderV1Vectors(t)
	codec := ProviderV1FrameCodec{}
	badHash, err := ParseFingerprint("sha256:" + strings.Repeat("0", 64))
	if err != nil {
		t.Fatal(err)
	}

	requirementRecord := decodeReleasedProviderV1Record(
		t,
		releasedProviderV1RecordByKind(t, vectors, providerV1KindRequirement),
	)
	requirement := requirementRecord.value.(ExecutionRequirement)
	requirement.canonicalHash = badHash

	operationRecord := decodeReleasedProviderV1Record(
		t,
		releasedProviderV1RecordByKind(t, vectors, providerV1KindOperation),
	)
	operation := operationRecord.value.(AgentOperation)
	operation.canonicalHash = badHash

	freezeRecord := decodeReleasedProviderV1Record(
		t,
		releasedProviderV1RecordByKind(t, vectors, providerV1KindFreeze),
	)
	freeze := freezeRecord.value.(Freeze)
	freeze.canonicalHash = badHash

	cancellationRecord := decodeReleasedProviderV1Record(
		t,
		releasedProviderV1RecordByKind(t, vectors, providerV1KindCancellation),
	)
	cancellation := cancellationRecord.value.(Cancellation)
	cancellation.canonicalHash = badHash

	for name, encode := range map[string]func() ([]byte, error){
		"execution requirement": func() ([]byte, error) {
			return codec.EncodeAdmission(requirement)
		},
		"agent operation": func() ([]byte, error) {
			return codec.EncodeOperation(operation)
		},
		"freeze": func() ([]byte, error) {
			return codec.EncodeFreeze(freeze)
		},
		"cancellation": func() ([]byte, error) {
			return codec.EncodeCancellation(cancellation)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := encode(); !errors.Is(err, errProviderV1Frame) {
				t.Fatalf("encode error = %v, want stable provider-v1 rejection", err)
			}
		})
	}
}

func TestProviderV1CodecDecodesProviderErrorForEveryResponse(t *testing.T) {
	vectors := loadReleasedProviderV1Vectors(t)
	errorVector := releasedProviderV1RecordByKind(t, vectors, providerV1KindError)
	expectedRecord := decodeReleasedProviderV1Record(t, errorVector)
	expected := expectedRecord.value.(*ProviderFailure)
	codec := ProviderV1FrameCodec{}

	decoders := map[string]func([]byte) error{
		"capabilities": func(frame []byte) error {
			_, err := codec.DecodeCapabilities(frame)
			return err
		},
		"admission": func(frame []byte) error {
			_, err := codec.DecodeAdmission(frame)
			return err
		},
		"operation": func(frame []byte) error {
			_, err := codec.DecodeOperation(frame)
			return err
		},
		"freeze ack": func(frame []byte) error {
			_, err := codec.DecodeFreezeAck(frame)
			return err
		},
		"cancellation ack": func(frame []byte) error {
			_, err := codec.DecodeCancellationAck(frame)
			return err
		},
	}
	for name, decode := range decoders {
		t.Run(name, func(t *testing.T) {
			err := decode([]byte(errorVector.WireJSON))
			var failure *ProviderFailure
			if !errors.As(err, &failure) {
				t.Fatalf("decode error %T = %v, want ProviderFailure", err, err)
			}
			if failure.Code() != expected.Code() ||
				failure.ProviderID() != expected.ProviderID() ||
				failure.ProviderEpoch() != expected.ProviderEpoch() ||
				failure.RetryFrom() != expected.RetryFrom() ||
				failure.CanonicalHash() != expected.CanonicalHash() {
				t.Fatalf("decoded provider failure does not match released vector")
			}
		})
	}
}

func loadReleasedProviderV1Vectors(t *testing.T) releasedProviderV1Vectors {
	t.Helper()
	root := filepath.Join("testdata", "planescape.provider.v1", "wire")
	data, err := os.ReadFile(filepath.Join(root, "vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if got := hex.EncodeToString(digest[:]); got != releasedProviderV1VectorsSHA256 {
		t.Fatalf("released vectors SHA-256 = %s, want %s", got, releasedProviderV1VectorsSHA256)
	}
	checksum, err := os.ReadFile(filepath.Join(root, "WIRE_VECTORS.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(checksum)), releasedProviderV1VectorsSHA256+"  vectors.json"; got != want {
		t.Fatalf("WIRE_VECTORS.sha256 = %q, want %q", got, want)
	}

	var vectors releasedProviderV1Vectors
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&vectors); err != nil {
		t.Fatal(err)
	}
	if vectors.Schema != "planescape.provider.wire_vectors.v1" ||
		vectors.ProtocolVersion != ProtocolVersionV1 ||
		vectors.GeneratedBy != "planescape-contracts/examples/provider_v1_vectors.rs" {
		t.Fatalf("released vector manifest identity is invalid")
	}
	return vectors
}

func releasedProviderV1RecordByKind(
	t *testing.T,
	vectors releasedProviderV1Vectors,
	kind string,
) releasedProviderV1Record {
	t.Helper()
	for _, record := range vectors.Records {
		if record.Kind == kind {
			return record
		}
	}
	t.Fatalf("released vector %q is missing", kind)
	return releasedProviderV1Record{}
}

func decodeReleasedProviderV1Record(
	t *testing.T,
	vector releasedProviderV1Record,
) decodedProviderV1Record {
	t.Helper()
	record, err := decodeProviderV1Record([]byte(vector.WireJSON))
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func encodeReleasedProviderV1Record(
	t *testing.T,
	codec ProviderV1FrameCodec,
	record decodedProviderV1Record,
) ([]byte, bool) {
	t.Helper()
	var (
		frame []byte
		err   error
	)
	switch record.kind {
	case providerV1KindDiscovery:
		frame, err = codec.EncodeDiscovery()
	case providerV1KindRequirement:
		frame, err = codec.EncodeAdmission(record.value.(ExecutionRequirement))
	case providerV1KindOperation:
		frame, err = codec.EncodeOperation(record.value.(AgentOperation))
	case providerV1KindFreeze:
		frame, err = codec.EncodeFreeze(record.value.(Freeze))
	case providerV1KindCancellation:
		frame, err = codec.EncodeCancellation(record.value.(Cancellation))
	default:
		return nil, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return frame, true
}

func decodeReleasedProviderV1Response(
	t *testing.T,
	codec ProviderV1FrameCodec,
	vector releasedProviderV1Record,
	record decodedProviderV1Record,
) {
	t.Helper()
	var err error
	switch record.kind {
	case providerV1KindCapabilities:
		_, err = codec.DecodeCapabilities([]byte(vector.WireJSON))
	case providerV1KindAdmission:
		_, err = codec.DecodeAdmission([]byte(vector.WireJSON))
	case providerV1KindOperationResult, providerV1KindQuiescence, providerV1KindCloseout:
		_, err = codec.DecodeOperation([]byte(vector.WireJSON))
	case providerV1KindFreezeAck:
		_, err = codec.DecodeFreezeAck([]byte(vector.WireJSON))
	case providerV1KindCancellationAck:
		_, err = codec.DecodeCancellationAck([]byte(vector.WireJSON))
	case providerV1KindError:
		_, err = codec.DecodeCapabilities([]byte(vector.WireJSON))
		var failure *ProviderFailure
		if !errors.As(err, &failure) {
			t.Fatalf("provider error vector decoded as %T, want ProviderFailure", err)
		}
		return
	default:
		return
	}
	if err != nil {
		t.Fatal(err)
	}
}

func mutateReleasedProviderV1Record(
	t *testing.T,
	vector releasedProviderV1Record,
	mutate func(map[string]json.RawMessage),
) []byte {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(vector.WireJSON), &fields); err != nil {
		t.Fatal(err)
	}
	mutate(fields)
	frame, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func releasedProviderV1RawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func duplicateReleasedProviderV1Schema(
	t *testing.T,
	vector releasedProviderV1Record,
) []byte {
	t.Helper()
	encodedSchema, err := json.Marshal(vector.Schema)
	if err != nil {
		t.Fatal(err)
	}
	prefix := `{"schema":` + string(encodedSchema)
	if !strings.HasPrefix(vector.WireJSON, prefix) {
		t.Fatalf("released vector %q does not begin with schema", vector.Kind)
	}
	return []byte(prefix + `,"schema":` + string(encodedSchema) + vector.WireJSON[len(prefix):])
}
