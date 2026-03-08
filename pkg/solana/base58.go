package solana

import (
	"errors"
	"math/big"
	"strings"
)

const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

var (
	bigRadix  = big.NewInt(58)
	bigZero   = big.NewInt(0)
	indexes   = buildAlphabetIndexes()
	ErrBase58 = errors.New("invalid base58 payload")
)

func buildAlphabetIndexes() map[rune]int {
	out := make(map[rune]int, len(alphabet))
	for i, ch := range alphabet {
		out[ch] = i
	}
	return out
}

func EncodeBase58(input []byte) string {
	if len(input) == 0 {
		return ""
	}

	x := new(big.Int).SetBytes(input)
	var out []byte
	for x.Cmp(bigZero) > 0 {
		mod := new(big.Int)
		x.DivMod(x, bigRadix, mod)
		out = append(out, alphabet[mod.Int64()])
	}

	for _, b := range input {
		if b != 0 {
			break
		}
		out = append(out, alphabet[0])
	}

	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func DecodeBase58(input string) ([]byte, error) {
	if strings.TrimSpace(input) == "" {
		return nil, ErrBase58
	}

	result := big.NewInt(0)
	for _, ch := range input {
		idx, ok := indexes[ch]
		if !ok {
			return nil, ErrBase58
		}
		result.Mul(result, bigRadix)
		result.Add(result, big.NewInt(int64(idx)))
	}

	decoded := result.Bytes()
	leadingZeros := 0
	for _, ch := range input {
		if ch != rune(alphabet[0]) {
			break
		}
		leadingZeros++
	}

	if leadingZeros == 0 {
		return decoded, nil
	}

	output := make([]byte, leadingZeros+len(decoded))
	copy(output[leadingZeros:], decoded)
	return output, nil
}
