package solana

import "errors"

var (
	// ErrInvalidAddress is returned when a Solana address fails validation.
	ErrInvalidAddress = errors.New("invalid solana address")

	// ErrInsufficientBalance is returned when an account lacks lamports or tokens.
	ErrInsufficientBalance = errors.New("insufficient balance")

	// ErrEmptyMnemonic is returned when mnemonic is required but empty.
	ErrEmptyMnemonic = errors.New("mnemonic is empty")

	// ErrAddressMismatch is returned when a derived address does not match the expected one.
	ErrAddressMismatch = errors.New("derived address does not match")

	// ErrRetryable indicates a transient RPC / network failure that can be retried.
	ErrRetryable = errors.New("retryable solana error")
)
