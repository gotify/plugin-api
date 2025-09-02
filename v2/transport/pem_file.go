package transport

import (
	"encoding/pem"
	"io"
)

func IteratePEMFile(r io.Reader, callback func(block *pem.Block) (continueIterate bool, err error)) error {
	var bufferBytes []byte
	for {
		var buf [2048]byte
		n, err := r.Read(buf[:])
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		bufferBytes = append(bufferBytes, buf[:n]...)

		for block, rest := pem.Decode(bufferBytes); block != nil; block, rest = pem.Decode(rest) {
			continueIterate, err := callback(block)
			if err != nil {
				return err
			}
			if !continueIterate {
				return nil
			}
		}
	}
	return nil
}
