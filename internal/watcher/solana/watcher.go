package solana

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dv-net/dv-processing/internal/config"
	"github.com/dv-net/dv-processing/internal/models"
	"github.com/dv-net/dv-processing/internal/services/baseservices"
	"github.com/dv-net/dv-processing/internal/services/wallets"
	"github.com/dv-net/dv-processing/internal/services/webhooks"
	"github.com/dv-net/dv-processing/internal/store"
	"github.com/dv-net/dv-processing/internal/store/repos"
	"github.com/dv-net/dv-processing/pkg/utils"
	"github.com/dv-net/dv-processing/pkg/walletsdk/solana"
	"github.com/dv-net/dv-processing/pkg/walletsdk/wconstants"
	"github.com/dv-net/mx/logger"
	solanago "github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

const serviceName = "solana-watcher"

// Service polls Solana RPC for incoming transfers to hot wallets.
type Service struct {
	log    logger.Logger
	conf   config.SolanaBlockchain
	client *solana.Solana
	bs     baseservices.IBaseServices
	store  store.IStore

	seenMu sync.Mutex
	seen   map[string]struct{}
}

// New creates a Solana polling watcher.
func New(
	l logger.Logger,
	conf config.SolanaBlockchain,
	client *solana.Solana,
	bs baseservices.IBaseServices,
	st store.IStore,
) *Service {
	return &Service{
		log: logger.With(l,
			"module", serviceName,
		),
		conf:   conf,
		client: client,
		bs:     bs,
		store:  st,
		seen:   make(map[string]struct{}),
	}
}

func (s *Service) Name() string { return serviceName }

