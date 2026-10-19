package fsmsolana

import (
	"context"
	"fmt"
	"time"

	"github.com/dv-net/dv-processing/internal/constants"
	"github.com/dv-net/dv-processing/internal/workflow"
	solanago "github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/riverqueue/river"
)

func (s *FSM) waitingForTheFirstConfirmation(ctx context.Context, _ *workflow.Workflow, _ *workflow.Stage, _ *workflow.Step) error {
	sentAt, err := s.txSentAt(ctx, s.transfer.TxHash.String)
	if err != nil {
		return err
	}

	sig, err := solanago.SignatureFromBase58(s.transfer.TxHash.String)
	if err != nil {
		return fmt.Errorf("parse signature: %w", err)
	}

	statuses, err := s.solana.GetSignatureStatuses(ctx, sig)
	if err != nil || len(statuses) == 0 || statuses[0] == nil {
		if time.Since(sentAt) > 5*time.Minute {
			return newErrFailedTransfer(fmt.Errorf("transaction %s not found in the blockchain: %w", s.transfer.TxHash.String, err))
		}
		duration := time.Second
		if time.Since(sentAt) > time.Hour {
			duration = 30 * time.Second
		}
		return workflow.NoConsoleError(river.JobSnooze(duration))
	}

	st := statuses[0]
	if st.Err != nil {
		return newErrFailedTransfer(fmt.Errorf("transaction failed on-chain: %v", st.Err))
	}

	if st.ConfirmationStatus == rpc.ConfirmationStatusProcessed || st.ConfirmationStatus == "" {
		return workflow.NoConsoleError(river.JobSnooze(time.Second))
	}

	if err := s.ensureTxConfirmed(ctx, s.transfer.TxHash.String); err != nil {
		return err
	}

	return s.setTransferStatus(ctx, constants.TransferStatusUnconfirmed)
}

func (s *FSM) waitingConfirmations(ctx context.Context, _ *workflow.Workflow, _ *workflow.Stage, _ *workflow.Step) error {
	return s.checkTransactionConfirmations(ctx, s.transfer.TxHash.String)
}

func (s *FSM) sendSuccessEvent(ctx context.Context, _ *workflow.Workflow, _ *workflow.Stage, _ *workflow.Step) error {
	return s.setTransferStatus(ctx, constants.TransferStatusCompleted)
}
