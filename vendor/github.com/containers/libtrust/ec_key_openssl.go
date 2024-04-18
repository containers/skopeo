//go:build libtrust_openssl
// +build libtrust_openssl

package libtrust

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"fmt"
	"io"
)

func (k *ecPrivateKey) sign(data io.Reader, hashID crypto.Hash) (sig []byte, err error) {
	hId := k.signatureAlgorithm.HashID()
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(data)
	if err != nil {
		return nil, fmt.Errorf("error reading data: %s", err)
	}

	return ecdsa.HashSignECDSA(k.PrivateKey, hId, buf.Bytes())
}
