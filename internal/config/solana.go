package config

import (
	"fmt"
	"time"

	"github.com/dv-net/dv-processing/pkg/walletsdk/solana"
	"github.com/gagliardetto/solana-go/rpc"
)

// SolanaBlockchain holds Solana network configuration.
type SolanaBlockchain struct {
	Enabled      bool          `json:"enabled" yaml:"enabled" default:"false"`
	Node         SolanaNode    `json:"node" yaml:"node"`
	Commitment   string        `json:"commitment" yaml:"commitment" default:"confirmed" example:"confirmed / finalized"`
	PollInterval time.Duration `json:"poll_interval" yaml:"poll_interval" default:"5s" example:"5s"`
	MaxRetries   int           `json:"max_retries" yaml:"max_retries" default:"3" example:"3"`
}

// SolanaNode holds Solana RPC / WSS endpoints.
type SolanaNode struct {
	Address string `json:"address" yaml:"address" usage:"HTTP RPC URL"`
	WSSURL  string `json:"wss_url" yaml:"wss_url" usage:"WebSocket URL (optional)"`
}

// Validate checks Solana config when the chain is enabled.
func (s SolanaBlockchain) Validate() error {
	if !s.Enabled {
		return nil
	}

	if s.Node.Address == "" {
		return fmt.Errorf("solana: node address must not be empty")
	}

	switch s.Commitment {
	case "", "processed", "confirmed", "finalized":
	default:
		return fmt.Errorf("solana: invalid commitment %q", s.Commitment)
	}

	if s.PollInterval < 0 {
		return fmt.Errorf("solana: poll interval must not be negative")
	}

	if s.MaxRetries < 0 {
		return fmt.Errorf("solana: max retries must not be negative")
	}

	return nil
}

// IsEnabled returns true if Solana is enabled.
func (s SolanaBlockchain) IsEnabled() bool { return s.Enabled }

// ConvertToSDKConfig maps config to walletsdk Solana config.
func (s SolanaBlockchain) ConvertToSDKConfig(headers map[string]string) solana.Config {
	commitment := rpc.CommitmentType(s.Commitment)
	if commitment == "" {
		commitment = rpc.CommitmentConfirmed
	}

	return solana.Config{
		NodeAddr:   s.Node.Address,
		WSSURL:     s.Node.WSSURL,
		Commitment: commitment,
		Headers:    headers,
	}
}
