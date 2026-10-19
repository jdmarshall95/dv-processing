package solana

import (
	solanago "github.com/gagliardetto/solana-go"
)

// ValidateAddress checks whether address is a valid base58-encoded Solana public key.
func ValidateAddress(address string) bool {
	_, err := solanago.PublicKeyFromBase58(address)
	return err == nil
}
