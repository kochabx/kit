// Package ecies implements the Elliptic Curve Integrated Encryption Scheme (ECIES).
//
// ECIES is a hybrid encryption scheme that combines the security of elliptic curve
// cryptography with the efficiency of symmetric encryption. This implementation uses:
//   - NIST P-256 elliptic curve
//   - ECDH key agreement (using crypto/ecdh for constant-time operations)
//   - HKDF-SHA256 for key derivation
//   - AES-256-GCM for authenticated encryption
//
// Security Features:
//   - Constant-time ECDH implementation from crypto/ecdh (Go 1.20+)
//   - Authenticated encryption with AES-GCM
//   - Ephemeral keys for each encryption (perfect forward secrecy)
//   - Proper key derivation with HKDF
//
// Example usage:
//
//	// Generate a new key pair
//	privateKey, err := ecies.GenerateKey()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer privateKey.Destroy()
//
//	publicKey := privateKey.Public()
//
//	// Encrypt a message
//	plaintext := []byte("Hello, ECIES!")
//	ciphertext, err := ecies.Encrypt(publicKey, plaintext)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Decrypt the message
//	decrypted, err := ecies.Decrypt(privateKey, ciphertext)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// For key persistence:
//
//	// Generate and save key pair to files
//	err := ecies.GenerateKeyPair(
//	    ecies.WithDirpath("./keys"),
//	    ecies.WithPrivateKeyFilename("my_key.pem"),
//	)
//
//	// Load keys from files
//	privateKey, err := ecies.LoadPrivateKey("./keys/my_key.pem")
//	publicKey, err := ecies.LoadPublicKey("./keys/public.pem")
package ecies
