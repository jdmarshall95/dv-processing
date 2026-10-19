package fsmsolana

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dv-net/dv-processing/internal/constants"
	"github.com/dv-net/dv-processing/internal/models"
	"github.com/dv-net/dv-processing/internal/services/webhooks"
	"github.com/dv-net/dv-processing/internal/store/repos"
	"github.com/dv-net/dv-processing/internal/store/repos/repo_transfer_transactions"
	"github.com/dv-net/dv-processing/internal/workflow"
	"github.com/dv-net/dv-processing/pkg/encryption"
	"github.com/dv-net/dv-processing/pkg/utils"
	"github.com/dv-net/dv-processing/pkg/walletsdk/solana"
	"github.com/dv-net/dv-processing/pkg/walletsdk/wconstants"
	trxv2 "github.com/dv-net/dv-proto/gen/go/eproxy/transactions/v2"
	solanago "github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/shopspring/decimal"
)

func (s *FSM) sendFailureEvent(ctx context.Context, w *workflow.Workflow, err error, repoOpts ...repos.Option) error {
	var stepName string
	if s.wf.CurrentStep() != nil {
		stepName = s.wf.CurrentStep().Name
	}

	params, err := s.bs.Webhooks().EventTransferStatusCreateParams(ctx, webhooks.EventTransferStatusCreateParamsData{
		TransferID:   s.transfer.ID,
		OwnerID:      s.transfer.OwnerID,
		Status:       constants.TransferStatusFailed,
		Step:         stepName,
		ErrorMessage: err.Error(),
	})
	if err != nil {
		return fmt.Errorf("get event transfer status create params: %w", err)
	}

	w.State.SetFailed(true).SetError(err)
	w.SetSkipError(true)

	if err := s.bs.Transfers().SetWorkflowSnapshot(ctx, s.transfer.ID, w.GetSnapshot(), repoOpts...); err != nil {
		return fmt.Errorf("set workflow snapshot: %w", err)
	}

	if err := s.bs.Webhooks().BatchCreate(ctx, []webhooks.BatchCreateParams{params}, repoOpts...); err != nil {
		return fmt.Errorf("create failed event: %w", err)
	}

	if err := s.bs.Transfers().SetStatus(ctx, s.transfer.ID, constants.TransferStatusFailed, repoOpts...); err != nil {
		return fmt.Errorf("set transfer status %s: %w", constants.TransferStatusFailed, err)
	}

	return nil
}

func (s *FSM) setTransferStatus(ctx context.Context, status constants.TransferStatus, repoOpts ...repos.Option) error {
	var stepName string
	if s.wf.CurrentStep() != nil {
		stepName = s.wf.CurrentStep().Name
	}

	params, err := s.bs.Webhooks().EventTransferStatusCreateParams(ctx, webhooks.EventTransferStatusCreateParamsData{
		TransferID: s.transfer.ID,
		OwnerID:    s.transfer.OwnerID,
		Step:       stepName,
		Status:     status,
	})
	if err != nil {
		return fmt.Errorf("get event transfer status create params: %w", err)
	}

	if err := s.bs.Webhooks().BatchCreate(ctx, []webhooks.BatchCreateParams{params}, repoOpts...); err != nil {
		return fmt.Errorf("create failed event: %w", err)
	}

	if err := s.bs.Transfers().SetStatus(ctx, s.transfer.ID, status, repoOpts...); err != nil {
		return fmt.Errorf("set transfer status %s: %w", status, err)
	}

	return nil
}

func (s *FSM) createTransferStepWh(ctx context.Context, step *workflow.Step) error {
	params, err := s.bs.Webhooks().EventTransferStatusCreateParams(ctx, webhooks.EventTransferStatusCreateParamsData{
		TransferID: s.transfer.ID,
		OwnerID:    s.transfer.OwnerID,
		Status:     s.transfer.Status,
		Step:       step.Name,
	})
	if err != nil {
		return fmt.Errorf("get event transfer status create params: %w", err)
	}

	if err := s.bs.Webhooks().BatchCreate(ctx, []webhooks.BatchCreateParams{params}); err != nil {
		return fmt.Errorf("create failed event: %w", err)
	}

	return nil
}

