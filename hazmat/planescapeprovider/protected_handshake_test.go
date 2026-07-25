package planescapeprovider

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	protectedBrokerInteropBaseCommit       = "dcf37f16e8c8c0e8bb2729f52eeeaeb7a011a3e9"
	protectedBrokerInteropCorrectionCommit = "241a1ab7972172d2e2948461d0a54fbcc4366540"
)

type protectedBrokerInteropFixtureV1 struct {
	PlanescapeBaseCommit       string `json:"planescape_base_commit"`
	PlanescapeCorrectionCommit string `json:"planescape_correction_commit"`
	BrokerSeedBase64URL        string `json:"broker_seed_base64url"`
	ClientSeedBase64URL        string `json:"client_seed_base64url"`
	ClientNonceBase64URL       string `json:"client_nonce_base64url"`
	ServerNonceBase64URL       string `json:"server_nonce_base64url"`
	BackendInstanceSHA256      string `json:"backend_instance_sha256"`
	ExecutableSHA256           string `json:"executable_sha256"`
	ExecutionEnvironmentSHA256 string `json:"execution_environment_sha256"`
	ProfileSHA256              string `json:"profile_sha256"`
	BrokerEpoch                uint64 `json:"broker_epoch"`
	BrokerPublicKeyBase64URL   string `json:"broker_public_key_base64url"`
	ClientPublicKeyBase64URL   string `json:"client_public_key_base64url"`
	AttestorPublicKeySHA256    string `json:"attestor_public_key_sha256"`
	BackendIdentitySHA256      string `json:"backend_identity_sha256"`
	ClientKeySHA256            string `json:"client_key_sha256"`
	ClientAuthoritySHA256      string `json:"client_authority_sha256"`
	TransportSessionSHA256     string `json:"transport_session_sha256"`
	ClientHelloJSON            string `json:"client_hello_json"`
	ServerChallengeJSON        string `json:"server_challenge_json"`
	ClientFinishJSON           string `json:"client_finish_json"`
	ServerAcceptedJSON         string `json:"server_accepted_json"`
}

