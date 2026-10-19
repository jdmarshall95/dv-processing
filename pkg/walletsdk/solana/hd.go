package solana

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
)

const ed25519SeedKey = "ed25519 seed"

// deriveEd25519Hardened derives a private key along a fully hardened SLIP-0010 path.
// pathIndexes are already hardened (i.e. without the 0x80000000 bit set in the values themselves);
// the hardened bit is applied inside this function.
func deriveEd25519Hardened(seed []byte, pathIndexes []uint32) ([]byte, error) {
	if len(seed) == 0 {
		return nil, fmt.Errorf("empty seed")
	}

	key, chainCode := slip10MasterKey(seed)

	for _, index := range pathIndexes {
		var err error
		key, chainCode, err = slip10DeriveHardened(key, chainCode, index)
		if err != nil {
			return nil, err
		}
	}

	return key, nil
}

func slip10MasterKey(seed []byte) (key, chainCode []byte) {
	mac := hmac.New(sha512.New, []byte(ed25519SeedKey))
	_, _ = mac.Write(seed)
	sum := mac.Sum(nil)
	return sum[:32], sum[32:]
}

func slip10DeriveHardened(key, chainCode []byte, index uint32) ([]byte, []byte, error) {
	if len(key) != 32 || len(chainCode) != 32 {
		return nil, nil, fmt.Errorf("invalid key or chain code length")
	}

	hardened := index | 0x80000000

	data := make([]byte, 1+32+4)
	data[0] = 0x00
	copy(data[1:33], key)
	binary.BigEndian.PutUint32(data[33:], hardened)

	mac := hmac.New(sha512.New, chainCode)
	_, _ = mac.Write(data)
	sum := mac.Sum(nil)

	return sum[:32], sum[32:], nil
}

// privateKeyFromSeed builds a 64-byte ed25519 private key from a 32-byte seed.
func privateKeyFromSeed(seed []byte) (ed25519.PrivateKey, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("invalid seed size: expected %d, got %d", ed25519.SeedSize, len(seed))
	}
	return ed25519.NewKeyFromSeed(seed), nil
}