func (s *FSM) txSentAt(ctx context.Context, txHash string) (time.Time, error) {
	txs, err := s.st.TransferTransactions().GetAllByTransfer(ctx, s.transfer.ID)
	if err != nil {
		return time.Time{}, fmt.Errorf("get transfer transactions: %w", err)
	}

	for _, tx := range txs {
		if strings.EqualFold(tx.TxHash, txHash) && tx.CreatedAt.Valid {
			return tx.CreatedAt.Time, nil
		}
	}

	return s.transfer.CreatedAt.Time, nil
}

func (s *FSM) getBalance(ctx context.Context, address, assetIdentifier string) (decimal.Decimal, error) {
	balance, err := s.bs.EProxy().AddressBalance(ctx, address, assetIdentifier, wconstants.BlockchainTypeSolana)
	if err != nil {
		s.logger.Warnw("explorer-proxy balance check failed, falling back to node", "error", err, "address", address)
		return s.getBalanceFromNode(ctx, address, assetIdentifier)
	}
	return balance, nil
}

func (s *FSM) getBalanceFromNode(ctx context.Context, address, assetIdentifier string) (decimal.Decimal, error) {
	if assetIdentifier == solana.SolAssetIdentifier {
		return s.solana.GetBalanceSOL(ctx, address)
	}

	decimals, err := s.getAssetDecimals(ctx, assetIdentifier)
	if err != nil {
		return decimal.Zero, err
	}

	return s.solana.GetTokenBalanceDecimal(ctx, address, assetIdentifier, decimals)
}

func (s *FSM) getAssetDecimals(ctx context.Context, assetIdentifier string) (int64, error) {
	if assetIdentifier == solana.SolAssetIdentifier {
		return solana.SolDecimals, nil
	}
	return s.bs.EProxy().AssetDecimals(ctx, wconstants.BlockchainTypeSolana, assetIdentifier)
}

type walletCreds struct {
	Address    string
	PrivateKey solanago.PrivateKey
}

func (s *FSM) getWalletCreds(ctx context.Context, ownerID uuid.UUID, sequence uint32) (*walletCreds, error) {
	owner, err := s.bs.Owners().GetByID(ctx, ownerID)
	if err != nil {
		return nil, fmt.Errorf("get owner: %w", err)
	}

	mnemonic := owner.Mnemonic
	if s.enabledSeedEncryption {
		mnemonic, err = encryption.Decrypt(mnemonic, owner.ID.String())
		if err != nil {
			return nil, fmt.Errorf("decrypt mnemonic: %w", err)
		}
	}

	address, priv, err := solana.WalletPubKeyHash(mnemonic, owner.PassPhrase.String, sequence)
	if err != nil {
		return nil, err
	}

	return &walletCreds{
		Address:    address,
		PrivateKey: priv,
	}, nil
}

func (s *FSM) memoFromState(ctx context.Context) string {
	stateData, err := s.bs.Transfers().GetStateData(ctx, s.transfer.ID)
	if err != nil || stateData == nil {
		return ""
	}
	if v, ok := stateData[StateKeyMemo].(string); ok {
		return v
	}
	if v, ok := stateData[StateKeyReference].(string); ok {
		return v
	}
	return ""
}

