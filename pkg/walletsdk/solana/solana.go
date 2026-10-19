package solana

import (
	"context"
	"errors"
	"fmt"
	"time"

	solanago "github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// Config holds Solana RPC client configuration.
type Config struct {
	NodeAddr   string
	WSSURL     string
	Commitment rpc.CommitmentType
	Headers    map[string]string
}

// Validate checks if the config is valid.
func (c *Config) Validate() error {
	if c.NodeAddr == "" {
		return errors.New("node address is required")
	}
	return nil
}

// Solana is the RPC client wrapper used by FSM and services.
type Solana struct {
	node   *rpc.Client
	config Config
}

// NewSolana creates a Solana RPC client.
func NewSolana(conf Config) (*Solana, error) {
	if err := conf.Validate(); err != nil {
		return nil, err
	}

	if conf.Commitment == "" {
		conf.Commitment = rpc.CommitmentConfirmed
	}

	var client *rpc.Client
	if len(conf.Headers) > 0 {
		client = rpc.NewWithHeaders(conf.NodeAddr, conf.Headers)
	} else {
		client = rpc.New(conf.NodeAddr)
	}

	return &Solana{
		node:   client,
		config: conf,
	}, nil
}

// Name returns the service name.
func (s *Solana) Name() string { return "solana-service" }

// Node returns the underlying RPC client.
func (s *Solana) Node() *rpc.Client { return s.node }

// Commitment returns the configured commitment level.
func (s *Solana) Commitment() rpc.CommitmentType { return s.config.Commitment }

// Start is a no-op for HTTP RPC clients.
func (s *Solana) Start(_ context.Context) error { return nil }

// Stop is a no-op for HTTP RPC clients.
func (s *Solana) Stop(_ context.Context) error { return nil }

// FindATA derives the Associated Token Account address for owner+mint.
func (s *Solana) FindATA(owner, mint solanago.PublicKey) (solanago.PublicKey, error) {
	ata, _, err := solanago.FindAssociatedTokenAddress(owner, mint)
	if err != nil {
		return solanago.PublicKey{}, fmt.Errorf("find ATA: %w", err)
	}
	return ata, nil
}

// GetSignatures returns recent confirmed signatures for an address.
func (s *Solana) GetSignatures(ctx context.Context, address solanago.PublicKey, limit int) ([]solanago.Signature, error) {
	if limit <= 0 {
		limit = 20
	}

	lim := limit
	out, err := s.node.GetSignaturesForAddressWithOpts(
		ctx,
		address,
		&rpc.GetSignaturesForAddressOpts{
			Limit:      &lim,
			Commitment: s.config.Commitment,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("%w: get signatures: %w", ErrRetryable, err)
	}

	sigs := make([]solanago.Signature, 0, len(out))
	for _, item := range out {
		sigs = append(sigs, item.Signature)
	}
	return sigs, nil
}

// GetSignatureStatuses returns confirmation statuses for the given signatures.
func (s *Solana) GetSignatureStatuses(ctx context.Context, sigs ...solanago.Signature) ([]*rpc.SignatureStatusesResult, error) {
	out, err := s.node.GetSignatureStatuses(ctx, true, sigs...)
	if err != nil {
		return nil, fmt.Errorf("%w: get signature statuses: %w", ErrRetryable, err)
	}
	if out == nil {
		return nil, fmt.Errorf("empty signature statuses response")
	}
	return out.Value, nil
}

// GetLatestBlockhash fetches a recent blockhash for building transactions.
func (s *Solana) GetLatestBlockhash(ctx context.Context) (solanago.Hash, error) {
	res, err := s.node.GetLatestBlockhash(ctx, s.config.Commitment)
	if err != nil {
		return solanago.Hash{}, fmt.Errorf("%w: get latest blockhash: %w", ErrRetryable, err)
	}
	return res.Value.Blockhash, nil
}

// AccountExists checks whether an account is present on-chain.
func (s *Solana) AccountExists(ctx context.Context, address solanago.PublicKey) (bool, error) {
	info, err := s.node.GetAccountInfoWithOpts(ctx, address, &rpc.GetAccountInfoOpts{
		Commitment: s.config.Commitment,
	})
	if err != nil {
		return false, fmt.Errorf("%w: get account info: %w", ErrRetryable, err)
	}
	return info != nil && info.Value != nil, nil
}

// Ping checks RPC connectivity.
func (s *Solana) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := s.node.GetHealth(ctx)
	if err != nil {
		return fmt.Errorf("solana rpc health: %w", err)
	}
	return nil
}
