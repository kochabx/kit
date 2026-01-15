package ecies

import (
	"crypto/rand"
	"testing"
)

// BenchmarkEncrypt benchmarks the encryption operation
func BenchmarkEncrypt(b *testing.B) {
	privateKey, err := GenerateKey()
	if err != nil {
		b.Fatalf("Failed to generate key: %v", err)
	}
	defer privateKey.Destroy()

	publicKey := privateKey.Public()
	plaintext := make([]byte, 1024) // 1 KB
	rand.Read(plaintext)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := Encrypt(publicKey, plaintext)
		if err != nil {
			b.Fatalf("Encryption failed: %v", err)
		}
	}
}

// BenchmarkDecrypt benchmarks the decryption operation
func BenchmarkDecrypt(b *testing.B) {
	privateKey, err := GenerateKey()
	if err != nil {
		b.Fatalf("Failed to generate key: %v", err)
	}
	defer privateKey.Destroy()

	plaintext := make([]byte, 1024) // 1 KB
	rand.Read(plaintext)

	ciphertext, err := Encrypt(privateKey.Public(), plaintext)
	if err != nil {
		b.Fatalf("Encryption failed: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := Decrypt(privateKey, ciphertext)
		if err != nil {
			b.Fatalf("Decryption failed: %v", err)
		}
	}
}

// BenchmarkKeyGeneration benchmarks key pair generation
func BenchmarkKeyGeneration(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		key, err := GenerateKey()
		if err != nil {
			b.Fatalf("Key generation failed: %v", err)
		}
		key.Destroy()
	}
}

// BenchmarkECDH benchmarks the ECDH operation
func BenchmarkECDH(b *testing.B) {
	privateKey1, _ := GenerateKey()
	defer privateKey1.Destroy()
	privateKey2, _ := GenerateKey()
	defer privateKey2.Destroy()

	publicKey2 := privateKey2.Public()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := privateKey1.Encapsulate(publicKey2)
		if err != nil {
			b.Fatalf("ECDH failed: %v", err)
		}
	}
}

// BenchmarkEncryptSizes benchmarks encryption with different data sizes
func BenchmarkEncryptSizes(b *testing.B) {
	privateKey, _ := GenerateKey()
	defer privateKey.Destroy()
	publicKey := privateKey.Public()

	sizes := []int{
		64,      // 64 B
		1024,    // 1 KB
		10240,   // 10 KB
		102400,  // 100 KB
		1048576, // 1 MB
	}

	for _, size := range sizes {
		plaintext := make([]byte, size)
		rand.Read(plaintext)

		b.Run(formatSize(size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := Encrypt(publicKey, plaintext)
				if err != nil {
					b.Fatalf("Encryption failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkDecryptSizes benchmarks decryption with different data sizes
func BenchmarkDecryptSizes(b *testing.B) {
	privateKey, _ := GenerateKey()
	defer privateKey.Destroy()

	sizes := []int{
		64,
		1024,
		10240,
		102400,
		1048576,
	}

	for _, size := range sizes {
		plaintext := make([]byte, size)
		rand.Read(plaintext)

		ciphertext, _ := Encrypt(privateKey.Public(), plaintext)

		b.Run(formatSize(size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := Decrypt(privateKey, ciphertext)
				if err != nil {
					b.Fatalf("Decryption failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkPublicKeyBytes benchmarks public key encoding
func BenchmarkPublicKeyBytes(b *testing.B) {
	privateKey, _ := GenerateKey()
	defer privateKey.Destroy()
	publicKey := privateKey.Public()

	b.Run("uncompressed", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = publicKey.Bytes(false)
		}
	})

	b.Run("compressed", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = publicKey.Bytes(true)
		}
	})
}

// BenchmarkKeyEquality benchmarks key comparison
func BenchmarkKeyEquality(b *testing.B) {
	key1, _ := GenerateKey()
	defer key1.Destroy()
	key2, _ := GenerateKey()
	defer key2.Destroy()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = key1.Equals(key2)
	}
}

// formatSize formats byte size for benchmark names
func formatSize(bytes int) string {
	switch {
	case bytes < 1024:
		return "64B"
	case bytes < 10240:
		return "1KB"
	case bytes < 102400:
		return "10KB"
	case bytes < 1048576:
		return "100KB"
	default:
		return "1MB"
	}
}
