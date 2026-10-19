# Solana Wallet SDK

HD wallets and RPC helpers for Solana (native SOL and SPL tokens).

## Derivation

Addresses are derived from BIP39 mnemonics with SLIP-0010 ed25519:

```text
m/44'/501'/{sequence}'/0'
```

(Phantom-compatible path.)

## Usage

```go
client, err := solana.NewSolana(solana.Config{
    NodeAddr:   "https://api.mainnet-beta.solana.com",
    Commitment: rpc.CommitmentConfirmed,
})

addr, priv, err := solana.WalletPubKeyHash(mnemonic, passphrase, 0)

sig, tx, err := client.SendSOL(ctx, priv, to, lamports, "payment-ref")
sig, tx, err := client.SendSPLToken(ctx, priv, to, mint, amount, decimals, "payment-ref")
```

Native asset identifier: `sol`. SPL assets use the mint address (e.g. USDC `EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`).