func (s *FSM) broadcast(ctx context.Context, tx *solanago.Transaction, sig solanago.Signature) error {
	isTransferTx := s.wf.CurrentStep() != nil && s.wf.CurrentStep().Name == stepSending

	var systemTx *models.TransferTransaction
	if err := pgx.BeginTxFunc(ctx, s.st.PSQLConn(), pgx.TxOptions{}, func(dbTx pgx.Tx) error {
		var err error
		systemTx, err = s.initPendingSystemTransaction(ctx, sig.String(), dbTx)
		if err != nil {
			return fmt.Errorf("create system transaction: %w", err)
		}

		if isTransferTx {
			if err := s.bs.Transfers().SetBroadcastAt(ctx, s.transfer.ID, repos.WithTx(dbTx)); err != nil {
				return fmt.Errorf("set broadcast at: %w", err)
			}
		}

		return nil
	}); err != nil {
		return err
	}

	sendErr := error(nil)
	if tx != nil {
		_, sendErr = s.solana.BroadcastSigned(ctx, tx)
	}

	if sendErr == nil {
		return nil
	}

	errMsg := strings.ToLower(sendErr.Error())
	if strings.Contains(errMsg, "already processed") || strings.Contains(errMsg, "already been processed") {
		return nil
	}

	// ambiguous network errors: keep system tx and retry
	if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "connection") {
		return fmt.Errorf("send transaction: %w", sendErr)
	}

	if err := pgx.BeginTxFunc(ctx, s.st.PSQLConn(), pgx.TxOptions{}, func(dbTx pgx.Tx) error {
		if err := s.st.TransferTransactions(repos.WithTx(dbTx)).Delete(ctx, systemTx.ID); err != nil {
			return fmt.Errorf("delete rejected system transaction: %w", err)
		}
		if isTransferTx {
			if err := s.bs.Transfers().ClearBroadcastAt(ctx, s.transfer.ID, repos.WithTx(dbTx)); err != nil {
				return fmt.Errorf("clear broadcast at: %w", err)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("send transaction: %w, rollback system transaction: %w", sendErr, err)
	}

	return fmt.Errorf("send transaction rejected by node: %w", sendErr)
}

func (s *FSM) initPendingSystemTransaction(ctx context.Context, hash string, dbTx pgx.Tx) (*models.TransferTransaction, error) {
	txType, err := s.prepareTransferTransactionTypeByStep()
	if err != nil {
		return nil, fmt.Errorf("prepare transfer transaction type: %w", err)
	}

	systemTx, err := s.st.TransferTransactions(repos.WithTx(dbTx)).Create(ctx, repo_transfer_transactions.CreateParams{
		TransferID: s.transfer.ID,
		TxHash:     hash,
		TxType:     *txType,
		Status:     models.TransferTransactionsStatusPending,
		Step:       s.wf.CurrentStep().Name,
	})
	if err != nil {
		return nil, fmt.Errorf("create transfer transaction: %w", err)
	}

	return systemTx, nil
}

func (s *FSM) prepareTransferTransactionTypeByStep() (*models.TransferTransactionType, error) {
	switch s.wf.CurrentStep().Name {
	case stepSendSOLForFees:
		return utils.Pointer(models.TransferTransactionTypeSendBurnBaseAsset), nil
	case stepSending:
		return utils.Pointer(models.TransferTransactionTypeTransfer), nil
	default:
		return nil, fmt.Errorf("unknown transfer step: %s", s.wf.CurrentStep().Name)
	}
}

func (s *FSM) ensureTxConfirmed(ctx context.Context, txHash string) error {
	sig, err := solanago.SignatureFromBase58(txHash)
	if err != nil {
		return fmt.Errorf("parse signature: %w", err)
	}

	sentAt, err := s.txSentAt(ctx, txHash)
	if err != nil {
		return err
	}

	statuses, err := s.solana.GetSignatureStatuses(ctx, sig)
	if err != nil {
		if time.Since(sentAt) > time.Hour {
			return newErrFailedTransfer(fmt.Errorf("transaction %s not found after 1 hour: %w", txHash, err))
		}
		return workflow.NoConsoleError(river.JobSnooze(time.Second))
	}

	if len(statuses) == 0 || statuses[0] == nil {
		if time.Since(sentAt) > time.Hour {
			return newErrFailedTransfer(fmt.Errorf("transaction %s not found after 1 hour", txHash))
		}
		return workflow.NoConsoleError(river.JobSnooze(time.Second))
	}

	st := statuses[0]
	if st.Err != nil {
		_ = s.updateSystemTransactionStatus(ctx, txHash, models.TransferTransactionsStatusFailed, decimal.Zero, decimal.Zero)
		return newErrFailedTransfer(fmt.Errorf("transaction failed on-chain: %v", st.Err))
	}

	confirmed := st.ConfirmationStatus == rpc.ConfirmationStatusConfirmed ||
		st.ConfirmationStatus == rpc.ConfirmationStatusFinalized
	if !confirmed {
		return workflow.NoConsoleError(river.JobSnooze(
			constants.ConfirmationsTimeoutWithRequired(wconstants.BlockchainTypeSolana, 1, 0),
		))
	}

	return s.updateSystemTransactionStatus(ctx, txHash, models.TransferTransactionsStatusUnconfirmed, decimal.Zero, decimal.Zero)
}

func (s *FSM) checkTransactionConfirmations(ctx context.Context, txHash string) error {
	// Prefer explorer-proxy when available
	tx, err := s.bs.EProxy().GetTransactionInfo(ctx, wconstants.BlockchainTypeSolana, txHash)
	if err == nil {
		if tx.GetStatus() != "success" {
			_ = s.updateSystemTransactionFromEproxy(ctx, tx, models.TransferTransactionsStatusFailed)
			return newErrFailedTransfer(fmt.Errorf("transaction status is not success: %s", tx.GetStatus()))
		}

		need := constants.GetMinConfirmations(wconstants.BlockchainTypeSolana)
		if tx.Confirmations < need {
			_ = s.updateSystemTransactionFromEproxy(ctx, tx, models.TransferTransactionsStatusUnconfirmed)
			return workflow.NoConsoleError(river.JobSnooze(
				constants.ConfirmationsTimeoutWithRequired(wconstants.BlockchainTypeSolana, need, tx.Confirmations),
			))
		}

		return s.updateSystemTransactionFromEproxy(ctx, tx, models.TransferTransactionsStatusConfirmed)
	}

	// Fallback to RPC GetSignatureStatuses with commitment=confirmed / finalized
	sig, parseErr := solanago.SignatureFromBase58(txHash)
	if parseErr != nil {
		return fmt.Errorf("parse signature: %w", parseErr)
	}

	statuses, stErr := s.solana.GetSignatureStatuses(ctx, sig)
	if stErr != nil {
		return workflow.NoConsoleError(river.JobSnooze(2 * time.Second))
	}
	if len(statuses) == 0 || statuses[0] == nil {
		return workflow.NoConsoleError(river.JobSnooze(2 * time.Second))
	}

	st := statuses[0]
	if st.Err != nil {
		_ = s.updateSystemTransactionStatus(ctx, txHash, models.TransferTransactionsStatusFailed, decimal.Zero, decimal.Zero)
		return newErrFailedTransfer(fmt.Errorf("transaction failed on-chain: %v", st.Err))
	}

	if st.ConfirmationStatus != rpc.ConfirmationStatusFinalized &&
		st.ConfirmationStatus != rpc.ConfirmationStatusConfirmed {
		return workflow.NoConsoleError(river.JobSnooze(time.Second))
	}

	return s.updateSystemTransactionStatus(ctx, txHash, models.TransferTransactionsStatusConfirmed, decimal.Zero, decimal.Zero)
}

func (s *FSM) updateSystemTransactionStatus(
	ctx context.Context,
	txHash string,
	status models.TransferTransactionsStatus,
	nativeAmount, nativeFee decimal.Decimal,
) error {
	return s.st.TransferTransactions().UpdatePendingTxExpense(ctx, repo_transfer_transactions.UpdatePendingTxExpenseParams{
		NativeTokenAmount: nativeAmount,
		NativeTokenFee:    nativeFee,
		CurrentTxStatus:   status,
		TransferID:        s.transfer.ID,
		TxHash:            txHash,
	})
}

func (s *FSM) updateSystemTransactionFromEproxy(ctx context.Context, tx *trxv2.Transaction, status models.TransferTransactionsStatus) error {
	if tx == nil {
		return fmt.Errorf("invalid transaction")
	}

	nativeTokenFee, err := decimal.NewFromString(tx.GetFee())
	if err != nil {
		nativeTokenFee = decimal.Zero
	}

	nativeTokenAmount := decimal.Zero
	if tx.GetAssetIdentifier() == solana.SolAssetIdentifier {
		nativeTokenAmount, err = decimal.NewFromString(tx.GetAmount())
		if err != nil {
			nativeTokenAmount = decimal.Zero
		}
	}

	return s.updateSystemTransactionStatus(ctx, tx.GetHash(), status, nativeTokenAmount, nativeTokenFee)
}
