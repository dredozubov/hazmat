package planescapeprovider

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
)

const (
	protectedBrokerClientHelloSchemaV1     = "execution.protected-broker.client-hello.v1"
	protectedBrokerServerChallengeSchemaV1 = "execution.protected-broker.server-challenge.v1"
	protectedBrokerClientFinishSchemaV1    = "execution.protected-broker.client-finish.v1"
	protectedBrokerServerAcceptedSchemaV1  = "execution.protected-broker.server-accepted.v1"
	protectedBrokerClientAuthoritySchemaV1 = "execution.protected-broker.authenticated-client-authority.v1"

	protectedBrokerBackendKindV1 = "external_containment_broker"
	protectedBrokerSignatureV1   = "sig-ed25519:"
	protectedBrokerNonceBytesV1  = 32

	protectedBrokerBackendIdentityHashDomainV1  = "planescape.execution.containment-backend-identity.v1\x00"
	protectedBrokerClientKeyHashDomainV1        = "planescape.execution.protected-broker.client-key.v1\x00"
	protectedBrokerClientHelloHashDomainV1      = "planescape.execution.protected-broker.client-hello.v1\x00"
	protectedBrokerServerChallengeHashDomainV1  = "planescape.execution.protected-broker.server-challenge.v1\x00"
	protectedBrokerServerChallengeSigDomainV1   = "planescape.execution.protected-broker.server-challenge-signature.v1\x00"
	protectedBrokerClientAuthorityHashDomainV1  = "planescape.execution.protected-broker.authenticated-client-authority.v1\x00"
	protectedBrokerTransportSessionHashDomainV1 = "planescape.execution.protected-broker.transport-session.v1\x00"
	protectedBrokerClientFinishHashDomainV1     = "planescape.execution.protected-broker.client-finish.v1\x00"
	protectedBrokerClientFinishSigDomainV1      = "planescape.execution.protected-broker.client-finish-signature.v1\x00"
	protectedBrokerServerAcceptedHashDomainV1   = "planescape.execution.protected-broker.server-accepted.v1\x00"
	protectedBrokerServerAcceptedSigDomainV1    = "planescape.execution.protected-broker.server-accepted-signature.v1\x00"
)

// ProtectedBrokerBackendIdentityInputV1 contains the exact backend components
// committed by Planescape's containment-backend identity. The backend kind is
// closed to external_containment_broker.
type ProtectedBrokerBackendIdentityInputV1 struct {
	BackendInstanceSHA256      string
	ExecutableSHA256           string
	ExecutionEnvironmentSHA256 string
	ProfileSHA256              string
	BrokerEpoch                uint64
}

// ProtectedBrokerBackendIdentityV1 is a validated, pinned backend identity.
// Its zero value is invalid.
type ProtectedBrokerBackendIdentityV1 struct {
	backendInstanceSHA256      protectedBrokerHashV1
	executableSHA256           protectedBrokerHashV1
	executionEnvironmentSHA256 protectedBrokerHashV1
	profileSHA256              protectedBrokerHashV1
	brokerEpoch                protectedBrokerEpochV1
	attestorPublicKeySHA256    protectedBrokerHashV1
	identitySHA256             protectedBrokerHashV1
}