func TestProtectedBrokerHandshakeMatchesDeterministicInteropFixture(t *testing.T) {
	fixture := loadProtectedBrokerInteropFixture(t)
	brokerKey, clientKey, identity, client, clientNonce := protectedBrokerFixtureClient(t, fixture)
	brokerPublicKey := brokerKey.Public().(ed25519.PublicKey)
	clientPublicKey := clientKey.Public().(ed25519.PublicKey)

	if got := base64.RawURLEncoding.EncodeToString(brokerPublicKey); got != fixture.BrokerPublicKeyBase64URL {
		t.Fatalf("broker public key = %q, want fixture", got)
	}
	if got := base64.RawURLEncoding.EncodeToString(clientPublicKey); got != fixture.ClientPublicKeyBase64URL {
		t.Fatalf("client public key = %q, want fixture", got)
	}
	if got := identity.AttestorPublicKeySHA256().String(); got != fixture.AttestorPublicKeySHA256 {
		t.Fatalf("attestor key hash = %q, want %q", got, fixture.AttestorPublicKeySHA256)
	}
	if got := identity.IdentitySHA256().String(); got != fixture.BackendIdentitySHA256 {
		t.Fatalf("backend identity = %q, want %q", got, fixture.BackendIdentitySHA256)
	}

	hello := decodeProtectedBrokerFixture[protectedBrokerClientHelloWireV1](t, fixture.ClientHelloJSON)
	challenge := decodeProtectedBrokerFixture[protectedBrokerServerChallengeWireV1](t, fixture.ServerChallengeJSON)
	finish := decodeProtectedBrokerFixture[protectedBrokerClientFinishWireV1](t, fixture.ClientFinishJSON)
	accepted := decodeProtectedBrokerFixture[protectedBrokerServerAcceptedWireV1](t, fixture.ServerAcceptedJSON)

	derivedHello, err := newProtectedBrokerClientHelloV1(client.clientKeySHA256, clientNonce)
	if err != nil {
		t.Fatal(err)
	}
	requireProtectedBrokerWireJSON(t, derivedHello, fixture.ClientHelloJSON)
	if derivedHello != hello {
		t.Fatalf("derived hello differs from fixture: %+v", derivedHello)
	}

	clientAuthoritySHA256, err := protectedBrokerClientAuthorityHashV1(client.clientKeySHA256, identity)
	if err != nil {
		t.Fatal(err)
	}
	if got := clientAuthoritySHA256.String(); got != fixture.ClientAuthoritySHA256 {
		t.Fatalf("client authority = %q, want %q", got, fixture.ClientAuthoritySHA256)
	}
	if _, err := challenge.validate(
		hello,
		identity,
		clientAuthoritySHA256,
		brokerPublicKey,
	); err != nil {
		t.Fatal(err)
	}

	transportSessionSHA256, err := protectedBrokerTransportSessionHashV1(
		hello,
		challenge,
		clientAuthoritySHA256,
		client.clientKeySHA256,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := transportSessionSHA256.String(); got != fixture.TransportSessionSHA256 {
		t.Fatalf("transport session = %q, want %q", got, fixture.TransportSessionSHA256)
	}

	derivedFinish, err := newProtectedBrokerClientFinishV1(
		challenge,
		client.clientKeySHA256,
		clientAuthoritySHA256,
		transportSessionSHA256,
		clientKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	requireProtectedBrokerWireJSON(t, derivedFinish, fixture.ClientFinishJSON)
	if derivedFinish != finish {
		t.Fatalf("derived finish differs from fixture: %+v", derivedFinish)
	}
	if err := verifyProtectedBrokerJSONV1(
		protectedBrokerClientFinishSigDomainV1,
		finish.Signature,
		clientPublicKey,
		protectedBrokerClientFinishSchemaV1,
		finish.ChallengeSHA256,
		finish.ClientKeySHA256,
		finish.ClientAuthoritySHA256,
		finish.TransportSessionSHA256,
		finish.FinishSHA256,
	); err != nil {
		t.Fatal(err)
	}
	if err := accepted.validate(
		finish,
		clientAuthoritySHA256,
		transportSessionSHA256,
		brokerPublicKey,
	); err != nil {
		t.Fatal(err)
	}

	session, err := runProtectedBrokerStaticHandshake(
		t,
		client,
		clientNonce,
		fixture.ClientHelloJSON,
		fixture.ServerChallengeJSON,
		fixture.ClientFinishJSON,
		fixture.ServerAcceptedJSON,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := session.BackendIdentitySHA256().String(); got != fixture.BackendIdentitySHA256 {
		t.Fatalf("session backend identity = %q, want %q", got, fixture.BackendIdentitySHA256)
	}
	if got := session.BrokerEpoch().Uint64(); got != fixture.BrokerEpoch {
		t.Fatalf("session epoch = %d, want %d", got, fixture.BrokerEpoch)
	}
	if got := session.ProfileSHA256().String(); got != fixture.ProfileSHA256 {
		t.Fatalf("session profile = %q, want %q", got, fixture.ProfileSHA256)
	}
	if got := session.ClientKeySHA256().String(); got != fixture.ClientKeySHA256 {
		t.Fatalf("session client key = %q, want %q", got, fixture.ClientKeySHA256)
	}
	if got := session.ClientAuthoritySHA256().String(); got != fixture.ClientAuthoritySHA256 {
		t.Fatalf("session client authority = %q, want %q", got, fixture.ClientAuthoritySHA256)
	}
	if got := session.TransportSessionSHA256().String(); got != fixture.TransportSessionSHA256 {
		t.Fatalf("session transport identity = %q, want %q", got, fixture.TransportSessionSHA256)
	}
}

func TestProtectedBrokerReconnectKeepsAuthorityAndRefreshesTransport(t *testing.T) {
	fixture := loadProtectedBrokerInteropFixture(t)
	brokerKey, _, identity, client, firstClientNonce := protectedBrokerFixtureClient(t, fixture)
	firstHello, err := newProtectedBrokerClientHelloV1(client.clientKeySHA256, firstClientNonce)
	if err != nil {
		t.Fatal(err)
	}
	firstChallenge := decodeProtectedBrokerFixture[protectedBrokerServerChallengeWireV1](
		t,
		fixture.ServerChallengeJSON,
	)
	stableAuthority, err := protectedBrokerClientAuthorityHashV1(client.clientKeySHA256, identity)
	if err != nil {
		t.Fatal(err)
	}
	firstTransport, err := protectedBrokerTransportSessionHashV1(
		firstHello,
		firstChallenge,
		stableAuthority,
		client.clientKeySHA256,
	)
	if err != nil {
		t.Fatal(err)
	}

	secondClientNonce := repeatedProtectedBrokerNonce(3)
	secondServerNonce := repeatedProtectedBrokerNonce(4)
	secondHello, err := newProtectedBrokerClientHelloV1(client.clientKeySHA256, secondClientNonce)
	if err != nil {
		t.Fatal(err)
	}
	secondAuthority, err := protectedBrokerClientAuthorityHashV1(client.clientKeySHA256, identity)
	if err != nil {
		t.Fatal(err)
	}
	secondChallenge, err := newProtectedBrokerServerChallengeForTest(
		secondHello,
		secondServerNonce,
		identity,
		secondAuthority,
		brokerKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	secondTransport, err := protectedBrokerTransportSessionHashV1(
		secondHello,
		secondChallenge,
		secondAuthority,
		client.clientKeySHA256,
	)
	if err != nil {
		t.Fatal(err)
	}

	if stableAuthority != secondAuthority {
		t.Fatalf("stable authority changed across reconnect: %s != %s", stableAuthority, secondAuthority)
	}
	if firstTransport == secondTransport {
		t.Fatalf("transport identity did not refresh: %s", firstTransport)
	}
}

func TestProtectedBrokerHandshakeRejectsMalformedUnknownAndOversizedFrames(t *testing.T) {
	fixture := loadProtectedBrokerInteropFixture(t)
	_, _, _, client, clientNonce := protectedBrokerFixtureClient(t, fixture)
	unknown := strings.TrimSuffix(fixture.ServerChallengeJSON, "}") + `,"unexpected":true}`

	cases := map[string]struct {
		response []byte
		want     ProtectedBrokerTransportErrorClassV1
	}{
		"malformed": {
			response: protectedFrame([]byte(`{"schema":`)),
			want:     ProtectedBrokerInvalidFrameV1,
		},
		"unknown field": {
			response: protectedFrame([]byte(unknown)),
			want:     ProtectedBrokerInvalidFrameV1,
		},
		"oversized": {
			response: protectedFrameHeader(MaxProtectedBrokerFrameBytesV1 + 1),
			want:     ProtectedBrokerFrameTooLargeV1,
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			err := runProtectedBrokerChallengeResponse(t, client, clientNonce, test.response)
			requireProtectedBrokerErrorClass(t, err, test.want)
		})
	}
}

func TestProtectedBrokerHandshakeRejectsWrongPeerAndReplay(t *testing.T) {
	fixture := loadProtectedBrokerInteropFixture(t)
	brokerKey, _, identity, client, clientNonce := protectedBrokerFixtureClient(t, fixture)

	foreignBrokerKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{13}, ed25519.SeedSize))
	_, err := NewProtectedBrokerClientV1(
		identity,
		foreignBrokerKey.Public().(ed25519.PublicKey),
		client.clientKey,
	)
	requireProtectedBrokerErrorClass(t, err, ProtectedBrokerWrongBrokerKeyV1)

	challenge := decodeProtectedBrokerFixture[protectedBrokerServerChallengeWireV1](
		t,
		fixture.ServerChallengeJSON,
	)
	challenge.Signature = mutateProtectedBrokerSignature(challenge.Signature)
	invalidSignature, err := json.Marshal(challenge)
	if err != nil {
		t.Fatal(err)
	}
	err = runProtectedBrokerChallengeResponse(
		t,
		client,
		clientNonce,
		protectedFrame(invalidSignature),
	)
	requireProtectedBrokerErrorClass(t, err, ProtectedBrokerInvalidSignatureV1)

	err = runProtectedBrokerChallengeResponse(
		t,
		client,
		repeatedProtectedBrokerNonce(3),
		protectedFrame([]byte(fixture.ServerChallengeJSON)),
	)
	requireProtectedBrokerErrorClass(t, err, ProtectedBrokerReplayBindingMismatchV1)

	hello := decodeProtectedBrokerFixture[protectedBrokerClientHelloWireV1](t, fixture.ClientHelloJSON)
	alternateIdentity, err := NewProtectedBrokerBackendIdentityV1(
		ProtectedBrokerBackendIdentityInputV1{
			BackendInstanceSHA256:      fixture.BackendInstanceSHA256,
			ExecutableSHA256:           fixture.ExecutableSHA256,
			ExecutionEnvironmentSHA256: fixture.ExecutionEnvironmentSHA256,
			ProfileSHA256:              repeatedProtectedBrokerTestHash(5),
			BrokerEpoch:                fixture.BrokerEpoch,
		},
		brokerKey.Public().(ed25519.PublicKey),
	)
	if err != nil {
		t.Fatal(err)
	}
	alternateAuthority, err := protectedBrokerClientAuthorityHashV1(
		client.clientKeySHA256,
		alternateIdentity,
	)
	if err != nil {
		t.Fatal(err)
	}
	wrongIdentityChallenge, err := newProtectedBrokerServerChallengeForTest(
		hello,
		repeatedProtectedBrokerNonce(2),
		alternateIdentity,
		alternateAuthority,
		brokerKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	wrongIdentityPayload, err := json.Marshal(wrongIdentityChallenge)
	if err != nil {
		t.Fatal(err)
	}
	err = runProtectedBrokerChallengeResponse(
		t,
		client,
		clientNonce,
		protectedFrame(wrongIdentityPayload),
	)
	requireProtectedBrokerErrorClass(t, err, ProtectedBrokerWrongBrokerIdentityV1)

	staleIdentity, err := NewProtectedBrokerBackendIdentityV1(
		ProtectedBrokerBackendIdentityInputV1{
			BackendInstanceSHA256:      fixture.BackendInstanceSHA256,
			ExecutableSHA256:           fixture.ExecutableSHA256,
			ExecutionEnvironmentSHA256: fixture.ExecutionEnvironmentSHA256,
			ProfileSHA256:              fixture.ProfileSHA256,
			BrokerEpoch:                fixture.BrokerEpoch + 1,
		},
		brokerKey.Public().(ed25519.PublicKey),
	)
	if err != nil {
		t.Fatal(err)
	}
	staleAuthority, err := protectedBrokerClientAuthorityHashV1(client.clientKeySHA256, staleIdentity)
	if err != nil {
		t.Fatal(err)
	}
	staleChallenge, err := newProtectedBrokerServerChallengeForTest(
		hello,
		repeatedProtectedBrokerNonce(2),
		staleIdentity,
		staleAuthority,
		brokerKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	stalePayload, err := json.Marshal(staleChallenge)
	if err != nil {
		t.Fatal(err)
	}
	err = runProtectedBrokerChallengeResponse(
		t,
		client,
		clientNonce,
		protectedFrame(stalePayload),
	)
	requireProtectedBrokerErrorClass(t, err, ProtectedBrokerStaleBrokerEpochV1)
}

func TestProtectedBrokerHandshakeRejectsReplayedAcceptance(t *testing.T) {
	fixture := loadProtectedBrokerInteropFixture(t)
	brokerKey, _, _, client, clientNonce := protectedBrokerFixtureClient(t, fixture)
	finish := decodeProtectedBrokerFixture[protectedBrokerClientFinishWireV1](t, fixture.ClientFinishJSON)
	clientAuthoritySHA256, err := parseProtectedBrokerHashV1(fixture.ClientAuthoritySHA256)
	if err != nil {
		t.Fatal(err)
	}
	wrongTransportSHA256, err := parseProtectedBrokerHashV1(repeatedProtectedBrokerTestHash(9))
	if err != nil {
		t.Fatal(err)
	}
	replayedAccepted, err := newProtectedBrokerServerAcceptedForTest(
		finish,
		clientAuthoritySHA256,
		wrongTransportSHA256,
		brokerKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	replayedAcceptedJSON, err := json.Marshal(replayedAccepted)
	if err != nil {
		t.Fatal(err)
	}

	_, err = runProtectedBrokerStaticHandshake(
		t,
		client,
		clientNonce,
		fixture.ClientHelloJSON,
		fixture.ServerChallengeJSON,
		fixture.ClientFinishJSON,
		string(replayedAcceptedJSON),
	)
	requireProtectedBrokerErrorClass(t, err, ProtectedBrokerReplayBindingMismatchV1)
}

func TestProtectedBrokerHandshakeDiagnosticsRedactKeysNoncesAndSignatures(t *testing.T) {
	fixture := loadProtectedBrokerInteropFixture(t)
	_, _, _, client, clientNonce := protectedBrokerFixtureClient(t, fixture)
	session, err := runProtectedBrokerStaticHandshake(
		t,
		client,
		clientNonce,
		fixture.ClientHelloJSON,
		fixture.ServerChallengeJSON,
		fixture.ClientFinishJSON,
		fixture.ServerAcceptedJSON,
	)
	if err != nil {
		t.Fatal(err)
	}

	challenge := decodeProtectedBrokerFixture[protectedBrokerServerChallengeWireV1](
		t,
		fixture.ServerChallengeJSON,
	)
	formatted := []string{
		fmt.Sprintf("%+v", client),
		fmt.Sprintf("%#v", client),
		fmt.Sprintf("%+v", session),
		fmt.Sprintf("%#v", session),
		protectedBrokerError(ProtectedBrokerInvalidSignatureV1).Error(),
	}
	secrets := []string{
		fixture.BrokerSeedBase64URL,
		fixture.ClientSeedBase64URL,
		fixture.ClientNonceBase64URL,
		fixture.ServerNonceBase64URL,
		challenge.Signature,
	}
	for _, output := range formatted {
		for _, secret := range secrets {
			if strings.Contains(output, secret) {
				t.Fatalf("formatted diagnostic leaked protected handshake material: %q", output)
			}
		}
	}
}

func loadProtectedBrokerInteropFixture(t *testing.T) protectedBrokerInteropFixtureV1 {
	t.Helper()
	encoded, err := os.ReadFile("testdata/protected_broker.v1/interop.json")
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var fixture protectedBrokerInteropFixtureV1
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("interop fixture has trailing JSON: %v", err)
	}
	if fixture.PlanescapeBaseCommit != protectedBrokerInteropBaseCommit {
		t.Fatalf("fixture base commit = %q, want %q", fixture.PlanescapeBaseCommit, protectedBrokerInteropBaseCommit)
	}
	if fixture.PlanescapeCorrectionCommit != protectedBrokerInteropCorrectionCommit {
		t.Fatalf(
			"fixture correction commit = %q, want %q",
			fixture.PlanescapeCorrectionCommit,
			protectedBrokerInteropCorrectionCommit,
		)
	}
	return fixture
}

func protectedBrokerFixtureClient(
	t *testing.T,
	fixture protectedBrokerInteropFixtureV1,
) (
	ed25519.PrivateKey,
	ed25519.PrivateKey,
	ProtectedBrokerBackendIdentityV1,
	*ProtectedBrokerClientV1,
	protectedBrokerNonceV1,
) {
	t.Helper()
	brokerSeed := decodeProtectedBrokerFixtureBytes(t, fixture.BrokerSeedBase64URL, ed25519.SeedSize)
	clientSeed := decodeProtectedBrokerFixtureBytes(t, fixture.ClientSeedBase64URL, ed25519.SeedSize)
	brokerKey := ed25519.NewKeyFromSeed(brokerSeed)
	clientKey := ed25519.NewKeyFromSeed(clientSeed)
	identity, err := NewProtectedBrokerBackendIdentityV1(
		ProtectedBrokerBackendIdentityInputV1{
			BackendInstanceSHA256:      fixture.BackendInstanceSHA256,
			ExecutableSHA256:           fixture.ExecutableSHA256,
			ExecutionEnvironmentSHA256: fixture.ExecutionEnvironmentSHA256,
			ProfileSHA256:              fixture.ProfileSHA256,
			BrokerEpoch:                fixture.BrokerEpoch,
		},
		brokerKey.Public().(ed25519.PublicKey),
	)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewProtectedBrokerClientV1(
		identity,
		brokerKey.Public().(ed25519.PublicKey),
		clientKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	clientNonce, err := parseProtectedBrokerNonceV1(fixture.ClientNonceBase64URL)
	if err != nil {
		t.Fatal(err)
	}
	return brokerKey, clientKey, identity, client, clientNonce
}

func decodeProtectedBrokerFixtureBytes(t *testing.T, encoded string, size int) []byte {
	t.Helper()
	value, ok := decodeProtectedBrokerBase64V1(encoded, size)
	if !ok {
		t.Fatal("fixture contains invalid base64url")
	}
	return value
}

func decodeProtectedBrokerFixture[T any](t *testing.T, payload string) T {
	t.Helper()
	var value T
	if err := (ProtectedBrokerFrameCodecV1{}).ReadJSONFrame(
		bytes.NewReader(protectedFrame([]byte(payload))),
		&value,
	); err != nil {
		t.Fatal(err)
	}
	return value
}

func requireProtectedBrokerWireJSON(t *testing.T, value any, want string) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != want {
		t.Fatalf("wire JSON = %s, want %s", encoded, want)
	}
}

func runProtectedBrokerStaticHandshake(
	t *testing.T,
	client *ProtectedBrokerClientV1,
	clientNonce protectedBrokerNonceV1,
	wantHello string,
	challenge string,
	wantFinish string,
	accepted string,
) (AuthenticatedBrokerClientSessionV1, error) {
	t.Helper()
	clientStream, serverStream := protectedBrokerStreamPair(t)
	peerError := make(chan error, 1)
	go func() {
		defer serverStream.Close()
		hello, err := readProtectedBrokerRawFrame(serverStream)
		if err != nil {
			peerError <- err
			return
		}
		if string(hello) != wantHello {
			peerError <- fmt.Errorf("client hello differs from fixture")
			return
		}
		if err := writeProtectedBrokerRawFrame(serverStream, []byte(challenge)); err != nil {
			peerError <- err
			return
		}
		finish, err := readProtectedBrokerRawFrame(serverStream)
		if err != nil {
			peerError <- err
			return
		}
		if string(finish) != wantFinish {
			peerError <- fmt.Errorf("client finish differs from fixture")
			return
		}
		peerError <- writeProtectedBrokerRawFrame(serverStream, []byte(accepted))
	}()

	session, authenticateErr := client.authenticateWithNonce(clientStream, clientNonce)
	_ = clientStream.Close()
	if err := <-peerError; err != nil {
		t.Fatal(err)
	}
	return session, authenticateErr
}

func runProtectedBrokerChallengeResponse(
	t *testing.T,
	client *ProtectedBrokerClientV1,
	clientNonce protectedBrokerNonceV1,
	response []byte,
) error {
	t.Helper()
	clientStream, serverStream := protectedBrokerStreamPair(t)
	peerError := make(chan error, 1)
	go func() {
		defer serverStream.Close()
		if _, err := readProtectedBrokerRawFrame(serverStream); err != nil {
			peerError <- err
			return
		}
		peerError <- writeProtectedBrokerBytes(serverStream, response)
	}()

	_, authenticateErr := client.authenticateWithNonce(clientStream, clientNonce)
	_ = clientStream.Close()
	if err := <-peerError; err != nil {
		t.Fatal(err)
	}
	return authenticateErr
}

func protectedBrokerStreamPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	client, server := net.Pipe()
	deadline := time.Now().Add(5 * time.Second)
	if err := client.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if err := server.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	return client, server
}

func readProtectedBrokerRawFrame(reader io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 || length > MaxProtectedBrokerFrameBytesV1 {
		return nil, fmt.Errorf("invalid raw protected broker frame length")
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeProtectedBrokerRawFrame(writer io.Writer, payload []byte) error {
	return writeProtectedBrokerBytes(writer, protectedFrame(payload))
}

func repeatedProtectedBrokerNonce(value byte) protectedBrokerNonceV1 {
	var nonce protectedBrokerNonceV1
	for index := range nonce {
		nonce[index] = value
	}
	return nonce
}

func repeatedProtectedBrokerTestHash(value byte) string {
	return "sha256:" + strings.Repeat(fmt.Sprintf("%02x", value), 32)
}

func mutateProtectedBrokerSignature(value string) string {
	if strings.HasSuffix(value, "A") {
		return strings.TrimSuffix(value, "A") + "Q"
	}
	return value[:len(value)-1] + "A"
}

func newProtectedBrokerServerChallengeForTest(
	hello protectedBrokerClientHelloWireV1,
	serverNonce protectedBrokerNonceV1,
	backend ProtectedBrokerBackendIdentityV1,
	clientAuthoritySHA256 protectedBrokerHashV1,
	brokerKey ed25519.PrivateKey,
) (protectedBrokerServerChallengeWireV1, error) {
	encodedNonce := serverNonce.encoded()
	challengeSHA256, err := hashProtectedBrokerJSONV1(
		protectedBrokerServerChallengeHashDomainV1,
		protectedBrokerServerChallengeSchemaV1,
		hello.HelloSHA256,
		encodedNonce,
		backend.identitySHA256,
		backend.brokerEpoch,
		backend.profileSHA256,
		clientAuthoritySHA256,
	)
	if err != nil {
		return protectedBrokerServerChallengeWireV1{}, err
	}
	signature, err := signProtectedBrokerJSONV1(
		protectedBrokerServerChallengeSigDomainV1,
		brokerKey,
		protectedBrokerServerChallengeSchemaV1,
		hello.HelloSHA256,
		encodedNonce,
		backend.identitySHA256,
		backend.brokerEpoch,
		backend.profileSHA256,
		clientAuthoritySHA256,
		challengeSHA256,
	)
	if err != nil {
		return protectedBrokerServerChallengeWireV1{}, err
	}
	return protectedBrokerServerChallengeWireV1{
		Schema:                protectedBrokerServerChallengeSchemaV1,
		HelloSHA256:           hello.HelloSHA256,
		ServerNonce:           encodedNonce,
		BackendIdentitySHA256: backend.identitySHA256,
		BrokerEpoch:           backend.brokerEpoch,
		ProfileSHA256:         backend.profileSHA256,
		ClientAuthoritySHA256: clientAuthoritySHA256,
		ChallengeSHA256:       challengeSHA256,
		Signature:             signature,
	}, nil
}

func newProtectedBrokerServerAcceptedForTest(
	finish protectedBrokerClientFinishWireV1,
	clientAuthoritySHA256 protectedBrokerHashV1,
	transportSessionSHA256 protectedBrokerHashV1,
	brokerKey ed25519.PrivateKey,
) (protectedBrokerServerAcceptedWireV1, error) {
	acceptedSHA256, err := hashProtectedBrokerJSONV1(
		protectedBrokerServerAcceptedHashDomainV1,
		protectedBrokerServerAcceptedSchemaV1,
		finish.FinishSHA256,
		clientAuthoritySHA256,
		transportSessionSHA256,
	)
	if err != nil {
		return protectedBrokerServerAcceptedWireV1{}, err
	}
	signature, err := signProtectedBrokerJSONV1(
		protectedBrokerServerAcceptedSigDomainV1,
		brokerKey,
		protectedBrokerServerAcceptedSchemaV1,
		finish.FinishSHA256,
		clientAuthoritySHA256,
		transportSessionSHA256,
		acceptedSHA256,
	)
	if err != nil {
		return protectedBrokerServerAcceptedWireV1{}, err
	}
	return protectedBrokerServerAcceptedWireV1{
		Schema:                 protectedBrokerServerAcceptedSchemaV1,
		FinishSHA256:           finish.FinishSHA256,
		ClientAuthoritySHA256:  clientAuthoritySHA256,
		TransportSessionSHA256: transportSessionSHA256,
		AcceptedSHA256:         acceptedSHA256,
		Signature:              signature,
	}, nil
}
