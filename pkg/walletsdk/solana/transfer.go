package solana

import (
	"context"
	"fmt"

	solanago "github.com/gagliardetto/solana-go"
	associatedtokenaccount "github.com/gagliardetto/solana-go/programs/associated-token-account"
	"github.com/gagliardetto/solana-go/programs/memo"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/programs/token"
	"github.com/gagliardetto/solana-go/rpc"
)

// SendSOL transfers native SOL (lamports) from the signer to the destination.
func (s *Solana) SendSOL(
	ctx context.Context,
	from solanago.PrivateKey,
	to solanago.PublicKey,
	amount uint64,
	memoText string,
) (solanago.Signature, *solanago.Transaction, error) {
	if amount == 0 {
		return solanago.Signature{}, nil, fmt.Errorf("amount must be greater than 0")
	}

	instructions := []solanago.Instruction{
		system.NewTransferInstruction(amount, from.PublicKey(), to).Build(),
	}

	if memoText != "" {
		instructions = append(instructions, memo.NewMemoInstruction([]byte(memoText), from.PublicKey()).Build())
	}

	return s.signAndSend(ctx, from, instructions)
}

// SendSPLToken transfers SPL tokens from the signer's ATA to the destination owner's ATA.
// If the destination ATA does not exist, a CreateIdempotent instruction is prepended (payer = from).
func (s *Solana) SendSPLToken(
	ctx context.Context,
	from solanago.PrivateKey,
	to solanago.PublicKey,
	mint solanago.PublicKey,
	amount uint64,
	decimals uint8,
	memoText string,
) (solanago.Signature, *solanago.Transaction, error) {
	if amount == 0 {
		return solanago.Signature{}, nil, fmt.Errorf("amount must be greater than 0")
	}

	fromATA, err := s.FindATA(from.PublicKey(), mint)
	if err != nil {
		return solanago.Signature{}, nil, err
	}

	toATA, err := s.FindATA(to, mint)
	if err != nil {
		return solanago.Signature{}, nil, err
	}

	instructions := make([]solanago.Instruction, 0, 3)

	toATAExists, err := s.AccountExists(ctx, toATA)
	if err != nil {
		return solanago.Signature{}, nil, err
	}
	if !toATAExists {
		instructions = append(instructions,
			associatedtokenaccount.NewCreateIdempotentInstruction(
				from.PublicKey(),
				to,
				mint,
			).Build(),
		)
	}

	instructions = append(instructions,
		token.NewTransferCheckedInstruction(
			amount,
			decimals,
			fromATA,
			mint,
			toATA,
			from.PublicKey(),
			[]solanago.PublicKey{},
		).Build(),
	)

	if memoText != "" {
		instructions = append(instructions, memo.NewMemoInstruction([]byte(memoText), from.PublicKey()).Build())
	}

	return s.signAndSend(ctx, from, instructions)
}

// CreateATA creates an Associated Token Account for owner+mint, paid by payer.
func (s *Solana) CreateATA(
	ctx context.Context,
	payer solanago.PrivateKey,
	owner, mint solanago.PublicKey,
) (solanago.Signature, solanago.PublicKey, error) {
	ata, err := s.FindATA(owner, mint)
	if err != nil {
		return solanago.Signature{}, solanago.PublicKey{}, err
	}

	exists, err := s.AccountExists(ctx, ata)
	if err != nil {
		return solanago.Signature{}, solanago.PublicKey{}, err
	}
	if exists {
		return solanago.Signature{}, ata, nil
	}

	sig, _, err := s.signAndSend(ctx, payer, []solanago.Instruction{
		associatedtokenaccount.NewCreateIdempotentInstruction(
			payer.PublicKey(),
			owner,
			mint,
		).Build(),
	})
	return sig, ata, err
}