// NewProtectedBrokerBackendIdentityV1 derives the same canonical backend hash
// as Planescape's ContainmentBackendIdentityV1::pinned.
func NewProtectedBrokerBackendIdentityV1(
	input ProtectedBrokerBackendIdentityInputV1,
	attestorPublicKey ed25519.PublicKey,
) (ProtectedBrokerBackendIdentityV1, error) {
	if len(attestorPublicKey) != ed25519.PublicKeySize {
		return ProtectedBrokerBackendIdentityV1{}, protectedBrokerError(ProtectedBrokerWrongBrokerKeyV1)
	}
	backendInstanceSHA256, err := parseProtectedBrokerHashV1(input.BackendInstanceSHA256)
	if err != nil {
		return ProtectedBrokerBackendIdentityV1{}, protectedBrokerError(ProtectedBrokerInvalidFrameV1)
	}
	executableSHA256, err := parseProtectedBrokerHashV1(input.ExecutableSHA256)
	if err != nil {
		return ProtectedBrokerBackendIdentityV1{}, protectedBrokerError(ProtectedBrokerInvalidFrameV1)
	}
	executionEnvironmentSHA256, err := parseProtectedBrokerHashV1(input.ExecutionEnvironmentSHA256)
	if err != nil {
		return ProtectedBrokerBackendIdentityV1{}, protectedBrokerError(ProtectedBrokerInvalidFrameV1)
	}
	profileSHA256, err := parseProtectedBrokerHashV1(input.ProfileSHA256)
	if err != nil {
		return ProtectedBrokerBackendIdentityV1{}, protectedBrokerError(ProtectedBrokerInvalidFrameV1)
	}
	if input.BrokerEpoch == 0 {
		return ProtectedBrokerBackendIdentityV1{}, protectedBrokerError(ProtectedBrokerInvalidFrameV1)
	}

	identity := ProtectedBrokerBackendIdentityV1{
		backendInstanceSHA256:      backendInstanceSHA256,
		executableSHA256:           executableSHA256,
		executionEnvironmentSHA256: executionEnvironmentSHA256,
		profileSHA256:              profileSHA256,
		brokerEpoch:                protectedBrokerEpochV1(input.BrokerEpoch),
		attestorPublicKeySHA256:    protectedBrokerRawHashV1(attestorPublicKey),
	}
	identity.identitySHA256, err = identity.expectedHash()
	if err != nil {
		return ProtectedBrokerBackendIdentityV1{}, err
	}
	return identity, nil
}

// IdentitySHA256 is the exact canonical containment-backend identity hash.
func (v ProtectedBrokerBackendIdentityV1) IdentitySHA256() Fingerprint {
	return v.identitySHA256.fingerprint()
}

func (v ProtectedBrokerBackendIdentityV1) BrokerEpoch() ProviderEpoch {
	return ProviderEpoch(v.brokerEpoch)
}

func (v ProtectedBrokerBackendIdentityV1) ProfileSHA256() Fingerprint {
	return v.profileSHA256.fingerprint()
}

func (v ProtectedBrokerBackendIdentityV1) AttestorPublicKeySHA256() Fingerprint {
	return v.attestorPublicKeySHA256.fingerprint()
}

func (v ProtectedBrokerBackendIdentityV1) expectedHash() (protectedBrokerHashV1, error) {
	return hashProtectedBrokerJSONV1(
		protectedBrokerBackendIdentityHashDomainV1,
		protectedBrokerBackendKindV1,
		v.backendInstanceSHA256,
		v.executableSHA256,
		v.executionEnvironmentSHA256,
		v.profileSHA256,
		v.brokerEpoch,
		v.attestorPublicKeySHA256,
	)
}

func (v ProtectedBrokerBackendIdentityV1) valid() bool {
	if !v.backendInstanceSHA256.valid() ||
		!v.executableSHA256.valid() ||
		!v.executionEnvironmentSHA256.valid() ||
		!v.profileSHA256.valid() ||
		v.brokerEpoch == 0 ||
		!v.attestorPublicKeySHA256.valid() ||
		!v.identitySHA256.valid() {
		return false
	}
	expected, err := v.expectedHash()
	return err == nil && expected == v.identitySHA256
}

// ProtectedBrokerClientV1 is immutable reconnectable client configuration.
// It retains no fallback transport and does not expose its private signing key.
type ProtectedBrokerClientV1 struct {
	expectedBackend ProtectedBrokerBackendIdentityV1
	brokerKey       ed25519.PublicKey
	clientKey       ed25519.PrivateKey
	clientKeySHA256 protectedBrokerHashV1
}

// NewProtectedBrokerClientV1 validates the broker key against the pinned
// backend identity and defensively copies both keys.
func NewProtectedBrokerClientV1(
	expectedBackend ProtectedBrokerBackendIdentityV1,
	brokerKey ed25519.PublicKey,
	clientKey ed25519.PrivateKey,
) (*ProtectedBrokerClientV1, error) {
	if !expectedBackend.valid() {
		return nil, protectedBrokerError(ProtectedBrokerWrongBrokerIdentityV1)
	}
	if len(brokerKey) != ed25519.PublicKeySize ||
		protectedBrokerRawHashV1(brokerKey) != expectedBackend.attestorPublicKeySHA256 {
		return nil, protectedBrokerError(ProtectedBrokerWrongBrokerKeyV1)
	}
	if !validProtectedBrokerPrivateKeyV1(clientKey) {
		return nil, protectedBrokerError(ProtectedBrokerInvalidFrameV1)
	}

	brokerKey = append(ed25519.PublicKey(nil), brokerKey...)
	clientKey = append(ed25519.PrivateKey(nil), clientKey...)
	clientPublicKey := ed25519.PublicKey(clientKey[ed25519.SeedSize:])
	return &ProtectedBrokerClientV1{
		expectedBackend: expectedBackend,
		brokerKey:       brokerKey,
		clientKey:       clientKey,
		clientKeySHA256: hashProtectedBrokerBytesV1(protectedBrokerClientKeyHashDomainV1, clientPublicKey),
	}, nil
}

