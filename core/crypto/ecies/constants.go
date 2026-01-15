package ecies

// Curve parameters for NIST P-256 elliptic curve
const (
	// CurvePointSize is the size in bytes of each coordinate (X or Y) on the P-256 curve
	CurvePointSize = 32

	// Point compression prefixes
	UncompressedPointTag = 0x04 // Uncompressed point format: 0x04 || X || Y
	CompressedEvenTag    = 0x02 // Compressed point with even Y coordinate
	CompressedOddTag     = 0x03 // Compressed point with odd Y coordinate
)

// Encryption algorithm parameters
const (
	// PublicKeyBytes is the size of an uncompressed public key in bytes
	// Format: [tag:1][X:32][Y:32]
	PublicKeyBytes = 1 + CurvePointSize + CurvePointSize // 65 bytes

	// AESKeySize is the size of the AES-256 symmetric key
	AESKeySize = 32 // 256 bits

	// AESGCMNonceSize is the size of the AES-GCM nonce (IV)
	AESGCMNonceSize = 16 // 128 bits

	// AESGCMTagSize is the size of the AES-GCM authentication tag
	AESGCMTagSize = 16 // 128 bits
)

// Ciphertext format parameters
const (
	// MinCiphertextSize is the minimum valid ciphertext size
	// Format: [version:1][ephemeral_pubkey:65][nonce:16][tag:16][encrypted_data:>=1]
	MinCiphertextSize = 1 + PublicKeyBytes + AESGCMNonceSize + AESGCMTagSize + 1

	// Ciphertext structure offsets (for version 1)
	offsetVersion       = 0
	offsetEphemeralKey  = 1
	offsetNonce         = offsetEphemeralKey + PublicKeyBytes
	offsetTag           = offsetNonce + AESGCMNonceSize
	offsetEncryptedData = offsetTag + AESGCMTagSize
)

// Protocol version
const (
	// CurrentVersion is the current protocol version
	CurrentVersion byte = 0x01
)
