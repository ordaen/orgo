package utils

import (
	"crypto/rand"
	"math/big"
)

var chars = [4]string{"0123456789", "abcdefghijklmnopqrstuvwxyz", "ABCDEFGHIJKLMNOPQRSTUVWXYZ", "#@$%^*"}

// RandomNumber returns a random string of digits
func RandomNumber(size int) string {
	return randomChars(size, 1)
}

// RandomPassword returns a random string of digits, letters and symbols
func RandomPassword(size int) string {
	return randomChars(size, 4)
}

// RandomString returns a random string of digits and letters
func RandomString(size int) string {
	return randomChars(size, 3)
}

// randomChars returns size characters from the first chunks sets of chars, picking a set and then a character in it.
// It uses crypto/rand, the results are used as passwords and salts.
func randomChars(size, chunks int) string {
	buf := make([]byte, size)
	for i := range buf {
		set := chars[randNumber(chunks)]
		buf[i] = set[randNumber(len(set))]
	}
	return string(buf)
}

func randNumber(num int) int {
	if num <= 1 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(num)))
	if err != nil {
		// crypto/rand does not fail on supported platforms
		panic(err)
	}
	return int(n.Int64())
}
