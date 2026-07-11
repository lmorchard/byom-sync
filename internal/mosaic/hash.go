package mosaic

import (
	"crypto/sha256"
	"encoding/hex"
)

// layoutVersion is folded into every mosaic hash so changing the layout logic
// invalidates cached URLs. Bump when Plan's geometry/rules change.
const layoutVersion = "v1"

// Name returns the content-addressed filename ("<sha256>.jpg") for a mosaic
// built from coverPaths in order. Deterministic; order-sensitive.
func Name(coverPaths []string) string {
	h := sha256.New()
	h.Write([]byte(layoutVersion))
	for _, p := range coverPaths {
		h.Write([]byte{0})
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil)) + ".jpg"
}
