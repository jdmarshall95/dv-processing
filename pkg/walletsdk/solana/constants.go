package solana

const (
	// SolAssetIdentifier is the native SOL asset identifier used across the platform.
	SolAssetIdentifier = "sol"

	// SolDecimals is the number of decimals for native SOL (lamports).
	SolDecimals = 9

	// CoinType is the BIP44 coin type for Solana (SLIP-0044).
	CoinType = 501

	// USDCMintMainnet is the Circle USDC mint address on Solana mainnet-beta.
	USDCMintMainnet = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"

	// USDCDecimals is the number of decimals for USDC SPL token.
	USDCDecimals = 6

	// LamportsPerSignature is the base fee charged per transaction signature.
	LamportsPerSignature = uint64(5000)

	// ATARentExemptLamports is an approximate rent-exempt minimum for an ATA (~0.00203928 SOL).
	ATARentExemptLamports = uint64(2_039_280)

	// TransferFeeCoeff multiplies estimated fees when checking hot-wallet top-ups.
	TransferFeeCoeff = 1.2
)
