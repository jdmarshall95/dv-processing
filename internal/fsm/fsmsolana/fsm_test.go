package fsmsolana

import (
	"testing"

	"github.com/dv-net/dv-processing/pkg/walletsdk/solana"
	"github.com/stretchr/testify/require"
)

func TestExitOnFailedSteps(t *testing.T) {
	t.Parallel()

	require.Contains(t, exitOnFailedSteps[stageBeforeSending], stepValidateRequest)
	require.Contains(t, exitOnFailedSteps[stageSending], stepSending)
}

func TestLamportsMathUsedByFSM(t *testing.T) {
	t.Parallel()

	require.Equal(t, "0.000005", solana.LamportsToSOL(solana.LamportsPerSignature).String())
	require.True(t, solana.ValidateAddress(solana.USDCMintMainnet))
}
