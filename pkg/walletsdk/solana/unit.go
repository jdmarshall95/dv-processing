package solana

import "github.com/shopspring/decimal"

// LamportsToSOL converts lamports to SOL.
func LamportsToSOL(lamports uint64) decimal.Decimal {
	return decimal.NewFromInt(int64(lamports)).Div(decimal.NewFromInt(1e9)) //nolint:gosec
}

// SOLToLamports converts SOL to lamports (floors fractional lamports).
func SOLToLamports(amount decimal.Decimal) uint64 {
	lamports := amount.Mul(decimal.NewFromInt(1e9)).Floor()
	return uint64(lamports.IntPart()) //nolint:gosec
}

// TokensToRaw converts a human-readable token amount to raw units using decimals.
func TokensToRaw(amount decimal.Decimal, decimals int64) uint64 {
	raw := amount.Mul(decimal.NewFromInt(10).Pow(decimal.NewFromInt(decimals))).Floor()
	return uint64(raw.IntPart()) //nolint:gosec
}

// RawToTokens converts raw token units to a human-readable amount.
func RawToTokens(raw uint64, decimals int64) decimal.Decimal {
	return decimal.NewFromInt(int64(raw)).Div(decimal.NewFromInt(10).Pow(decimal.NewFromInt(decimals))) //nolint:gosec
}
