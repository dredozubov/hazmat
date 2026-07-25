package planescapeprovider

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall"
	"testing"
)

type protectedFrameFixture struct {
	Schema string                      `json:"schema"`
	Value  string                      `json:"value"`
	Nested protectedFrameNestedFixture `json:"nested"`
}

type protectedFrameNestedFixture struct {
	Count uint64 `json:"count"`
}

func TestProtectedBrokerFrameCodecMatchesLengthPrefixedJSON(t *testing.T) {
	codec := ProtectedBrokerFrameCodecV1{}
	value := protectedFrameFixture{
		Schema: "execution.protected-broker.test.v1",
		Value:  "ok",
		Nested: protectedFrameNestedFixture{Count: 7},
	}
	payload := []byte(`{"schema":"execution.protected-broker.test.v1","value":"ok","nested":{"count":7}}`)
	want := protectedFrame(payload)

	var encoded bytes.Buffer
	if err := codec.WriteJSONFrame(&encoded, value); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded.Bytes(), want) {
		t.Fatalf("encoded frame = %x, want %x", encoded.Bytes(), want)
	}

	var decoded protectedFrameFixture
	if err := codec.ReadJSONFrame(bytes.NewReader(want), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != value {
		t.Fatalf("decoded frame = %+v, want %+v", decoded, value)
	}
}

func TestProtectedBrokerFrameCodecRejectsMalformedInputs(t *testing.T) {
	valid := `{"schema":"execution.protected-broker.test.v1","value":"ok","nested":{"count":7}}`
	cases := map[string]struct {
		frame []byte
		want  ProtectedBrokerTransportErrorClassV1
	}{
		"zero length": {
			frame: []byte{0, 0, 0, 0},
			want:  ProtectedBrokerFrameTooLargeV1,
		},
		"oversized": {
			frame: protectedFrameHeader(MaxProtectedBrokerFrameBytesV1 + 1),
			want:  ProtectedBrokerFrameTooLargeV1,
		},
		"truncated header": {
			frame: []byte{0, 0},
			want:  ProtectedBrokerUnavailableV1,
		},
		"truncated payload": {
			frame: append(protectedFrameHeader(4), '{'),
			want:  ProtectedBrokerUnavailableV1,
		},
		"malformed JSON": {
			frame: protectedFrame([]byte(`{"schema":`)),
			want:  ProtectedBrokerInvalidFrameV1,
		},
		"unknown field": {
			frame: protectedFrame([]byte(`{"schema":"execution.protected-broker.test.v1","value":"ok","nested":{"count":7},"unexpected":true}`)),
			want:  ProtectedBrokerInvalidFrameV1,
		},
		"case-folded field": {
			frame: protectedFrame([]byte(`{"Schema":"execution.protected-broker.test.v1","value":"ok","nested":{"count":7}}`)),
			want:  ProtectedBrokerInvalidFrameV1,
		},
		"missing field": {
			frame: protectedFrame([]byte(`{"schema":"execution.protected-broker.test.v1","nested":{"count":7}}`)),
			want:  ProtectedBrokerInvalidFrameV1,
		},
		"null required field": {
			frame: protectedFrame([]byte(`{"schema":"execution.protected-broker.test.v1","value":null,"nested":{"count":7}}`)),
			want:  ProtectedBrokerInvalidFrameV1,
		},
		"duplicate field": {
			frame: protectedFrame([]byte(`{"schema":"execution.protected-broker.test.v1","value":"first","value":"second","nested":{"count":7}}`)),
			want:  ProtectedBrokerInvalidFrameV1,
		},
		"nested duplicate": {
			frame: protectedFrame([]byte(`{"schema":"execution.protected-broker.test.v1","value":"ok","nested":{"count":7,"count":8}}`)),
			want:  ProtectedBrokerInvalidFrameV1,
		},
		"trailing JSON": {
			frame: protectedFrame([]byte(valid + `{}`)),
			want:  ProtectedBrokerInvalidFrameV1,
		},
		"invalid UTF-8": {
			frame: protectedFrame(append([]byte(`{"schema":"`), 0xff)),
			want:  ProtectedBrokerInvalidFrameV1,
		},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			var target protectedFrameFixture
			err := (ProtectedBrokerFrameCodecV1{}).ReadJSONFrame(bytes.NewReader(test.frame), &target)
			requireProtectedBrokerErrorClass(t, err, test.want)
		})
	}
}

