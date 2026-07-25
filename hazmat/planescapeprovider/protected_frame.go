package planescapeprovider

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"reflect"
	"strings"
	"syscall"
	"unicode/utf8"
)

// MaxProtectedBrokerFrameBytesV1 is the exact protected-transport JSON payload
// ceiling. The four-byte big-endian length prefix is not part of this limit.
const MaxProtectedBrokerFrameBytesV1 = 16 * 1024

// ProtectedBrokerTransportErrorClassV1 is the stable fail-closed taxonomy
// shared with the protected broker transport. Error values intentionally retain
// no peer-supplied bytes or underlying diagnostic text.
type ProtectedBrokerTransportErrorClassV1 string

const (
	ProtectedBrokerUnavailableV1           ProtectedBrokerTransportErrorClassV1 = "unavailable"
	ProtectedBrokerIOV1                    ProtectedBrokerTransportErrorClassV1 = "io"
	ProtectedBrokerEntropyUnavailableV1    ProtectedBrokerTransportErrorClassV1 = "entropy_unavailable"
	ProtectedBrokerFrameTooLargeV1         ProtectedBrokerTransportErrorClassV1 = "frame_too_large"
	ProtectedBrokerInvalidFrameV1          ProtectedBrokerTransportErrorClassV1 = "invalid_frame"
	ProtectedBrokerInvalidSchemaV1         ProtectedBrokerTransportErrorClassV1 = "invalid_schema"
	ProtectedBrokerInvalidNonceV1          ProtectedBrokerTransportErrorClassV1 = "invalid_nonce"
	ProtectedBrokerHashMismatchV1          ProtectedBrokerTransportErrorClassV1 = "hash_mismatch"
	ProtectedBrokerInvalidSignatureV1      ProtectedBrokerTransportErrorClassV1 = "invalid_signature"
	ProtectedBrokerWrongBrokerKeyV1        ProtectedBrokerTransportErrorClassV1 = "wrong_broker_key"
	ProtectedBrokerWrongBrokerIdentityV1   ProtectedBrokerTransportErrorClassV1 = "wrong_broker_identity"
	ProtectedBrokerStaleBrokerEpochV1      ProtectedBrokerTransportErrorClassV1 = "stale_broker_epoch"
	ProtectedBrokerWrongClientV1           ProtectedBrokerTransportErrorClassV1 = "wrong_client"
	ProtectedBrokerReplayBindingMismatchV1 ProtectedBrokerTransportErrorClassV1 = "replay_binding_mismatch"
)

// ProtectedBrokerTransportErrorV1 is inspectable by class without exposing
// transport diagnostics or malformed frame contents.
type ProtectedBrokerTransportErrorV1 struct {
	class ProtectedBrokerTransportErrorClassV1
}

func (e *ProtectedBrokerTransportErrorV1) Error() string {
	if e == nil {
		return "protected broker transport failure"
	}
	return "protected broker transport failure: " + string(e.class)
}

func (e *ProtectedBrokerTransportErrorV1) Class() ProtectedBrokerTransportErrorClassV1 {
	if e == nil {
		return ""
	}
	return e.class
}

func protectedBrokerError(class ProtectedBrokerTransportErrorClassV1) error {
	return &ProtectedBrokerTransportErrorV1{class: class}
}

// ProtectedBrokerFrameCodecV1 encodes and decodes the transport's bounded
// length-prefixed JSON records. Its zero value is ready for use.
type ProtectedBrokerFrameCodecV1 struct{}

// WriteJSONFrame writes a four-byte big-endian payload length followed by one
// compact JSON value and flushes buffered writers when supported.
func (ProtectedBrokerFrameCodecV1) WriteJSONFrame(writer io.Writer, value any) error {
	if writer == nil {
		return protectedBrokerError(ProtectedBrokerIOV1)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return protectedBrokerError(ProtectedBrokerInvalidFrameV1)
	}
	if len(payload) == 0 || len(payload) > MaxProtectedBrokerFrameBytesV1 {
		return protectedBrokerError(ProtectedBrokerFrameTooLargeV1)
	}

	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if err := writeProtectedBrokerBytes(writer, header[:]); err != nil {
		return err
	}
	if err := writeProtectedBrokerBytes(writer, payload); err != nil {
		return err
	}
	if flusher, ok := writer.(interface{ Flush() error }); ok {
		if err := flusher.Flush(); err != nil {
			return mapProtectedBrokerIOError(err)
		}
	}
	return nil
}

