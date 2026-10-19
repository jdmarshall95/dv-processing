package solana

import (
	"context"
	"fmt"

	solanago "github.com/gagliardetto/solana-go"
	"github.com/shopspring/decimal"
)

// GetBalance returns the native SOL balance of address in lamports.
func (s *Solana) GetBalance(ctx context.Context, address solanago.PublicKey) (uint64, error) {
	out, err := s.node.GetBalance(ctx, address, s.config.Commitment)
	if err != nil {
		return 0, fmt.Errorf("%w: get balance: %w", ErrRetryable, err)
	}
	return out.Value, nil
}

// GetBalanceSOL returns the native SOL balance as a decimal (in SOL).
func (s *Solana) GetBalanceSOL(ctx context.Context, address string) (decimal.Decimal, error) {
	pk, err := solanago.PublicKeyFromBase58(address)
	if err != nil {
		return decimal.Zero, fmt.Errorf("%w: %s", ErrInvalidAddress, address)
	}

	lamports, err := s.GetBalance(ctx, pk)
	if err != nil {
		return decimal.Zero, err
	}

	return LamportsToSOL(lamports), nil
}

// GetTokenBalance returns the SPL token balance (raw units) for owner+mint via ATA.
func (s *Solana) GetTokenBalance(ctx context.Context, owner, mint solanago.PublicKey) (uint64, error) {
	ata, err := s.FindATA(owner, mint)
	if err != nil {
		return 0, err
	}

	exists, err := s.AccountExists(ctx, ata)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}

	out, err := s.node.GetTokenAccountBalance(ctx, ata, s.config.Commitment)
	if err != nil {
		return 0, fmt.Errorf("%w: get token account balance: %w", ErrRetryable, err)
	}
	if out == nil || out.Value == nil {
		return 0, nil
	}

	amount, err := decimal.NewFromString(out.Value.Amount)
	if err != nil {
		return 0, fmt.Errorf("parse token amount: %w", err)
	}

	return uint64(amount.IntPart()), nil //nolint:gosec
}

// GetTokenBalanceDecimal returns the SPL token balance as a human-readable decimal.
func (s *Solana) GetTokenBalanceDecimal(ctx context.Context, ownerAddress, mintAddress string, decimals int64) (decimal.Decimal, error) {
	owner, err := solanago.PublicKeyFromBase58(ownerAddress)
	if err != nil {
		return decimal.Zero, fmt.Errorf("%w: owner %s", ErrInvalidAddress, ownerAddress)
	}

	mint, err := solanago.PublicKeyFromBase58(mintAddress)
	if err != nil {
		return decimal.Zero, fmt.Errorf("%w: mint %s", ErrInvalidAddress, mintAddress)
	}

	raw, err := s.GetTokenBalance(ctx, owner, mint)
	if err != nil {
		return decimal.Zero, err
	}

	return RawToTokens(raw, decimals), nil
}

// GetBalanceForAsset returns SOL or SPL balance depending on assetIdentifier.
func (s *Solana) GetBalanceForAsset(ctx context.Context, address, assetIdentifier string, decimals int64) (decimal.Decimal, error) {
	if assetIdentifier == SolAssetIdentifier {
		return s.GetBalanceSOL(ctx, address)
	}
	return s.GetTokenBalanceDecimal(ctx, address, assetIdentifier, decimals)
}
