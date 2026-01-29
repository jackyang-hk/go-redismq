package go_redismq

import (
	"encoding/base64"
	"fmt"
	"math/rand"
	"runtime"
	"time"
)

func GetLineSeparator() string {
	switch runtime.GOOS {
	case "windows":
		return "\r\n"
	default:
		return "\n"
	}
}

func CurrentTimeMillis() (s int64) {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

func GenerateRandomAlphanumeric(length int) string {
	//rand.Seed(time.Now().UnixNano())
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	result := make([]byte, length)
	for i := range result {
		result[i] = charset[rand.Intn(len(charset))]
	}

	return string(result)
}

func JodaTimePrefix() (prefix string) {
	return time.Now().Format("20060102")
}

const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

var seededRand = rand.New(rand.NewSource(time.Now().UnixNano()))

func GenerateRandomCode(length int) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}

	return string(b)
}

func GenerateRandomNumber(length int) string {
	return fmt.Sprintf("%06v", rand.New(rand.NewSource(time.Now().UnixNano())).Int31n(1000000))
}

func GenerateRandomOpenApiKey(length int) (string, error) {
	// Create a byte slice to hold the random bytes
	key := make([]byte, length)

	// Read random bytes from crypto/rand into the byte slice
	_, err := rand.Read(key)
	if err != nil {
		return "", err
	}

	// Encode the random bytes to base64 to get a string representation
	encodedKey := base64.URLEncoding.EncodeToString(key)

	// Truncate the encoded string to the desired length
	// (base64 encoding increases the length by approximately 33%)
	return encodedKey[:length], nil
}
