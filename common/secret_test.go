package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncryptSecretRoundTrip(t *testing.T) {
	originalCryptoSecret := CryptoSecret
	CryptoSecret = "upstream-panel-test-secret"
	t.Cleanup(func() { CryptoSecret = originalCryptoSecret })

	ciphertext, err := EncryptSecret("upstream-channel-password", "correct horse battery staple")
	require.NoError(t, err)
	require.NotEmpty(t, ciphertext)
	require.NotContains(t, ciphertext, "correct horse battery staple")

	plaintext, err := DecryptSecret("upstream-channel-password", ciphertext)
	require.NoError(t, err)
	require.Equal(t, "correct horse battery staple", plaintext)
}

func TestDecryptSecretRejectsDifferentPurpose(t *testing.T) {
	originalCryptoSecret := CryptoSecret
	CryptoSecret = "upstream-panel-test-secret"
	t.Cleanup(func() { CryptoSecret = originalCryptoSecret })

	ciphertext, err := EncryptSecret("upstream-channel-password", "secret")
	require.NoError(t, err)

	_, err = DecryptSecret("different-purpose", ciphertext)
	require.Error(t, err)
}
