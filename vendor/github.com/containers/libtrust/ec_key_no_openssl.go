//go:build !libtrust_openssl
// +build !libtrust_openssl

package libtrust

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"fmt"
	"io"
)

func (k *ecPrivateKey) sign(data io.Reader, hashID crypto.Hash) (sig []byte, err error) {
	hasher := k.signatureAlgorithm.HashID().New()
	_, err = io.Copy(hasher, data)
	if err != nil {
		return nil, fmt.Errorf("error reading data to sign: %s", err)
	}
	hash := hasher.Sum(nil)

	r, s, err := ecdsa.Sign(rand.Reader, k.PrivateKey, hash)
	if err != nil {
		return nil, err
	}

	rBytes, sBytes := r.Bytes(), s.Bytes()
	octetLength := (k.ecPublicKey.Params().BitSize + 7) >> 3
	// MUST include leading zeros in the output
	rBuf := make([]byte, octetLength-len(rBytes), octetLength)
	sBuf := make([]byte, octetLength-len(sBytes), octetLength)

	rBuf = append(rBuf, rBytes...)
	sBuf = append(sBuf, sBytes...)

	return append(rBuf, sBuf...), nil
}