// Authenticate performs exactly one fresh mutual-authentication handshake over
// the supplied byte stream. No admission or provider RPC is exposed here.
func (c *ProtectedBrokerClientV1) Authenticate(
	stream io.ReadWriter,
) (AuthenticatedBrokerClientSessionV1, error) {
	if c == nil || !c.valid() {
		return AuthenticatedBrokerClientSessionV1{}, protectedBrokerError(ProtectedBrokerWrongBrokerIdentityV1)
	}
	nonce, err := randomProtectedBrokerNonceV1()
	if err != nil {
		return AuthenticatedBrokerClientSessionV1{}, err
	}
	return c.authenticateWithNonce(stream, nonce)
}

func (c *ProtectedBrokerClientV1) authenticateWithNonce(
	stream io.ReadWriter,
	clientNonce protectedBrokerNonceV1,
) (AuthenticatedBrokerClientSessionV1, error) {
	if c == nil || !c.valid() {
		return AuthenticatedBrokerClientSessionV1{}, protectedBrokerError(ProtectedBrokerWrongBrokerIdentityV1)
	}
	if stream == nil {
		return AuthenticatedBrokerClientSessionV1{}, protectedBrokerError(ProtectedBrokerIOV1)
	}
	if !clientNonce.valid() {
		return AuthenticatedBrokerClientSessionV1{}, protectedBrokerError(ProtectedBrokerInvalidNonceV1)
	}

	hello, err := newProtectedBrokerClientHelloV1(c.clientKeySHA256, clientNonce)
	if err != nil {
		return AuthenticatedBrokerClientSessionV1{}, err
	}
	codec := ProtectedBrokerFrameCodecV1{}
	if err := codec.WriteJSONFrame(stream, hello); err != nil {
		return AuthenticatedBrokerClientSessionV1{}, err
	}

	var challenge protectedBrokerServerChallengeWireV1
	if err := codec.ReadJSONFrame(stream, &challenge); err != nil {
		return AuthenticatedBrokerClientSessionV1{}, err
	}
	clientAuthoritySHA256, err := protectedBrokerClientAuthorityHashV1(
		c.clientKeySHA256,
		c.expectedBackend,
	)
	if err != nil {
		return AuthenticatedBrokerClientSessionV1{}, err
	}
	serverNonce, err := challenge.validate(
		hello,
		c.expectedBackend,
		clientAuthoritySHA256,
		c.brokerKey,
	)
	if err != nil {
		return AuthenticatedBrokerClientSessionV1{}, err
	}
	transportSessionSHA256, err := protectedBrokerTransportSessionHashV1(
		hello,
		challenge,
		clientAuthoritySHA256,
		c.clientKeySHA256,
	)
	if err != nil {
		return AuthenticatedBrokerClientSessionV1{}, err
	}
	finish, err := newProtectedBrokerClientFinishV1(
		challenge,
		c.clientKeySHA256,
		clientAuthoritySHA256,
		transportSessionSHA256,
		c.clientKey,
	)
	if err != nil {
		return AuthenticatedBrokerClientSessionV1{}, err
	}
	if err := codec.WriteJSONFrame(stream, finish); err != nil {
		return AuthenticatedBrokerClientSessionV1{}, err
	}

	var accepted protectedBrokerServerAcceptedWireV1
	if err := codec.ReadJSONFrame(stream, &accepted); err != nil {
		return AuthenticatedBrokerClientSessionV1{}, err
	}
	if err := accepted.validate(
		finish,
		clientAuthoritySHA256,
		transportSessionSHA256,
		c.brokerKey,
	); err != nil {
		return AuthenticatedBrokerClientSessionV1{}, err
	}

	return AuthenticatedBrokerClientSessionV1{
		backendIdentitySHA256:  c.expectedBackend.identitySHA256,
		brokerEpoch:            c.expectedBackend.brokerEpoch,
		profileSHA256:          c.expectedBackend.profileSHA256,
		clientKeySHA256:        c.clientKeySHA256,
		clientAuthoritySHA256:  clientAuthoritySHA256,
		transportSessionSHA256: transportSessionSHA256,
		clientNonce:            clientNonce,
		serverNonce:            serverNonce,
	}, nil
}

