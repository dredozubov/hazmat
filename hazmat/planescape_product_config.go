package hazmat

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"hazmat/configmodel"
	"hazmat/planescapeprovider"
)

const (
	minPlanescapeProductDialTimeoutMS = uint64(1)
	maxPlanescapeProductDialTimeoutMS = uint64(60_000)
	planescapeProductSigningSeedSize  = ed25519.SeedSize
)

// configuredPlanescapeProductEndpoint constructs only the published protected
// TCP endpoint. It does not provide a plan compiler, operation source, local
// transport, resolver, proxy, or fallback.
func configuredPlanescapeProductEndpoint(
	config *configmodel.PlanescapeProviderConfig,
) (planescapeprovider.BoundEndpoint, error) {
	if config == nil {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}

	target, err := netip.ParseAddrPort(config.Endpoint)
	if err != nil {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	if config.DialTimeoutMS < minPlanescapeProductDialTimeoutMS ||
		config.DialTimeoutMS > maxPlanescapeProductDialTimeoutMS {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	timeout := time.Duration(config.DialTimeoutMS) * time.Millisecond
	dialer, err := planescapeprovider.NewProtectedBrokerTCPDialerV1(
		target,
		timeout,
	)
	if err != nil {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}

	brokerPublicKey, err := decodePlanescapeProductPublicKey(
		config.BrokerPublicKeyBase64URL,
	)
	if err != nil {
		return nil, err
	}
	clientPublicKey, err := decodePlanescapeProductPublicKey(
		config.ClientPublicKeyBase64URL,
	)
	if err != nil {
		return nil, err
	}

	backend, err := planescapeprovider.NewProtectedBrokerBackendIdentityV1(
		planescapeprovider.ProtectedBrokerBackendIdentityInputV1{
			BackendInstanceSHA256:      config.Backend.BackendInstanceSHA256,
			ExecutableSHA256:           config.Backend.ExecutableSHA256,
			ExecutionEnvironmentSHA256: config.Backend.ExecutionEnvironmentSHA256,
			ProfileSHA256:              config.Backend.ProfileSHA256,
			BrokerEpoch:                config.Backend.BrokerEpoch,
		},
		brokerPublicKey,
	)
	if err != nil {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	expectedIdentity, err := planescapeprovider.ParseFingerprint(
		config.Backend.IdentitySHA256,
	)
	if err != nil {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	if backend.IdentitySHA256() != expectedIdentity {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}

	clientSeed, err := readPlanescapeProductSigningSeed(
		config.ClientSigningSeedFile,
	)
	if err != nil {
		return nil, err
	}
	defer clear(clientSeed)
	clientPrivateKey := ed25519.NewKeyFromSeed(clientSeed)
	defer clear(clientPrivateKey)
	derivedClientPublicKey := ed25519.PublicKey(
		clientPrivateKey[ed25519.SeedSize:],
	)
	if !bytes.Equal(derivedClientPublicKey, clientPublicKey) {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}

	client, err := planescapeprovider.NewProtectedBrokerClientV1(
		backend,
		brokerPublicKey,
		clientPrivateKey,
	)
	if err != nil {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}
	endpoint, err := planescapeprovider.NewProtectedBrokerEndpointV1(
		planescapeprovider.ProtectedBrokerEndpointConfigV1{
			Dialer: dialer,
			Client: client,
		},
	)
	if err != nil {
		return nil, mapPlanescapeProductError(err)
	}
	if endpoint.BackendBinding().IdentitySHA256() != expectedIdentity ||
		endpoint.BackendBinding().ProviderEpoch().Uint64() !=
			config.Backend.BrokerEpoch {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}
	return endpoint, nil
}

func decodePlanescapeProductPublicKey(value string) (ed25519.PublicKey, error) {
	if len(value) != base64.RawURLEncoding.EncodedLen(ed25519.PublicKeySize) {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil ||
		len(decoded) != ed25519.PublicKeySize ||
		base64.RawURLEncoding.EncodeToString(decoded) != value {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	return ed25519.PublicKey(decoded), nil
}

func readPlanescapeProductSigningSeed(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}
	if !validPlanescapeProductSigningSeedFile(pathInfo) {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}
	openedInfo, statErr := file.Stat()
	if statErr != nil ||
		!os.SameFile(pathInfo, openedInfo) ||
		!validPlanescapeProductSigningSeedFile(openedInfo) {
		_ = file.Close()
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorConflict,
			planescapeProductProviderFailure,
		)
	}
	seed, readErr := io.ReadAll(
		io.LimitReader(file, planescapeProductSigningSeedSize+1),
	)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		clear(seed)
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorUnavailable,
			planescapeProductProviderFailure,
		)
	}
	if len(seed) != planescapeProductSigningSeedSize ||
		bytes.Equal(seed, make([]byte, planescapeProductSigningSeedSize)) {
		clear(seed)
		return nil, newPlanescapeProductError(
			planescapeprovider.ErrorInvalid,
			planescapeProductProviderFailure,
		)
	}
	return seed, nil
}

func validPlanescapeProductSigningSeedFile(info os.FileInfo) bool {
	if info == nil ||
		!info.Mode().IsRegular() ||
		info.Mode().Perm() != 0o600 ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && stat.Nlink == 1
}
