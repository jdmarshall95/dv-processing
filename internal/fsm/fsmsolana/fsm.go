package fsmsolana

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/dv-net/dv-processing/internal/config"
	"github.com/dv-net/dv-processing/internal/constants"
	"github.com/dv-net/dv-processing/internal/models"
	"github.com/dv-net/dv-processing/internal/services/baseservices"
	"github.com/dv-net/dv-processing/internal/store"
	"github.com/dv-net/dv-processing/internal/store/repos"
	"github.com/dv-net/dv-processing/internal/workflow"
	"github.com/dv-net/dv-processing/pkg/walletsdk/solana"
	"github.com/dv-net/mx/logger"
	"github.com/jackc/pgx/v5"
)

// FSM is the Solana transfer finite state machine.
type FSM struct {
	logger                logger.Logger
	config                *config.Config
	enabledSeedEncryption bool
	wf                    *workflow.Workflow
	transfer              *models.Transfer
	st                    store.IStore
	solana                *solana.Solana
	bs                    baseservices.IBaseServices
}

// NewFSM creates a Solana transfer FSM.
func NewFSM(
	l logger.Logger,
	conf *config.Config,
	st store.IStore,
	bs baseservices.IBaseServices,
	transfer *models.Transfer,
) (*FSM, error) {
	if bs.Solana() == nil {
		return nil, fmt.Errorf("solana service is not initialized")
	}

	fsm := &FSM{
		logger:                l,
		config:                conf,
		enabledSeedEncryption: conf.IsEnabledSeedEncryption(),
		st:                    st,
		bs:                    bs,
		solana:                bs.Solana(),
		transfer:              transfer,
	}

	fsm.wf = workflow.New(
		workflow.WithName("solana fsm"),
		workflow.WithLogger(l),
		workflow.WithDebug(true),
		workflow.WithBeforeAllStepsFn(func(ctx context.Context, _ *workflow.Workflow, _ *workflow.Stage, _ *workflow.Step) error {
			if err := fsm.bs.Transfers().SetWorkflowSnapshot(ctx, fsm.transfer.ID, fsm.wf.GetSnapshot()); err != nil {
				return fmt.Errorf("set workflow snapshot: %w", err)
			}
			return nil
		}),
		workflow.WithAfterAllStepsFn(func(ctx context.Context, _ *workflow.Workflow, _ *workflow.Stage, _ *workflow.Step) error {
			if err := fsm.bs.Transfers().SetWorkflowSnapshot(ctx, fsm.transfer.ID, fsm.wf.GetSnapshot()); err != nil {
				return fmt.Errorf("set workflow snapshot: %w", err)
			}
			return nil
		}),
		workflow.WithAfterFn(func(ctx context.Context, _ *workflow.Workflow) error {
			if err := fsm.bs.Transfers().SetWorkflowSnapshot(ctx, fsm.transfer.ID, fsm.wf.GetSnapshot()); err != nil {
				return fmt.Errorf("set workflow snapshot: %w", err)
			}
			return nil
		}),
	).SetOnFailureFn(func(ctx context.Context, w *workflow.Workflow, err error) error {
		if err == nil {
			return nil
		}

		if errors.Is(err, errFailedTransfer) {
			return pgx.BeginTxFunc(ctx, st.PSQLConn(), pgx.TxOptions{}, func(tx pgx.Tx) error {
				return fsm.sendFailureEvent(ctx, w, err, repos.WithTx(tx))
			})
		}

		stage, step := w.CurrentStage(), w.CurrentStep()
		if stage == nil || stage.Name == "" || step == nil || step.Name == "" {
			return nil
		}

		currentStep := step.Name
		if !slices.Contains(exitOnFailedSteps[stage.Name], currentStep) {
			return nil
		}

		sent, sentErr := fsm.stepTxSaved(ctx, currentStep)
		if sentErr != nil || sent {
			fsm.logger.Warnw("step failed after its transaction was saved, retrying",
				"transfer_id", fsm.transfer.ID,
				"step", currentStep,
				"error", err,
				"check_error", sentErr,
			)
			return nil
		}

		return pgx.BeginTxFunc(ctx, st.PSQLConn(), pgx.TxOptions{}, func(tx pgx.Tx) error {
			return fsm.sendFailureEvent(ctx, w, err, repos.WithTx(tx))
		})
	})

	fsm.wf.SetStages([]*workflow.Stage{
		{
			Name: stageBeforeSending,
			Steps: []*workflow.Step{
				{Name: stepValidateRequest, Func: fsm.validateRequest},
				{Name: stepSendSOLForFees, Func: fsm.sendSOLForFees, BeforeFn: fsm.createTransferStepWh},
				{Name: stepWaitingSendSOLForFees, Func: fsm.waitingSendSOLForFeesConfirmations},
			},
		},
		{
			Name: stageSending,
			Steps: []*workflow.Step{
				{Name: stepSending, Func: fsm.sending},
			},
		},
		{
			Name: stageAfterSending,
			Steps: []*workflow.Step{
				{Name: stepWaitingForTheFirstConfirmation, Func: fsm.waitingForTheFirstConfirmation, BeforeFn: fsm.createTransferStepWh},
				{Name: stepWaitingConfirmations, Func: fsm.waitingConfirmations, BeforeFn: fsm.createTransferStepWh},
				{Name: stepSendSuccessEvent, Func: fsm.sendSuccessEvent},
			},
		},
	})

	if err := fsm.wf.SetSnapshot(transfer.WorkflowSnapshot); err != nil {
		return nil, fmt.Errorf("set workflow snapshot for transfer %s: %w", transfer.ID.String(), err)
	}

	return fsm, nil
}

// Run executes the workflow.
func (s *FSM) Run(ctx context.Context) error {
	return s.wf.Run(constants.WithClientContext(ctx, s.transfer.ClientID))
}

func (s *FSM) stepTxSaved(ctx context.Context, step string) (bool, error) {
	var txType models.TransferTransactionType
	switch step {
	case stepSending:
		txType = models.TransferTransactionTypeTransfer
	case stepSendSOLForFees:
		txType = models.TransferTransactionTypeSendBurnBaseAsset
	default:
		return false, nil
	}

	txs, err := s.st.TransferTransactions().FindTransactionByType(ctx, s.transfer.ID, txType)
	if err != nil {
		return false, fmt.Errorf("find system transactions: %w", err)
	}

	return len(txs) > 0, nil
}