// ReadJSONFrame reads exactly one bounded frame into a pointer to a JSON
// struct. Field names are case-sensitive, unknown and duplicate fields fail,
// and every non-optional field must be present.
func (ProtectedBrokerFrameCodecV1) ReadJSONFrame(reader io.Reader, target any) error {
	policy, err := protectedJSONFieldPolicy(target)
	if err != nil {
		return protectedBrokerError(ProtectedBrokerInvalidFrameV1)
	}
	if reader == nil {
		return protectedBrokerError(ProtectedBrokerIOV1)
	}

	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return mapProtectedBrokerIOError(err)
	}
	length := uint64(binary.BigEndian.Uint32(header[:]))
	if length == 0 || length > MaxProtectedBrokerFrameBytesV1 {
		return protectedBrokerError(ProtectedBrokerFrameTooLargeV1)
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return mapProtectedBrokerIOError(err)
	}
	if err := validateProtectedBrokerJSON(payload, policy); err != nil {
		return protectedBrokerError(ProtectedBrokerInvalidFrameV1)
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return protectedBrokerError(ProtectedBrokerInvalidFrameV1)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return protectedBrokerError(ProtectedBrokerInvalidFrameV1)
	}
	return nil
}

type protectedJSONPolicy struct {
	allowed  map[string]struct{}
	required map[string]struct{}
}

func protectedJSONFieldPolicy(target any) (protectedJSONPolicy, error) {
	value := reflect.ValueOf(target)
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() {
		return protectedJSONPolicy{}, fmt.Errorf("protected broker frame target must be a pointer")
	}
	typ := value.Type().Elem()
	if typ.Kind() != reflect.Struct {
		return protectedJSONPolicy{}, fmt.Errorf("protected broker frame target must point to a struct")
	}

	policy := protectedJSONPolicy{
		allowed:  make(map[string]struct{}, typ.NumField()),
		required: make(map[string]struct{}, typ.NumField()),
	}
	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		if !field.IsExported() {
			continue
		}
		if field.Anonymous {
			return protectedJSONPolicy{}, fmt.Errorf("protected broker frame cannot use embedded fields")
		}
		tag := field.Tag.Get("json")
		parts := strings.Split(tag, ",")
		name := parts[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		if _, exists := policy.allowed[name]; exists {
			return protectedJSONPolicy{}, fmt.Errorf("protected broker frame has duplicate JSON field tags")
		}
		policy.allowed[name] = struct{}{}
		if !containsJSONOption(parts[1:], "omitempty") && !containsJSONOption(parts[1:], "omitzero") {
			policy.required[name] = struct{}{}
		}
	}
	if len(policy.allowed) == 0 {
		return protectedJSONPolicy{}, fmt.Errorf("protected broker frame target has no fields")
	}
	return policy, nil
}

func containsJSONOption(options []string, expected string) bool {
	for _, option := range options {
		if option == expected {
			return true
		}
	}
	return false
}

func validateProtectedBrokerJSON(payload []byte, policy protectedJSONPolicy) error {
	if !utf8.Valid(payload) {
		return fmt.Errorf("protected broker frame is not UTF-8")
	}
	if err := rejectDuplicateProtectedJSONFields(payload); err != nil {
		return err
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil || object == nil {
		return fmt.Errorf("protected broker frame must be a JSON object")
	}
	for name := range object {
		if _, ok := policy.allowed[name]; !ok {
			return fmt.Errorf("protected broker frame has an unknown field")
		}
	}
	for name := range policy.required {
		value, ok := object[name]
		if !ok {
			return fmt.Errorf("protected broker frame is missing a field")
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("protected broker frame has a null required field")
		}
	}
	return nil
}

func rejectDuplicateProtectedJSONFields(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := scanProtectedJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("protected broker frame has trailing JSON")
	}
	return nil
}

func scanProtectedJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("protected broker object key is invalid")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("protected broker frame has duplicate fields")
			}
			seen[key] = struct{}{}
			if err := scanProtectedJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return fmt.Errorf("protected broker object is incomplete")
		}
	case '[':
		for decoder.More() {
			if err := scanProtectedJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return fmt.Errorf("protected broker array is incomplete")
		}
	default:
		return fmt.Errorf("protected broker JSON delimiter is invalid")
	}
	return nil
}

func writeProtectedBrokerBytes(writer io.Writer, value []byte) error {
	for len(value) > 0 {
		written, err := writer.Write(value)
		if err != nil {
			return mapProtectedBrokerIOError(err)
		}
		if written <= 0 || written > len(value) {
			return protectedBrokerError(ProtectedBrokerIOV1)
		}
		value = value[written:]
	}
	return nil
}

func mapProtectedBrokerIOError(err error) error {
	if isUnavailableProtectedBrokerIOError(err) {
		return protectedBrokerError(ProtectedBrokerUnavailableV1)
	}
	return protectedBrokerError(ProtectedBrokerIOV1)
}

func isUnavailableProtectedBrokerIOError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, io.ErrClosedPipe) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, os.ErrDeadlineExceeded) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ENOTCONN) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}
