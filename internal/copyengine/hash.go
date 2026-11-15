package copyengine

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha3"
	"crypto/sha512"
	"fmt"
	"hash"

	"github.com/zeebo/xxh3"
)

type HashMode string

const (
	HashXXH3     HashMode = "xxh3"
	HashSHA256   HashMode = "sha256"
	HashSHA512   HashMode = "sha512"
	HashSHA3_256 HashMode = "sha3-256"
	HashSHA3_512 HashMode = "sha3-512"
	HashSHA1     HashMode = "sha1"
	HashMD5      HashMode = "md5"
)

func newHash(mode HashMode) (hash.Hash, error) {
	switch mode {
	case "", HashXXH3:
		return xxh3.New(), nil
	case HashSHA256:
		return sha256.New(), nil
	case HashSHA512:
		return sha512.New(), nil
	case HashSHA3_256:
		return sha3.New256(), nil
	case HashSHA3_512:
		return sha3.New512(), nil
	case HashSHA1:
		return sha1.New(), nil
	case HashMD5:
		return md5.New(), nil
	default:
		return nil, fmt.Errorf("unknown hash algorithm: %s", mode)
	}
}
