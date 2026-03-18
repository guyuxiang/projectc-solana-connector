package solana

import "testing"

func TestAssociatedTokenProgramID(t *testing.T) {
	const want = "ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL"
	if associatedTokenProgramID != want {
		t.Fatalf("unexpected associated token program id: got %q want %q", associatedTokenProgramID, want)
	}
}

func TestDeriveAssociatedTokenAddressRejectsInvalidOwner(t *testing.T) {
	_, err := DeriveAssociatedTokenAddress("invalid-owner", "So11111111111111111111111111111111111111112", TokenProgramID)
	if err == nil {
		t.Fatal("expected invalid owner to fail")
	}
}