func (s *Service) Start(ctx context.Context) error {
	if s == nil || !s.conf.Enabled || s.client == nil {
		return nil
	}

	interval := s.conf.PollInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}

	s.log.Infow("solana watcher started", "poll_interval", interval.String())

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if err := s.poll(ctx); err != nil {
			s.log.Errorw("solana watcher poll failed", "error", err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *Service) Stop(_ context.Context) error { return nil }

func (s *Service) Ping(ctx context.Context) error {
	if s.client == nil {
		return nil
	}
	return s.client.Ping(ctx)
}

func (s *Service) poll(ctx context.Context) error {
	hotWallets, err := s.bs.Wallets().Hot().GetAll(ctx)
	if err != nil {
		return fmt.Errorf("list hot wallets: %w", err)
	}

	addresses := make([]string, 0)
	for _, w := range hotWallets {
		if w.Blockchain == wconstants.BlockchainTypeSolana {
			addresses = append(addresses, w.Address)
		}
	}

	for _, addr := range addresses {
		if err := s.pollAddress(ctx, addr); err != nil {
			s.log.Errorw("poll address failed", "address", addr, "error", err)
		}
	}

	return nil
}

func (s *Service) pollAddress(ctx context.Context, address string) error {
	pk, err := solanago.PublicKeyFromBase58(address)
	if err != nil {
		return fmt.Errorf("invalid address %s: %w", address, err)
	}

	sigs, err := s.client.GetSignatures(ctx, pk, 20)
	if err != nil {
		return err
	}

	for _, sig := range sigs {
		sigStr := sig.String()
		if s.alreadySeen(sigStr) {
			continue
		}

		if err := s.processSignature(ctx, address, sig); err != nil {
			s.log.Errorw("process signature failed", "signature", sigStr, "error", err)
			continue
		}

		s.markSeen(sigStr)
	}

	return nil
}

func (s *Service) alreadySeen(sig string) bool {
	s.seenMu.Lock()
	defer s.seenMu.Unlock()
	_, ok := s.seen[sig]
	return ok
}

func (s *Service) markSeen(sig string) {
	s.seenMu.Lock()
	defer s.seenMu.Unlock()
	s.seen[sig] = struct{}{}

	if len(s.seen) > 10_000 {
		s.seen = make(map[string]struct{})
	}
}

type parsedTransfer struct {
	from    string
	to      string
	amount  decimal.Decimal
	assetID string
	uniqKey string
}

func (s *Service) processSignature(ctx context.Context, watchedAddress string, sig solanago.Signature) error {
	txResult, err := s.client.Node().GetTransaction(ctx, sig, &rpc.GetTransactionOpts{
		Encoding:                       solanago.EncodingBase64,
		Commitment:                     s.client.Commitment(),
		MaxSupportedTransactionVersion: utils.Pointer(uint64(0)),
	})
	if err != nil {
		return fmt.Errorf("get transaction: %w", err)
	}
	if txResult == nil || txResult.Transaction == nil {
		return nil
	}

	transfers, err := parseTransfers(txResult, watchedAddress)
	if err != nil {
		return err
	}
	if len(transfers) == 0 {
		return nil
	}

	checkResult, err := s.bs.Wallets().CheckWallet(ctx, wconstants.BlockchainTypeSolana, watchedAddress)
	if err != nil {
		if errors.Is(err, wallets.ErrAddressNotFound) {
			return nil
		}
		return fmt.Errorf("check wallet: %w", err)
	}

	return pgx.BeginTxFunc(ctx, s.store.PSQLConn(), pgx.TxOptions{}, func(dbTx pgx.Tx) error {
		batch := make([]webhooks.BatchCreateParams, 0, len(transfers))
		for _, tr := range transfers {
			confirmations := uint64(1)
			fee := ""
			if txResult.Meta != nil {
				fee = solana.LamportsToSOL(txResult.Meta.Fee).String()
			}

			from := tr.from
			to := tr.to
			asset := tr.assetID
			uniq := tr.uniqKey
			value := tr.amount.String()

			params, err := s.bs.Webhooks().EventTransactionCreateParams(ctx, webhooks.EventTransactionCreateParamsData{
				Blockchain: wconstants.BlockchainTypeSolana,
				Tx: webhooks.TransactionData{
					Hash:          sig.String(),
					Confirmations: confirmations,
					Fee:           &fee,
				},
				Event: webhooks.TransactionEventData{
					AddressFrom:       &from,
					AddressTo:         &to,
					Value:             &value,
					AssetIdentify:     &asset,
					BlockchainUniqKey: &uniq,
				},
				WebhookKind:      models.WebhookKindDeposit,
				WalletType:       checkResult.WalletType,
				WebhookStatus:    models.WebhookEventStatusWaitingConfirmations,
				OwnerID:          checkResult.OwnerID,
				ExternalWalletID: checkResult.ExternalWalletID,
			})
			if err != nil {
				return fmt.Errorf("prepare webhook params: %w", err)
			}
			batch = append(batch, params)
		}

		if len(batch) == 0 {
			return nil
		}

		return s.bs.Webhooks().BatchCreate(ctx, batch, repos.WithTx(dbTx))
	})
}

// parseTransfers extracts SOL / SPL transfers that credit watchedAddress via balance diffs.
func parseTransfers(txResult *rpc.GetTransactionResult, watchedAddress string) ([]parsedTransfer, error) {
	if txResult.Meta == nil {
		return nil, nil
	}

	out := make([]parsedTransfer, 0)

	if txResult.Transaction != nil {
		tx, err := txResult.Transaction.GetTransaction()
		if err == nil && tx != nil {
			keys := tx.Message.AccountKeys
			pre := txResult.Meta.PreBalances
			post := txResult.Meta.PostBalances
			if len(keys) == len(pre) && len(pre) == len(post) {
				for i, key := range keys {
					if key.String() != watchedAddress {
						continue
					}
					if post[i] <= pre[i] {
						continue
					}
					delta := post[i] - pre[i]
					from := ""
					var maxDebit uint64
					for j := range keys {
						if pre[j] > post[j] {
							debit := pre[j] - post[j]
							if debit > maxDebit {
								maxDebit = debit
								from = keys[j].String()
							}
						}
					}
					sigStr := ""
					if len(tx.Signatures) > 0 {
						sigStr = tx.Signatures[0].String()
					}
					out = append(out, parsedTransfer{
						from:    from,
						to:      watchedAddress,
						amount:  solana.LamportsToSOL(delta),
						assetID: solana.SolAssetIdentifier,
						uniqKey: fmt.Sprintf("%s:sol:%d", sigStr, i),
					})
				}
			}
		}
	}

	for _, postTB := range txResult.Meta.PostTokenBalances {
		if postTB.Owner == nil || postTB.Owner.String() != watchedAddress {
			continue
		}
		if postTB.UiTokenAmount == nil {
			continue
		}

		postAmount, err := decimal.NewFromString(postTB.UiTokenAmount.Amount)
		if err != nil {
			continue
		}

		var preAmount decimal.Decimal
		for _, preTB := range txResult.Meta.PreTokenBalances {
			if preTB.AccountIndex == postTB.AccountIndex && preTB.UiTokenAmount != nil {
				preAmount, _ = decimal.NewFromString(preTB.UiTokenAmount.Amount)
				break
			}
		}

		delta := postAmount.Sub(preAmount)
		if !delta.IsPositive() {
			continue
		}

		decimals := int64(postTB.UiTokenAmount.Decimals)
		out = append(out, parsedTransfer{
			to:      watchedAddress,
			amount:  solana.RawToTokens(uint64(delta.IntPart()), decimals), //nolint:gosec
			assetID: postTB.Mint.String(),
			uniqKey: fmt.Sprintf("spl:%d:%s", postTB.AccountIndex, postTB.Mint.String()),
		})
	}

	return out, nil
}
