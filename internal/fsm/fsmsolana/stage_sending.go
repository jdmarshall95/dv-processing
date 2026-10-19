package fsmsolana

import (
	"context"
	"fmt"

	"github.com/dv-net/dv-processing/internal/models"
	"github.com/dv-net/dv-processing/internal/workflow"
	"github.com/dv-net/dv-processing/pkg/walletsdk/solana"
	"github.com/dv-net/dv-processing/rpccode"
	solanago "github.com/gagliardetto/solana-go"
	"github.com/shopspring/decimal"
)

func (s *FSM) sending(ctx context.Context, _ *workflow.Workflow, _ *workflow.Stage, _ *workflow.Step) error {
	existingTxs, err := s.st.TransferTransactions().FindTransactionByType(ctx, s.transfer.ID, models.TransferTransactionTypeTransfer)
	if err != nil {
		return fmt.Errorf("find existing transaction: %w", err)
	}
	if len(existingTxs) > 0 {
		s.logger.Infow("transaction already exists, skipping send", "tx_hash", existingTxs[0].TxHash)
		if s.transfer.TxHash.String == "" || s.transfer.TxHash.String != existingTxs[0].TxHash {
			s.transfer, err = s.bs.Transfers().SetTxHash(ctx, s.transfer.ID, existingTxs[0].TxHash)
			if err != nil {
				return fmt.Errorf("set tx hash: %w", err)
			}
		}
		return nil
	}

	fromAddress := s.transfer.GetFromAddress()
	toAddress := s.transfer.GetToAddress()

	amount := s.transfer.Amount.Decimal
	if s.transfer.WholeAmount {
		balance, err := s.getBalance(ctx, fromAddress, s.transfer.AssetIdentifier)
		if err != nil {
			return fmt.Errorf("get balance: %w", err)
		}
		amount = balance
	}

	estimateResult, err := s.solana.EstimateTransfer(ctx, fromAddress, toAddress, s.transfer.AssetIdentifier)
	if err != nil {
		return fmt.Errorf("estimate transfer: %w", err)
	}

	fee := estimateResult.FeeSOL

	if (s.transfer.WholeAmount || s.transfer.FeeFromAmount()) && s.transfer.AssetIdentifier == solana.SolAssetIdentifier {
		requested := amount
		amount = amount.Sub(fee)
		if s.transfer.FeeFromAmount() {
			if err := s.saveRefundFee(ctx, requested, fee, amount); err != nil {
				return err
			}
		}
	}

	if !amount.IsPositive() {
		return fmt.Errorf("transfer amount is less than or equal to zero, amount %s / fee %s", amount, fee)
	}

	sequence, err := s.bs.Wallets().GetSequenceByWalletType(ctx, s.transfer.WalletFromType, s.transfer.OwnerID, s.transfer.Blockchain, fromAddress)
	if err != nil {
		return fmt.Errorf("get sequence by wallet type: %w", err)
	}

	wcreds, err := s.getWalletCreds(ctx, s.transfer.OwnerID, uint32(sequence)) //nolint:gosec
	if err != nil {
		return fmt.Errorf("get wallet creds: %w", err)
	}

	toPk, err := solanago.PublicKeyFromBase58(toAddress)
	if err != nil {
		return fmt.Errorf("parse to address: %w", err)
	}

	memoText := s.memoFromState(ctx)

	var tx *solanago.Transaction

	if s.transfer.AssetIdentifier == solana.SolAssetIdentifier {
		tx, err = s.solana.BuildSOLTransfer(ctx, wcreds.PrivateKey, toPk, solana.SOLToLamports(amount), memoText)
		if err != nil {
			return fmt.Errorf("build sol transfer: %w", err)
		}
	} else {
		mint, err := solanago.PublicKeyFromBase58(s.transfer.AssetIdentifier)
		if err != nil {
			return fmt.Errorf("parse mint: %w", err)
		}

		decimals, err := s.getAssetDecimals(ctx, s.transfer.AssetIdentifier)
		if err != nil {
			return fmt.Errorf("get asset decimals: %w", err)
		}

		tx, err = s.solana.BuildSPLTransfer(
			ctx,
			wcreds.PrivateKey,
			toPk,
			mint,
			solana.TokensToRaw(amount, decimals),
			uint8(decimals), //nolint:gosec
			memoText,
			estimateResult.NeedCreateDestATA,
		)
		if err != nil {
			return fmt.Errorf("build spl transfer: %w", err)
		}
	}

	if len(tx.Signatures) == 0 {
		return fmt.Errorf("signed transaction has no signatures")
	}
	sig := tx.Signatures[0]

	if err := s.broadcast(ctx, tx, sig); err != nil {
		return fmt.Errorf("broadcast: %w", err)
	}

	if err := s.bs.Transfers().SetStateData(ctx, s.transfer.ID, map[string]any{
		"send_asset": map[string]any{
			"amount":          amount.String(),
			"fee":             fee.String(),
			"memo":            memoText,
			"need_create_ata": estimateResult.NeedCreateDestATA,
		},
	}); err != nil {
		return fmt.Errorf("set state data: %w", err)
	}

	s.transfer, err = s.bs.Transfers().SetTxHash(ctx, s.transfer.ID, sig.String())
	if err != nil {
		return fmt.Errorf("set tx hash: %w", err)
	}

	return nil
}

func (s *FSM) saveRefundFee(ctx context.Context, requestedAmount, fee, sentAmount decimal.Decimal) error {
	if !sentAmount.IsPositive() {
		return fmt.Errorf("%w: refund amount %s does not cover fee %s", rpccode.GetErrorByCode(rpccode.RPCCodeRefundInsufficientFee), requestedAmount, fee)
	}

	if err := s.bs.Transfers().SetStateData(ctx, s.transfer.ID, map[string]any{
		"refund": map[string]any{
			"amount":      requestedAmount.String(),
			"fee":         fee.String(),
			"sent_amount": sentAmount.String(),
		},
	}); err != nil {
		return fmt.Errorf("set state data: %w", err)
	}

	return nil
}