func (c ProtectedBrokerClientV1) String() string {
	return "ProtectedBrokerClientV1{backend_identity_sha256:" +
		c.expectedBackend.identitySHA256.String() +
		",broker_epoch:" + c.expectedBackend.brokerEpoch.String() +
		",client_key_sha256:" + c.clientKeySHA256.String() +
		"}"
}

func (c ProtectedBrokerClientV1) GoString() string {
	return c.String()
}

func (c *ProtectedBrokerClientV1) valid() bool {
	if c == nil ||
		!c.expectedBackend.valid() ||
		len(c.brokerKey) != ed25519.PublicKeySize ||
		len(c.clientKey) != ed25519.PrivateKeySize ||
		!validProtectedBrokerPrivateKeyV1(c.clientKey) {
		return false
	}
	if protectedBrokerRawHashV1(c.brokerKey) != c.expectedBackend.attestorPublicKeySHA256 {
		return false
	}
	clientPublicKey := ed25519.PublicKey(c.clientKey[ed25519.SeedSize:])
	return hashProtectedBrokerBytesV1(protectedBrokerClientKeyHashDomainV1, clientPublicKey) == c.clientKeySHA256
}

// AuthenticatedBrokerClientSessionV1 proves that the pinned broker accepted
// the client's fresh mutual-authentication handshake.
type AuthenticatedBrokerClientSessionV1 struct {
	backendIdentitySHA256  protectedBrokerHashV1
	brokerEpoch            protectedBrokerEpochV1
	profileSHA256          protectedBrokerHashV1
	clientKeySHA256        protectedBrokerHashV1
	clientAuthoritySHA256  protectedBrokerHashV1
	transportSessionSHA256 protectedBrokerHashV1
	clientNonce            protectedBrokerNonceV1
	serverNonce            protectedBrokerNonceV1
}

func (v AuthenticatedBrokerClientSessionV1) BackendIdentitySHA256() Fingerprint {
	return v.backendIdentitySHA256.fingerprint()
}

func (v AuthenticatedBrokerClientSessionV1) BrokerEpoch() ProviderEpoch {
	return ProviderEpoch(v.brokerEpoch)
}

func (v AuthenticatedBrokerClientSessionV1) ProfileSHA256() Fingerprint {
	return v.profileSHA256.fingerprint()
}

func (v AuthenticatedBrokerClientSessionV1) ClientKeySHA256() Fingerprint {
	return v.clientKeySHA256.fingerprint()
}

// ClientAuthoritySHA256 is reconnect-stable and is the only identity suitable
// for durable service or lease ownership.
func (v AuthenticatedBrokerClientSessionV1) ClientAuthoritySHA256() Fingerprint {
	return v.clientAuthoritySHA256.fingerprint()
}

// TransportSessionSHA256 is fresh for this handshake and is reserved for
// connection-local ordering and replay protection.
func (v AuthenticatedBrokerClientSessionV1) TransportSessionSHA256() Fingerprint {
	return v.transportSessionSHA256.fingerprint()
}

func (v AuthenticatedBrokerClientSessionV1) String() string {
	return "AuthenticatedBrokerClientSessionV1{backend_identity_sha256:" +
		v.backendIdentitySHA256.String() +
		",broker_epoch:" + v.brokerEpoch.String() +
		",client_key_sha256:" + v.clientKeySHA256.String() +
		",client_authority_sha256:" + v.clientAuthoritySHA256.String() +
		",transport_session_sha256:" + v.transportSessionSHA256.String() +
		"}"
}

func (v AuthenticatedBrokerClientSessionV1) GoString() string {
	return v.String()
}

type protectedBrokerClientHelloWireV1 struct {
	Schema          string                `json:"schema"`
	ClientKeySHA256 protectedBrokerHashV1 `json:"client_key_sha256"`
	ClientNonce     string                `json:"client_nonce"`
	HelloSHA256     protectedBrokerHashV1 `json:"hello_sha256"`
}

