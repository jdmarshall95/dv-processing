package solana

import (
	"context"
	"fmt"

	solanago "github.com/gagliardetto/solana-go"
	"github.com/shopspring/decimal"
)

// EstimateTransferResult holds fee estimates for a Solana transfer.
type EstimateTransferResult struct {
	// FeeLamports is the estimated network fee in lamports (signatures + optional ATA rent).
	FeeLamports uint64 `json:"fee_lamports"`
	// FeeSOL is FeeLamports expressed in SOL.
	FeeSOL decimal.Decimal `json:"fee_sol"`
	// NeedCreateDestATA is true when the destination ATA must be created.
	NeedCreateDestATA bool `json:"need_create_dest_ata"`
	// Signatures is the number of signatures expected in the transaction.
	Signatures int `json:"signatures"`
}

// EstimateTransfer estimates the SOL fee required to send assetIdentifier from→to.
func (s *Solana) EstimateTransfer(
	ctx context.Context,
	fromAddress, toAddress, assetIdentifier string,
) (*EstimateTransferResult, error) {
	signatures := 1
	fee := LamportsPerSignature * uint64(signatures) //nolint:gosec
	needATA := false

	if assetIdentifier != SolAssetIdentifier {
		to, err := solanago.PublicKeyFromBase58(toAddress)
		if err != nil {
			return nil, fmt.Errorf("%w: to %s", ErrInvalidAddress, toAddress)
		}
		mint, err := solanago.PublicKeyFromBase58(assetIdentifier)
		if err != nil {
			return nil, fmt.Errorf("%w: mint %s", ErrInvalidAddress, assetIdentifier)
		}

		ata, err := s.FindATA(to, mint)
		if err != nil {
			return nil, err
		}

		exists, err := s.AccountExists(ctx, ata)
		if err != nil {
			return nil, err
		}
		if !exists {
			needATA = true
			fee += ATARentExemptLamports
		}
	}

	_ = fromAddress // reserved for future priority-fee estimation keyed by sender

	return &EstimateTransferResult{
		FeeLamports:       fee,
		FeeSOL:            LamportsToSOL(fee),
		NeedCreateDestATA: needATA,
		Signatures:        signatures,
	}, nil
}