func TestProtectedBrokerFrameCodecRejectsInvalidTargetsAndOversizedWrites(t *testing.T) {
	codec := ProtectedBrokerFrameCodecV1{}
	frame := protectedFrame([]byte(`{"schema":"execution.protected-broker.test.v1","value":"ok","nested":{"count":7}}`))

	var nonStruct map[string]any
	for name, target := range map[string]any{
		"nil":         nil,
		"non-pointer": protectedFrameFixture{},
		"nil pointer": (*protectedFrameFixture)(nil),
		"map":         &nonStruct,
	} {
		t.Run(name, func(t *testing.T) {
			requireProtectedBrokerErrorClass(
				t,
				codec.ReadJSONFrame(bytes.NewReader(frame), target),
				ProtectedBrokerInvalidFrameV1,
			)
		})
	}

	err := codec.WriteJSONFrame(io.Discard, protectedFrameFixture{
		Schema: "execution.protected-broker.test.v1",
		Value:  strings.Repeat("x", MaxProtectedBrokerFrameBytesV1),
		Nested: protectedFrameNestedFixture{Count: 1},
	})
	requireProtectedBrokerErrorClass(t, err, ProtectedBrokerFrameTooLargeV1)
}

func TestProtectedBrokerFrameCodecMapsIOErrorsWithoutDiagnostics(t *testing.T) {
	secret := "peer-secret-diagnostic"
	cases := map[string]struct {
		writer io.Writer
		want   ProtectedBrokerTransportErrorClassV1
	}{
		"broken pipe": {
			writer: frameErrorWriter{err: fmt.Errorf("%s: %w", secret, syscall.EPIPE)},
			want:   ProtectedBrokerUnavailableV1,
		},
		"other IO": {
			writer: frameErrorWriter{err: errors.New(secret)},
			want:   ProtectedBrokerIOV1,
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			err := (ProtectedBrokerFrameCodecV1{}).WriteJSONFrame(test.writer, protectedFrameFixture{
				Schema: "execution.protected-broker.test.v1",
				Value:  "ok",
				Nested: protectedFrameNestedFixture{Count: 1},
			})
			requireProtectedBrokerErrorClass(t, err, test.want)
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error leaked transport diagnostics: %v", err)
			}
		})
	}
}

func TestProtectedBrokerTransportErrorsAreStableAndRedacted(t *testing.T) {
	err := protectedBrokerError(ProtectedBrokerInvalidSignatureV1)
	if got, want := err.Error(), "protected broker transport failure: invalid_signature"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if strings.Contains(fmt.Sprintf("%+v", err), "signature-bytes") {
		t.Fatal("formatted error leaked peer bytes")
	}
}

type frameErrorWriter struct {
	err error
}

func (w frameErrorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func protectedFrame(payload []byte) []byte {
	frame := protectedFrameHeader(len(payload))
	return append(frame, payload...)
}

func protectedFrameHeader(length int) []byte {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(length))
	return header[:]
}

func requireProtectedBrokerErrorClass(
	t *testing.T,
	err error,
	want ProtectedBrokerTransportErrorClassV1,
) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s", want)
	}
	var transportError *ProtectedBrokerTransportErrorV1
	if !errors.As(err, &transportError) {
		t.Fatalf("error %T = %v, want ProtectedBrokerTransportErrorV1", err, err)
	}
	if got := transportError.Class(); got != want {
		t.Fatalf("error class = %s, want %s", got, want)
	}
}