func newProtectedBrokerClientHelloV1(
	clientKeySHA256 protectedBrokerHashV1,
	clientNonce protectedBrokerNonceV1,
) (protectedBrokerClientHelloWireV1, error) {
	encodedNonce := clientNonce.encoded()
	helloSHA256, err := hashProtectedBrokerJSONV1(
		protectedBrokerClientHelloHashDomainV1,
		protectedBrokerClientHelloSchemaV1,
		clientKeySHA256,
		encodedNonce,
	)
	if err != nil {
		return protectedBrokerClientHelloWireV1{}, err
	}
	return protectedBrokerClientHelloWireV1{
		Schema:          protectedBrokerClientHelloSchemaV1,
		ClientKeySHA256: clientKeySHA256,
		ClientNonce:     encodedNonce,
		HelloSHA256:     helloSHA256,
	}, nil
}

type protectedBrokerServerChallengeWireV1 struct {
	Schema                string                 `json:"schema"`
	HelloSHA256           protectedBrokerHashV1  `json:"hello_sha256"`
	ServerNonce           string                 `json:"server_nonce"`
	BackendIdentitySHA256 protectedBrokerHashV1  `json:"backend_identity_sha256"`
	BrokerEpoch           protectedBrokerEpochV1 `json:"broker_epoch"`
	ProfileSHA256         protectedBrokerHashV1  `json:"profile_sha256"`
	ClientAuthoritySHA256 protectedBrokerHashV1  `json:"client_authority_sha256"`
	ChallengeSHA256       protectedBrokerHashV1  `json:"challenge_sha256"`
	Signature             string                 `json:"signature"`
}

func (v protectedBrokerServerChallengeWireV1) validate(
	hello protectedBrokerClientHelloWireV1,
	expectedBackend ProtectedBrokerBackendIdentityV1,
	expectedClientAuthoritySHA256 protectedBrokerHashV1,
	brokerKey ed25519.PublicKey,
) (protectedBrokerNonceV1, error) {
	if v.Schema != protectedBrokerServerChallengeSchemaV1 {
		return protectedBrokerNonceV1{}, protectedBrokerError(ProtectedBrokerInvalidSchemaV1)
	}
	nonce, err := parseProtectedBrokerNonceV1(v.ServerNonce)
	if err != nil {
		return protectedBrokerNonceV1{}, err
	}
	expectedHash, err := hashProtectedBrokerJSONV1(
		protectedBrokerServerChallengeHashDomainV1,
		protectedBrokerServerChallengeSchemaV1,
		v.HelloSHA256,
		v.ServerNonce,
		v.BackendIdentitySHA256,
		v.BrokerEpoch,
		v.ProfileSHA256,
		v.ClientAuthoritySHA256,
	)
	if err != nil {
		return protectedBrokerNonceV1{}, err
	}
	if v.ChallengeSHA256 != expectedHash {
		return protectedBrokerNonceV1{}, protectedBrokerError(ProtectedBrokerHashMismatchV1)
	}
	if err := verifyProtectedBrokerJSONV1(
		protectedBrokerServerChallengeSigDomainV1,
		v.Signature,
		brokerKey,
		protectedBrokerServerChallengeSchemaV1,
		v.HelloSHA256,
		v.ServerNonce,
		v.BackendIdentitySHA256,
		v.BrokerEpoch,
		v.ProfileSHA256,
		v.ClientAuthoritySHA256,
		v.ChallengeSHA256,
	); err != nil {
		return protectedBrokerNonceV1{}, err
	}
	if v.HelloSHA256 != hello.HelloSHA256 {
		return protectedBrokerNonceV1{}, protectedBrokerError(ProtectedBrokerReplayBindingMismatchV1)
	}
	if v.BrokerEpoch != expectedBackend.brokerEpoch {
		return protectedBrokerNonceV1{}, protectedBrokerError(ProtectedBrokerStaleBrokerEpochV1)
	}
	if v.BackendIdentitySHA256 != expectedBackend.identitySHA256 ||
		v.ProfileSHA256 != expectedBackend.profileSHA256 ||
		v.ClientAuthoritySHA256 != expectedClientAuthoritySHA256 {
		return protectedBrokerNonceV1{}, protectedBrokerError(ProtectedBrokerWrongBrokerIdentityV1)
	}
	return nonce, nil
}

