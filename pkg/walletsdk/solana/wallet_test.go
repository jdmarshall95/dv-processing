package solana

import (
	"testing"

	"github.com/dv-net/go-bip39"
	"github.com/stretchr/testify/require"
)

func TestValidateAddress(t *testing.T) {
	t.Parallel()

	require.True(t, ValidateAddress(USDCMintMainnet))
	require.False(t, ValidateAddress(""))
	require.False(t, ValidateAddress("not-a-solana-address"))
	require.False(t, ValidateAddress("0x32Be343B94f860124dC4fEe278FDCBD38C102D88"))
}

func TestWalletPubKeyHashDeterministic(t *testing.T) {
	t.Parallel()

	entropy, err := bip39.NewEntropy(128)
	require.NoError(t, err)
	mnemonic, err := bip39.NewMnemonic(entropy)
	require.NoError(t, err)

	addr0, priv0, err := WalletPubKeyHash(mnemonic, "", 0)
	require.NoError(t, err)
	require.True(t, ValidateAddress(addr0))
	require.Equal(t, addr0, priv0.PublicKey().String())

	addr0Again, _, err := WalletPubKeyHash(mnemonic, "", 0)
	require.NoError(t, err)
	require.Equal(t, addr0, addr0Again)

	addr1, _, err := WalletPubKeyHash(mnemonic, "", 1)
	require.NoError(t, err)
	require.NotEqual(t, addr0, addr1)
}

func TestAddressSecretAndPublic(t *testing.T) {
	t.Parallel()

	entropy, err := bip39.NewEntropy(128)
	require.NoError(t, err)
	mnemonic, err := bip39.NewMnemonic(entropy)
	require.NoError(t, err)

	addr, _, err := WalletPubKeyHash(mnemonic, "pass", 0)
	require.NoError(t, err)

	secret, err := AddressSecret(addr, mnemonic, "pass", 0)
	require.NoError(t, err)
	require.NotEmpty(t, secret)

	pub, err := AddressPublic(addr, mnemonic, "pass", 0)
	require.NoError(t, err)
	require.NotEmpty(t, pub)

	_, err = AddressSecret("11111111111111111111111111111111", mnemonic, "pass", 0)
	require.ErrorIs(t, err, ErrAddressMismatch)
}

func TestNewWalletFromSeed(t *testing.T) {
	t.Parallel()

	entropy, err := bip39.NewEntropy(128)
	require.NoError(t, err)
	mnemonic, err := bip39.NewMnemonic(entropy)
	require.NoError(t, err)

	seed := bip39.NewSeed(mnemonic, "")
	wallet, err := NewWallet(seed)
	require.NoError(t, err)
	require.True(t, ValidateAddress(wallet.DeriveAddress().String()))

	_, err = NewWallet([]byte{1, 2, 3})
	require.Error(t, err)
}

func TestLamportsConversion(t *testing.T) {
	t.Parallel()

	require.Equal(t, "1", LamportsToSOL(1_000_000_000).String())
	require.Equal(t, uint64(1_000_000_000), SOLToLamports(LamportsToSOL(1_000_000_000)))
	require.Equal(t, "1", RawToTokens(1_000_000, USDCDecimals).String())
	require.Equal(t, uint64(1_000_000), TokensToRaw(RawToTokens(1_000_000, USDCDecimals), USDCDecimals))
}
