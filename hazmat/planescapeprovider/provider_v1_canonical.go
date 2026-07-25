package planescapeprovider

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"strings"
	"unicode/utf8"
)

var errProviderV1Frame = errors.New("planescapeprovider: provider-v1 frame rejected")

type providerV1CanonicalKind byte

const (
	providerV1CanonicalUTF8        providerV1CanonicalKind = 0x02
	providerV1CanonicalSortedList  providerV1CanonicalKind = 0x03
	providerV1CanonicalFingerprint providerV1CanonicalKind = 0x04
	providerV1CanonicalU64         providerV1CanonicalKind = 0x05
)

type providerV1CanonicalField struct {
	tag   byte
	kind  providerV1CanonicalKind
	value []byte
}

type providerV1CanonicalBuilder struct {
	domain string
	fields []providerV1CanonicalField
	err    error
}

func newProviderV1CanonicalBuilder(domain string) *providerV1CanonicalBuilder {
	return &providerV1CanonicalBuilder{domain: domain}
}

func (b *providerV1CanonicalBuilder) utf8(tag byte, value string) {
	if b == nil || b.err != nil {
		return
	}
	field, err := providerV1UTF8Field(tag, value)
	if err != nil {
		b.err = err
		return
	}
	b.fields = append(b.fields, field)
}

func (b *providerV1CanonicalBuilder) fingerprint(tag byte, value string) {
	if b == nil || b.err != nil {
		return
	}
	field, err := providerV1FingerprintField(tag, value)
	if err != nil {
		b.err = err
		return
	}
	b.fields = append(b.fields, field)
}

func (b *providerV1CanonicalBuilder) u64(tag byte, value uint64) {
	if b == nil || b.err != nil {
		return
	}
	b.fields = append(b.fields, providerV1U64Field(tag, value))
}

func (b *providerV1CanonicalBuilder) sortedList(tag byte, values []string) {
	if b == nil || b.err != nil {
		return
	}
	field, err := providerV1SortedListField(tag, values)
	if err != nil {
		b.err = err
		return
	}
	b.fields = append(b.fields, field)
}

func (b *providerV1CanonicalBuilder) preimage() ([]byte, error) {
	if b == nil || b.err != nil {
		return nil, errProviderV1Frame
	}
	return providerV1CanonicalPreimage(b.domain, b.fields...)
}

func providerV1UTF8Field(tag byte, value string) (providerV1CanonicalField, error) {
	if !validProviderV1Text(value) {
		return providerV1CanonicalField{}, errProviderV1Frame
	}
	return providerV1CanonicalField{
		tag:   tag,
		kind:  providerV1CanonicalUTF8,
		value: []byte(value),
	}, nil
}

func providerV1FingerprintField(tag byte, value string) (providerV1CanonicalField, error) {
	if _, err := ParseFingerprint(value); err != nil {
		return providerV1CanonicalField{}, errProviderV1Frame
	}
	return providerV1CanonicalField{
		tag:   tag,
		kind:  providerV1CanonicalFingerprint,
		value: []byte(value),
	}, nil
}

func providerV1U64Field(tag byte, value uint64) providerV1CanonicalField {
	encoded := make([]byte, 8)
	binary.BigEndian.PutUint64(encoded, value)
	return providerV1CanonicalField{
		tag:   tag,
		kind:  providerV1CanonicalU64,
		value: encoded,
	}
}

func providerV1SortedListField(tag byte, values []string) (providerV1CanonicalField, error) {
	if len(values) > maxCapabilities || !providerV1StringsStrictlySorted(values) {
		return providerV1CanonicalField{}, errProviderV1Frame
	}
	size := 4
	for _, value := range values {
		if !validProviderV1Text(value) || len(value) > math.MaxUint32 {
			return providerV1CanonicalField{}, errProviderV1Frame
		}
		size += 4 + len(value)
		if size > MaxRecordBytes {
			return providerV1CanonicalField{}, errProviderV1Frame
		}
	}
	encoded := make([]byte, 4, size)
	binary.BigEndian.PutUint32(encoded, uint32(len(values)))
	for _, value := range values {
		length := make([]byte, 4)
		binary.BigEndian.PutUint32(length, uint32(len(value)))
		encoded = append(encoded, length...)
		encoded = append(encoded, value...)
	}
	return providerV1CanonicalField{
		tag:   tag,
		kind:  providerV1CanonicalSortedList,
		value: encoded,
	}, nil
}

func providerV1CanonicalPreimage(
	domain string,
	fields ...providerV1CanonicalField,
) ([]byte, error) {
	if !validProviderV1Text(domain) || len(fields) == 0 || len(fields) > math.MaxUint16 {
		return nil, errProviderV1Frame
	}
	size := len(domain) + 2
	var previousTag byte
	for index, field := range fields {
		if field.tag == 0 || (index > 0 && field.tag <= previousTag) ||
			len(field.value) > math.MaxUint32 {
			return nil, errProviderV1Frame
		}
		switch field.kind {
		case providerV1CanonicalUTF8,
			providerV1CanonicalSortedList,
			providerV1CanonicalFingerprint,
			providerV1CanonicalU64:
		default:
			return nil, errProviderV1Frame
		}
		if field.kind == providerV1CanonicalU64 && len(field.value) != 8 {
			return nil, errProviderV1Frame
		}
		size += 6 + len(field.value)
		if size > MaxRecordBytes {
			return nil, errProviderV1Frame
		}
		previousTag = field.tag
	}

	preimage := make([]byte, 0, size)
	preimage = append(preimage, domain...)
	fieldCount := make([]byte, 2)
	binary.BigEndian.PutUint16(fieldCount, uint16(len(fields)))
	preimage = append(preimage, fieldCount...)
	for _, field := range fields {
		preimage = append(preimage, field.tag, byte(field.kind))
		length := make([]byte, 4)
		binary.BigEndian.PutUint32(length, uint32(len(field.value)))
		preimage = append(preimage, length...)
		preimage = append(preimage, field.value...)
	}
	return preimage, nil
}

func providerV1CanonicalHash(preimage []byte) string {
	digest := sha256.Sum256(preimage)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func providerV1ValidateCanonicalHash(
	supplied string,
	preimage []byte,
) error {
	if _, err := ParseFingerprint(supplied); err != nil ||
		providerV1CanonicalHash(preimage) != supplied {
		return errProviderV1Frame
	}
	return nil
}

func providerV1StringsStrictlySorted(values []string) bool {
	for index, value := range values {
		if index > 0 && strings.Compare(values[index-1], value) >= 0 {
			return false
		}
	}
	return true
}

func validProviderV1Text(value string) bool {
	return value != "" &&
		utf8.ValidString(value) &&
		!strings.ContainsFunc(value, func(r rune) bool {
			return r < 0x20 || r == 0x7f
		})
}
