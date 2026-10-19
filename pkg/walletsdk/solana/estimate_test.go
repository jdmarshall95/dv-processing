package solana

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEstimateTransferNativeNoRPC(t *testing.T) {
	t.Parallel()

	// Without RPC client, EstimateTransfer for native SOL only needs fee math;
	// AccountExists is skipped for native asset.
	s := &Solana{config: Config{Commitment: "confirmed"}}

	res, err := s.EstimateTransfer(t.Context(), "11111111111111111111111111111111", "11111111111111111111111111111112", SolAssetIdentifier)
	require.NoError(t, err)
	require.Equal(t, LamportsPerSignature, res.FeeLamports)
	require.False(t, res.NeedCreateDestATA)
}
