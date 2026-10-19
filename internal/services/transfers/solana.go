package transfers

import (
	"context"
	"fmt"

	"github.com/dv-net/dv-processing/internal/constants"
	"github.com/dv-net/dv-processing/pkg/walletsdk/solana"
	"github.com/dv-net/dv-processing/pkg/walletsdk/wconstants"
	"github.com/dv-net/dv-processing/rpccode"
	"github.com/shopspring/decimal"
)

// processSolana handle transfer request for Solana blockchain.
func (s *Service) processSolana(ctx context.Context, req *CreateTransferRequest) error {
	if !s.config.Blockchain.Solana.Enabled {
		return rpccode.GetErrorByCode(rpccode.RPCCodeBlockchainIsDisabled)
	}

	if s.blockchains.Solana == nil {
		return fmt.Errorf("solana service is not initialized")
	}

	balance, err := s.eproxySvc.AddressBalance(ctx, req.FromAddresses[0], req.AssetIdentifier, req.Blockchain)
	if err != nil {
		// fallback to node when explorer-proxy is unavailable
		s.logger.Warnw("explorer-proxy balance check failed, falling back to node", "error", err)
		decimals := int64(solana.SolDecimals)
		if req.AssetIdentifier != solana.SolAssetIdentifier {
			decimals, err = s.eproxySvc.AssetDecimals(ctx, req.Blockchain, req.AssetIdentifier)
			if err != nil {
				return fmt.Errorf("get asset decimals: %w", err)
			}
		}
		balance, err = s.blockchains.Solana.GetBalanceForAsset(ctx, req.FromAddresses[0], req.AssetIdentifier, decimals)
		if err != nil {
			return fmt.Errorf("get wallet [%s] balance: %w", req.FromAddresses[0], err)
		}
	}

	insufficient := balance.LessThanOrEqual(req.Amount.Decimal)
	if req.Purpose == constants.TransferPurposeRefund {
		insufficient = balance.LessThan(req.Amount.Decimal)
	}

	if !req.WholeAmount && insufficient {
		return fmt.Errorf("%w for transfer. required: %s, available: %s", rpccode.GetErrorByCode(rpccode.RPCCodeAddressEmptyBalance), req.Amount.Decimal, balance)
	}

	if req.WholeAmount && !balance.IsPositive() {
		return fmt.Errorf("%w for transfer with whole amount, available: 0", rpccode.GetErrorByCode(rpccode.RPCCodeAddressEmptyBalance))
	}

	transfersInProgress, err := s.store.Transfers().Find(ctx, FindParams{
		StatusesNotIn: []string{
			constants.TransferStatusCompleted.String(),
			constants.TransferStatusFailed.String(),
			constants.TransferStatusCanceled.String(),
		},
		FromAddress: &req.FromAddresses[0],
		Blockchain:  &req.Blockchain,
	})
	if err != nil {
		return fmt.Errorf("find transfers: %w", err)
	}
	if len(transfersInProgress) > 0 {
		return rpccode.GetErrorByCode(rpccode.RPCCodeAddressIsTaken)
	}

	estimateResult, err := s.blockchains.Solana.EstimateTransfer(ctx, req.FromAddresses[0], req.ToAddresses[0], req.AssetIdentifier)
	if err != nil {
		return fmt.Errorf("estimate transfer: %w", err)
	}

	fee := estimateResult.FeeSOL.Mul(decimal.NewFromFloat(solana.TransferFeeCoeff))

	req.stateData = map[string]any{
		stateKeyHandlerEstimatedResources: estimateResult,
		"need_fee_amount":                 fee,
	}

	if req.Purpose == constants.TransferPurposeRefund {
		req.refundEstimatedFee = &fee
		if req.feeFromAmount() {
			if req.Amount.Decimal.LessThanOrEqual(fee) {
				return fmt.Errorf("%w: refund amount %s does not cover fee %s", rpccode.GetErrorByCode(rpccode.RPCCodeRefundInsufficientFee), req.Amount.Decimal, fee)
			}
			return nil
		}

		baseAssetBalance, err := s.eproxySvc.AddressBalance(ctx, req.FromAddresses[0], wconstants.BlockchainTypeSolana.GetAssetIdentifier(), req.Blockchain)
		if err != nil {
			baseAssetBalance, err = s.blockchains.Solana.GetBalanceSOL(ctx, req.FromAddresses[0])
			if err != nil {
				return fmt.Errorf("get hot wallet sol balance: %w", err)
			}
		}
		if baseAssetBalance.LessThan(fee) {
			return fmt.Errorf("%w: sol balance %s, fee %s", rpccode.GetErrorByCode(rpccode.RPCCodeRefundInsufficientFee), baseAssetBalance, fee)
		}
		return nil
	}

	// for SPL transfers from hot wallets, ensure SOL for fees (top-up from processing if needed)
	if req.walletFromType == constants.WalletTypeHot && req.AssetIdentifier != solana.SolAssetIdentifier {
		processingWallet, err := s.walletsSvc.Processing().GetByBlockchain(ctx, req.OwnerID, req.Blockchain)
		if err != nil {
			return fmt.Errorf("get processing wallet: %w", err)
		}

		hotSOL, err := s.blockchains.Solana.GetBalanceSOL(ctx, req.FromAddresses[0])
		if err != nil {
			return fmt.Errorf("get hot wallet sol balance: %w", err)
		}

		if hotSOL.LessThan(fee) {
			needFee := fee.Sub(hotSOL)
			req.stateData["need_fee_amount"] = needFee

			processingSOL, err := s.blockchains.Solana.GetBalanceSOL(ctx, processingWallet.Address)
			if err != nil {
				return fmt.Errorf("get processing wallet sol balance: %w", err)
			}
			if processingSOL.LessThan(needFee.Add(LamportsPerSignatureSOL())) {
				return fmt.Errorf(
					"%w sol on processing wallet: fee %s / balance %s",
					rpccode.GetErrorByCode(rpccode.RPCCodeNotEnoughBalance),
					needFee.String(),
					processingSOL.String(),
				)
			}
		}
	}

	return nil
}

// LamportsPerSignatureSOL returns the base signature fee in SOL.
func LamportsPerSignatureSOL() decimal.Decimal {
	return solana.LamportsToSOL(solana.LamportsPerSignature)
}
