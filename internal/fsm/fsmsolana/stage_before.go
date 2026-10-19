package fsmsolana

import (
	"context"
	"fmt"

	"github.com/dv-net/dv-processing/internal/constants"
	"github.com/dv-net/dv-processing/internal/models"
	"github.com/dv-net/dv-processing/internal/workflow"
	"github.com/dv-net/dv-processing/pkg/walletsdk/solana"
	"github.com/dv-net/dv-processing/rpccode"
	solanago "github.com/gagliardetto/solana-go"
	"github.com/shopspring/decimal"
)

func (s *FSM) validateRequest(ctx context.Context, _ *workflow.Workflow, _ *workflow.Stage, _ *workflow.Step) error {
	if len(s.transfer.FromAddresses) != 1 {
		return fmt.Errorf("required one from address")
	}
	if len(s.transfer.ToAddresses) != 1 {
		return fmt.Errorf("required one to address")
	}
	if s.transfer.AssetIdentifier == "" {
		return fmt.Errorf("asset identifier is empty")
	}

	if err := s.bs.Wallets().ValidateTransferDestination(ctx, s.transfer); err != nil {
		return fmt.Errorf("validate transfer destination: %w", err)
	}

	return s.setTransferStatus(ctx, constants.TransferStatusProcessing)
}

// sendSOLForFees tops up the hot wallet with SOL from the processing wallet when sending SPL tokens.
func (s *FSM) sendSOLForFees(ctx context.Context, wf *workflow.Workflow, _ *workflow.Stage, _ *workflow.Step) error {
	if s.transfer.WalletFromType == constants.WalletTypeProcessing {
		wf.State.SetNextStage(stageSending).SetNextStep(stepSending)
		return nil
	}

	if s.transfer.WalletFromType != constants.WalletTypeHot {
		return fmt.Errorf("unsupported wallet type: %s", s.transfer.WalletFromType)
	}

	if s.transfer.AssetIdentifier == solana.SolAssetIdentifier {
		wf.State.SetNextStage(stageSending).SetNextStep(stepSending)
		return nil
	}

	existingTxs, err := s.st.TransferTransactions().FindTransactionByType(ctx, s.transfer.ID, models.TransferTransactionTypeSendBurnBaseAsset)
	if err != nil {
		return fmt.Errorf("find existing transaction: %w", err)
	}
	if len(existingTxs) > 0 {
		s.logger.Infow("fee top-up transaction already exists, skipping send", "tx_hash", existingTxs[0].TxHash)
		return nil
	}

	estimateResult, err := s.solana.EstimateTransfer(ctx, s.transfer.GetFromAddress(), s.transfer.GetToAddress(), s.transfer.AssetIdentifier)
	if err != nil {
		return fmt.Errorf("estimate transfer: %w", err)
	}

	transferFee := estimateResult.FeeSOL.Mul(decimal.NewFromFloat(solana.TransferFeeCoeff))

	hotSOL, err := s.getBalance(ctx, s.transfer.GetFromAddress(), solana.SolAssetIdentifier)
	if err != nil {
		return fmt.Errorf("get hot wallet sol balance: %w", err)
	}

	if hotSOL.GreaterThanOrEqual(transferFee) {
		wf.State.SetNextStage(stageSending).SetNextStep(stepSending)
		return nil
	}

	needFee := transferFee.Sub(hotSOL)
	if !needFee.IsPositive() {
		wf.State.SetNextStage(stageSending).SetNextStep(stepSending)
		return nil
	}

	if s.transfer.IsRefund() {
		return fmt.Errorf("%w: sol balance %s, fee %s", rpccode.GetErrorByCode(rpccode.RPCCodeRefundInsufficientFee), hotSOL, transferFee)
	}

	processingWallet, err := s.bs.Wallets().Processing().GetByBlockchain(ctx, s.transfer.OwnerID, s.transfer.Blockchain)
	if err != nil {
		return fmt.Errorf("get processing wallet: %w", err)
	}

	processingSOL, err := s.getBalance(ctx, processingWallet.Address, solana.SolAssetIdentifier)
	if err != nil {
		return fmt.Errorf("get processing sol balance: %w", err)
	}

	topUpCost := needFee.Add(solana.LamportsToSOL(solana.LamportsPerSignature))
	if processingSOL.LessThan(topUpCost) {
		return fmt.Errorf("not enough sol on processing wallet: need %s / balance %s", topUpCost, processingSOL)
	}

	wcreds, err := s.getWalletCreds(ctx, s.transfer.OwnerID, uint32(processingWallet.Sequence)) //nolint:gosec
	if err != nil {
		return fmt.Errorf("get wallet creds: %w", err)
	}

	toPk, err := solanago.PublicKeyFromBase58(s.transfer.GetFromAddress())
	if err != nil {
		return fmt.Errorf("parse hot wallet address: %w", err)
	}

	tx, err := s.solana.BuildSOLTransfer(ctx, wcreds.PrivateKey, toPk, solana.SOLToLamports(needFee), "")
	if err != nil {
		return fmt.Errorf("build sol top-up: %w", err)
	}

	sig := tx.Signatures[0]
	if err := s.broadcast(ctx, tx, sig); err != nil {
		return fmt.Errorf("broadcast sol top-up: %w", err)
	}

	if err := s.bs.Transfers().SetStateData(ctx, s.transfer.ID, map[string]any{
		SendSOLForFees: map[string]any{
			"from":            processingWallet.Address,
			"to":              s.transfer.GetFromAddress(),
			"need_fee_amount": needFee.String(),
			"tx_hash":         sig.String(),
		},
	}); err != nil {
		return fmt.Errorf("set state data: %w", err)
	}

	return nil
}

func (s *FSM) waitingSendSOLForFeesConfirmations(ctx context.Context, wf *workflow.Workflow, _ *workflow.Stage, _ *workflow.Step) error {
	txs, err := s.st.TransferTransactions().FindTransactionByType(ctx, s.transfer.ID, models.TransferTransactionTypeSendBurnBaseAsset)
	if err != nil {
		return err
	}
	if len(txs) == 0 {
		// skipped top-up
		wf.State.SetNextStage(stageSending).SetNextStep(stepSending)
		return nil
	}
	if len(txs) != 1 {
		return fmt.Errorf("expected exactly one fee top-up tx, got: %d", len(txs))
	}

	if err := s.ensureTxConfirmed(ctx, txs[0].TxHash); err != nil {
		return err
	}

	wf.State.SetNextStage(stageSending).SetNextStep(stepSending)
	return nil
}