func (s *Solana) signAndSend(
	ctx context.Context,
	signer solanago.PrivateKey,
	instructions []solanago.Instruction,
) (solanago.Signature, *solanago.Transaction, error) {
	blockhash, err := s.GetLatestBlockhash(ctx)
	if err != nil {
		return solanago.Signature{}, nil, err
	}

	tx, err := solanago.NewTransaction(
		instructions,
		blockhash,
		solanago.TransactionPayer(signer.PublicKey()),
	)
	if err != nil {
		return solanago.Signature{}, nil, fmt.Errorf("build transaction: %w", err)
	}

	_, err = tx.Sign(func(key solanago.PublicKey) *solanago.PrivateKey {
		if key.Equals(signer.PublicKey()) {
			return &signer
		}
		return nil
	})
	if err != nil {
		return solanago.Signature{}, nil, fmt.Errorf("sign transaction: %w", err)
	}

	sig, err := s.node.SendTransactionWithOpts(ctx, tx, rpc.TransactionOpts{
		SkipPreflight:       false,
		PreflightCommitment: s.config.Commitment,
	})
	if err != nil {
		return solanago.Signature{}, nil, fmt.Errorf("send transaction: %w", err)
	}

	return sig, tx, nil
}

// BroadcastSigned sends an already-signed transaction.
func (s *Solana) BroadcastSigned(ctx context.Context, tx *solanago.Transaction) (solanago.Signature, error) {
	sig, err := s.node.SendTransactionWithOpts(ctx, tx, rpc.TransactionOpts{
		SkipPreflight:       false,
		PreflightCommitment: s.config.Commitment,
	})
	if err != nil {
		return solanago.Signature{}, fmt.Errorf("send transaction: %w", err)
	}
	return sig, nil
}

// BuildSOLTransfer builds (but does not send) a native SOL transfer transaction.
func (s *Solana) BuildSOLTransfer(
	ctx context.Context,
	from solanago.PrivateKey,
	to solanago.PublicKey,
	amount uint64,
	memoText string,
) (*solanago.Transaction, error) {
	instructions := []solanago.Instruction{
		system.NewTransferInstruction(amount, from.PublicKey(), to).Build(),
	}
	if memoText != "" {
		instructions = append(instructions, memo.NewMemoInstruction([]byte(memoText), from.PublicKey()).Build())
	}

	blockhash, err := s.GetLatestBlockhash(ctx)
	if err != nil {
		return nil, err
	}

	tx, err := solanago.NewTransaction(
		instructions,
		blockhash,
		solanago.TransactionPayer(from.PublicKey()),
	)
	if err != nil {
		return nil, fmt.Errorf("build transaction: %w", err)
	}

	_, err = tx.Sign(func(key solanago.PublicKey) *solanago.PrivateKey {
		if key.Equals(from.PublicKey()) {
			return &from
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("sign transaction: %w", err)
	}

	return tx, nil
}

// BuildSPLTransfer builds (but does not send) an SPL token transfer, optionally creating the destination ATA.
func (s *Solana) BuildSPLTransfer(
	ctx context.Context,
	from solanago.PrivateKey,
	to solanago.PublicKey,
	mint solanago.PublicKey,
	amount uint64,
	decimals uint8,
	memoText string,
	createDestATA bool,
) (*solanago.Transaction, error) {
	fromATA, err := s.FindATA(from.PublicKey(), mint)
	if err != nil {
		return nil, err
	}
	toATA, err := s.FindATA(to, mint)
	if err != nil {
		return nil, err
	}

	instructions := make([]solanago.Instruction, 0, 3)
	if createDestATA {
		instructions = append(instructions,
			associatedtokenaccount.NewCreateIdempotentInstruction(from.PublicKey(), to, mint).Build(),
		)
	}
	instructions = append(instructions,
		token.NewTransferCheckedInstruction(
			amount, decimals, fromATA, mint, toATA, from.PublicKey(), []solanago.PublicKey{},
		).Build(),
	)
	if memoText != "" {
		instructions = append(instructions, memo.NewMemoInstruction([]byte(memoText), from.PublicKey()).Build())
	}

	blockhash, err := s.GetLatestBlockhash(ctx)
	if err != nil {
		return nil, err
	}

	tx, err := solanago.NewTransaction(
		instructions,
		blockhash,
		solanago.TransactionPayer(from.PublicKey()),
	)
	if err != nil {
		return nil, fmt.Errorf("build transaction: %w", err)
	}

	_, err = tx.Sign(func(key solanago.PublicKey) *solanago.PrivateKey {
		if key.Equals(from.PublicKey()) {
			return &from
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("sign transaction: %w", err)
	}

	return tx, nil
}