type protectedBrokerClientFinishWireV1 struct {
	Schema                 string                `json:"schema"`
	ChallengeSHA256        protectedBrokerHashV1 `json:"challenge_sha256"`
	ClientKeySHA256        protectedBrokerHashV1 `json:"client_key_sha256"`
	ClientAuthoritySHA256  protectedBrokerHashV1 `json:"client_authority_sha256"`
	TransportSessionSHA256 protectedBrokerHashV1 `json:"transport_session_sha256"`
	FinishSHA256           protectedBrokerHashV1 `json:"finish_sha256"`
	Signature              string                `json:"signature"`
}

func newProtectedBrokerClientFinishV1(
	challenge protectedBrokerServerChallengeWireV1,
	clientKeySHA256 protectedBrokerHashV1,
	clientAuthoritySHA256 protectedBrokerHashV1,
	transportSessionSHA256 protectedBrokerHashV1,
	clientKey ed25519.PrivateKey,
) (protectedBrokerClientFinishWireV1, error) {
	finishSHA256, err := hashProtectedBrokerJSONV1(
		protectedBrokerClientFinishHashDomainV1,
		protectedBrokerClientFinishSchemaV1,
		challenge.ChallengeSHA256,
		clientKeySHA256,
		clientAuthoritySHA256,
		transportSessionSHA256,
	)
	if err != nil {
		return protectedBrokerClientFinishWireV1{}, err
	}
	signature, err := signProtectedBrokerJSONV1(
		protectedBrokerClientFinishSigDomainV1,
		clientKey,
		protectedBrokerClientFinishSchemaV1,
		challenge.ChallengeSHA256,
		clientKeySHA256,
		clientAuthoritySHA256,
		transportSessionSHA256,
		finishSHA256,
	)
	if err != nil {
		return protectedBrokerClientFinishWireV1{}, err
	}
	return protectedBrokerClientFinishWireV1{
		Schema:                 protectedBrokerClientFinishSchemaV1,
		ChallengeSHA256:        challenge.ChallengeSHA256,
		ClientKeySHA256:        clientKeySHA256,
		ClientAuthoritySHA256:  clientAuthoritySHA256,
		TransportSessionSHA256: transportSessionSHA256,
		FinishSHA256:           finishSHA256,
		Signature:              signature,
	}, nil
}

type protectedBrokerServerAcceptedWireV1 struct {
	Schema                 string                `json:"schema"`
	FinishSHA256           protectedBrokerHashV1 `json:"finish_sha256"`
	ClientAuthoritySHA256  protectedBrokerHashV1 `json:"client_authority_sha256"`
	TransportSessionSHA256 protectedBrokerHashV1 `json:"transport_session_sha256"`
	AcceptedSHA256         protectedBrokerHashV1 `json:"accepted_sha256"`
	Signature              string                `json:"signature"`
}

func (v protectedBrokerServerAcceptedWireV1) validate(
	finish protectedBrokerClientFinishWireV1,
	expectedClientAuthoritySHA256 protectedBrokerHashV1,
	expectedTransportSessionSHA256 protectedBrokerHashV1,
	brokerKey ed25519.PublicKey,
) error {
	if v.Schema != protectedBrokerServerAcceptedSchemaV1 {
		return protectedBrokerError(ProtectedBrokerInvalidSchemaV1)
	}
	expectedHash, err := hashProtectedBrokerJSONV1(
		protectedBrokerServerAcceptedHashDomainV1,
		protectedBrokerServerAcceptedSchemaV1,
		v.FinishSHA256,
		v.ClientAuthoritySHA256,
		v.TransportSessionSHA256,
	)
	if err != nil {
		return err
	}
	if v.AcceptedSHA256 != expectedHash {
		return protectedBrokerError(ProtectedBrokerHashMismatchV1)
	}
	if err := verifyProtectedBrokerJSONV1(
		protectedBrokerServerAcceptedSigDomainV1,
		v.Signature,
		brokerKey,
		protectedBrokerServerAcceptedSchemaV1,
		v.FinishSHA256,
		v.ClientAuthoritySHA256,
		v.TransportSessionSHA256,
		v.AcceptedSHA256,
	); err != nil {
		return err
	}
	if v.FinishSHA256 != finish.FinishSHA256 ||
		v.ClientAuthoritySHA256 != expectedClientAuthoritySHA256 ||
		v.TransportSessionSHA256 != expectedTransportSessionSHA256 {
		return protectedBrokerError(ProtectedBrokerReplayBindingMismatchV1)
	}
	return nil
}

