package dbdoc

// Cipher identifiers. They are the values the document model and the settings of a
// database carry; internal/gkdb maps them onto the algorithms of the container.
var (
	// CipherAES256 is the default: AES-256 in the container.
	CipherAES256 = MustParseUUID("31C1F2E6BF714350BE5805216AFC5AFF")
	// CipherChaCha20 selects ChaCha20 in the container.
	CipherChaCha20 = MustParseUUID("D6038A2B8B6F4CB5A524339A31DBB59A")
	// CipherTwoFish is kept for the tag of a database written by an older build.
	CipherTwoFish = MustParseUUID("AD68F29F576F4BB9A36AD47AF965346C")
)

// Compression algorithms of the container payload.
const (
	CompressionNone = 0
	CompressionGzip = 1
)
