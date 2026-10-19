package solana

import (
	"encoding/hex"
	"fmt"

	"github.com/dv-net/go-bip39"
	solanago "github.com/gagliardetto/solana-go"
)

// WalletSDK provides HD wallet helpers for Solana (no RPC dependency).
type WalletSDK struct{}

// NewWalletSDK creates a new WalletSDK instance.
func NewWalletSDK() *WalletSDK {
	return &WalletSDK{}
}

// Wallet wraps a Solana keypair.
type Wallet struct {
	PrivateKey solanago.PrivateKey
}

// NewWallet creates a Wallet from a BIP39 seed (64 bytes from NewSeed) or a 32-byte ed25519 seed.
// For a BIP39 seed the master key is derived via SLIP-0010 at m/44'/501'/0'/0'.
func NewWallet(seed []byte) (*Wallet, error) {
	switch len(seed) {
	case 32:
		priv, err := privateKeyFromSeed(seed)
		if err != nil {
			return nil, err
		}
		return &Wallet{PrivateKey: solanago.PrivateKey(priv)}, nil
	case 64:
		// Treat as BIP39 seed → derive account 0
		derived, err := deriveEd25519Hardened(seed, []uint32{44, CoinType, 0, 0})
		if err != nil {
			return nil, fmt.Errorf("derive key: %w", err)
		}
		priv, err := privateKeyFromSeed(derived)
		if err != nil {
			return nil, err
		}
		return &Wallet{PrivateKey: solanago.PrivateKey(priv)}, nil
	default:
		return nil, fmt.Errorf("unsupported seed length %d (expected 32 or 64)", len(seed))
	}
}

// DeriveAddress returns the public key of the wallet.
func (w *Wallet) DeriveAddress() solanago.PublicKey {
	return w.PrivateKey.PublicKey()
}

// WalletPubKeyHash derives a Solana address and private key from a mnemonic
// using path m/44'/501'/{sequence}'/0' (Phantom-compatible).
func WalletPubKeyHash(mnemonic, passphrase string, sequence uint32) (string, solanago.PrivateKey, error) {
	if mnemonic == "" {
		return "", nil, ErrEmptyMnemonic
	}

	seed := bip39.NewSeed(mnemonic, passphrase)

	derived, err := deriveEd25519Hardened(seed, []uint32{44, CoinType, sequence, 0})
	if err != nil {
		return "", nil, fmt.Errorf("derive private key: %w", err)
	}

	priv, err := privateKeyFromSeed(derived)
	if err != nil {
		return "", nil, err
	}

	pk := solanago.PrivateKey(priv)
	return pk.PublicKey().String(), pk, nil
}

// AddressWallet returns the Solana address for the given mnemonic and sequence.
func AddressWallet(mnemonic, passphrase string, sequence uint32) (string, error) {
	address, _, err := WalletPubKeyHash(mnemonic, passphrase, sequence)
	return address, err
}

// AddressSecret returns the base58-encoded private key for the address.
func AddressSecret(address, mnemonic, passphrase string, sequence uint32) (string, error) {
	wAddress, private, err := WalletPubKeyHash(mnemonic, passphrase, sequence)
	if err != nil {
		return "", err
	}
	if address != wAddress {
		return "", ErrAddressMismatch
	}
	return private.String(), nil
}

// AddressPublic returns the hex-encoded public key for the address.
func AddressPublic(address, mnemonic, passphrase string, sequence uint32) (string, error) {
	wAddress, private, err := WalletPubKeyHash(mnemonic, passphrase, sequence)
	if err != nil {
		return "", err
	}
	if address != wAddress {
		return "", ErrAddressMismatch
	}
	pub := private.PublicKey()
	return hex.EncodeToString(pub[:]), nil
}

// ValidateAddress validates a Solana address.
func (s WalletSDK) ValidateAddress(address string) bool {
	return ValidateAddress(address)
}