func protectedBrokerClientAuthorityHashV1(
	clientKeySHA256 protectedBrokerHashV1,
	backend ProtectedBrokerBackendIdentityV1,
) (protectedBrokerHashV1, error) {
	return hashProtectedBrokerJSONV1(
		protectedBrokerClientAuthorityHashDomainV1,
		protectedBrokerClientAuthoritySchemaV1,
		clientKeySHA256,
		backend.identitySHA256,
		backend.brokerEpoch,
		backend.profileSHA256,
	)
}

func protectedBrokerTransportSessionHashV1(
	hello protectedBrokerClientHelloWireV1,
	challenge protectedBrokerServerChallengeWireV1,
	clientAuthoritySHA256 protectedBrokerHashV1,
	clientKeySHA256 protectedBrokerHashV1,
) (protectedBrokerHashV1, error) {
	return hashProtectedBrokerJSONV1(
		protectedBrokerTransportSessionHashDomainV1,
		clientAuthoritySHA256,
		hello.HelloSHA256,
		challenge.ChallengeSHA256,
		clientKeySHA256,
	)
}

type protectedBrokerHashV1 string

func parseProtectedBrokerHashV1(value string) (protectedBrokerHashV1, error) {
	fingerprint, err := ParseFingerprint(value)
	if err != nil {
		return "", errors.New("invalid protected broker hash")
	}
	return protectedBrokerHashV1(fingerprint.String()), nil
}

func (v protectedBrokerHashV1) valid() bool {
	_, err := parseProtectedBrokerHashV1(string(v))
	return err == nil
}

func (v protectedBrokerHashV1) String() string {
	return string(v)
}

func (v protectedBrokerHashV1) fingerprint() Fingerprint {
	return Fingerprint{value: string(v)}
}

func (v protectedBrokerHashV1) MarshalJSON() ([]byte, error) {
	if !v.valid() {
		return nil, errors.New("invalid protected broker hash")
	}
	return json.Marshal(string(v))
}

func (v *protectedBrokerHashV1) UnmarshalJSON(encoded []byte) error {
	if v == nil || bytes.Equal(bytes.TrimSpace(encoded), []byte("null")) {
		return errors.New("invalid protected broker hash")
	}
	var value string
	if err := json.Unmarshal(encoded, &value); err != nil {
		return errors.New("invalid protected broker hash")
	}
	parsed, err := parseProtectedBrokerHashV1(value)
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}

type protectedBrokerEpochV1 uint64

func (v protectedBrokerEpochV1) String() string {
	return strconv.FormatUint(uint64(v), 10)
}

func (v protectedBrokerEpochV1) MarshalJSON() ([]byte, error) {
	if v == 0 {
		return nil, errors.New("invalid protected broker epoch")
	}
	return json.Marshal(uint64(v))
}

func (v *protectedBrokerEpochV1) UnmarshalJSON(encoded []byte) error {
	if v == nil || bytes.Equal(bytes.TrimSpace(encoded), []byte("null")) {
		return errors.New("invalid protected broker epoch")
	}
	var value uint64
	if err := json.Unmarshal(encoded, &value); err != nil || value == 0 {
		return errors.New("invalid protected broker epoch")
	}
	*v = protectedBrokerEpochV1(value)
	return nil
}

type protectedBrokerNonceV1 [protectedBrokerNonceBytesV1]byte

func randomProtectedBrokerNonceV1() (protectedBrokerNonceV1, error) {
	var value protectedBrokerNonceV1
	if _, err := io.ReadFull(rand.Reader, value[:]); err != nil || !value.valid() {
		return protectedBrokerNonceV1{}, protectedBrokerError(ProtectedBrokerEntropyUnavailableV1)
	}
	return value, nil
}

