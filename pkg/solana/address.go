package solana

import (
	"crypto/sha256"
	"fmt"

	"filippo.io/edwards25519"
)

const (
	associatedTokenProgramID = "ATokenGPvbdGVxr1cW5xWH25efTNsLJA8knL"
	tokenProgramID           = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
	token2022ProgramID       = "TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb"
	TokenProgramID           = tokenProgramID
	Token2022ProgramID       = token2022ProgramID
)

var pdaMarker = []byte("ProgramDerivedAddress")

func DeriveAssociatedTokenAddress(owner string, mint string, tokenProgram string) (string, error) {
	if tokenProgram == "" {
		tokenProgram = tokenProgramID
	}
	ownerBytes, err := DecodeBase58(owner)
	if err != nil {
		return "", fmt.Errorf("decode owner address: %w", err)
	}
	mintBytes, err := DecodeBase58(mint)
	if err != nil {
		return "", fmt.Errorf("decode mint address: %w", err)
	}
	tokenProgramBytes, err := DecodeBase58(tokenProgram)
	if err != nil {
		return "", fmt.Errorf("decode token program address: %w", err)
	}
	programIDBytes, err := DecodeBase58(associatedTokenProgramID)
	if err != nil {
		return "", fmt.Errorf("decode associated token program address: %w", err)
	}

	addr, err := findProgramAddress([][]byte{ownerBytes, tokenProgramBytes, mintBytes}, programIDBytes)
	if err != nil {
		return "", err
	}
	return EncodeBase58(addr), nil
}

func findProgramAddress(seeds [][]byte, programID []byte) ([]byte, error) {
	seedsWithBump := make([][]byte, 0, len(seeds)+1)
	seedsWithBump = append(seedsWithBump, seeds...)
	for bump := uint8(255); ; bump-- {
		addr := createProgramAddress(append(seedsWithBump, []byte{bump}), programID)
		if !isOnCurve(addr) {
			return addr, nil
		}
		if bump == 0 {
			break
		}
	}
	return nil, fmt.Errorf("unable to find a viable program address")
}

func createProgramAddress(seeds [][]byte, programID []byte) []byte {
	hash := sha256.New()
	for _, seed := range seeds {
		hash.Write(seed)
	}
	hash.Write(programID)
	hash.Write(pdaMarker)
	return hash.Sum(nil)
}

func isOnCurve(point []byte) bool {
	if len(point) != 32 {
		return false
	}
	_, err := new(edwards25519.Point).SetBytes(point)
	return err == nil
}