func parseProtectedBrokerNonceV1(value string) (protectedBrokerNonceV1, error) {
	decoded, ok := decodeProtectedBrokerBase64V1(value, protectedBrokerNonceBytesV1)
	if !ok {
		return protectedBrokerNonceV1{}, protectedBrokerError(ProtectedBrokerInvalidNonceV1)
	}
	var nonce protectedBrokerNonceV1
	copy(nonce[:], decoded)
	if !nonce.valid() {
		return protectedBrokerNonceV1{}, protectedBrokerError(ProtectedBrokerInvalidNonceV1)
	}
	return nonce, nil
}

func (v protectedBrokerNonceV1) encoded() string {
	return base64.RawURLEncoding.EncodeToString(v[:])
}

func (v protectedBrokerNonceV1) valid() bool {
	return v != protectedBrokerNonceV1{}
}

func validProtectedBrokerPrivateKeyV1(value ed25519.PrivateKey) bool {
	if len(value) != ed25519.PrivateKeySize {
		return false
	}
	derived := ed25519.NewKeyFromSeed(value[:ed25519.SeedSize])
	return subtle.ConstantTimeCompare(value, derived) == 1
}

func protectedBrokerRawHashV1(value []byte) protectedBrokerHashV1 {
	digest := sha256.Sum256(value)
	return protectedBrokerHashFromDigestV1(digest)
}

func hashProtectedBrokerBytesV1(domain string, value []byte) protectedBrokerHashV1 {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(domain))
	_, _ = hasher.Write(value)
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return protectedBrokerHashFromDigestV1(digest)
}

func hashProtectedBrokerJSONV1(
	domain string,
	values ...any,
) (protectedBrokerHashV1, error) {
	message, err := protectedBrokerJSONMessageV1(domain, values...)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(message)
	return protectedBrokerHashFromDigestV1(digest), nil
}

func protectedBrokerHashFromDigestV1(digest [sha256.Size]byte) protectedBrokerHashV1 {
	return protectedBrokerHashV1("sha256:" + hex.EncodeToString(digest[:]))
}

func signProtectedBrokerJSONV1(
	domain string,
	key ed25519.PrivateKey,
	values ...any,
) (string, error) {
	if !validProtectedBrokerPrivateKeyV1(key) {
		return "", protectedBrokerError(ProtectedBrokerInvalidFrameV1)
	}
	message, err := protectedBrokerJSONMessageV1(domain, values...)
	if err != nil {
		return "", err
	}
	signature := ed25519.Sign(key, message)
	return protectedBrokerSignatureV1 + base64.RawURLEncoding.EncodeToString(signature), nil
}

func verifyProtectedBrokerJSONV1(
	domain string,
	encodedSignature string,
	key ed25519.PublicKey,
	values ...any,
) error {
	if len(key) != ed25519.PublicKeySize {
		return protectedBrokerError(ProtectedBrokerInvalidSignatureV1)
	}
	if len(encodedSignature) <= len(protectedBrokerSignatureV1) ||
		encodedSignature[:len(protectedBrokerSignatureV1)] != protectedBrokerSignatureV1 {
		return protectedBrokerError(ProtectedBrokerInvalidSignatureV1)
	}
	signature, ok := decodeProtectedBrokerBase64V1(
		encodedSignature[len(protectedBrokerSignatureV1):],
		ed25519.SignatureSize,
	)
	if !ok {
		return protectedBrokerError(ProtectedBrokerInvalidSignatureV1)
	}
	message, err := protectedBrokerJSONMessageV1(domain, values...)
	if err != nil {
		return err
	}
	if !ed25519.Verify(key, message, signature) {
		return protectedBrokerError(ProtectedBrokerInvalidSignatureV1)
	}
	return nil
}

func protectedBrokerJSONMessageV1(domain string, values ...any) ([]byte, error) {
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, protectedBrokerError(ProtectedBrokerInvalidFrameV1)
	}
	message := make([]byte, 0, len(domain)+len(encoded))
	message = append(message, domain...)
	message = append(message, encoded...)
	return message, nil
}

func decodeProtectedBrokerBase64V1(value string, decodedBytes int) ([]byte, bool) {
	if len(value) != base64.RawURLEncoding.EncodedLen(decodedBytes) {
		return nil, false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if !((character >= 'A' && character <= 'Z') ||
			(character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '-' ||
			character == '_') {
			return nil, false
		}
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(decoded) != decodedBytes {
		return nil, false
	}
	if base64.RawURLEncoding.EncodeToString(decoded) != value {
		return nil, false
	}
	return decoded, true
}
