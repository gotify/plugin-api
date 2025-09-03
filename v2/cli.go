package plugin

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/gotify/plugin-api/v2/transport"
)

// / PluginCli implements the CLI interface for a Gotify plugin.
type PluginCli struct {
	flagSet     *flag.FlagSet
	KexReqFile  *os.File
	KexRespFile *os.File
	Debug       bool
}

// ParsePluginCli parses the CLI arguments and returns a PluginCli instance.
func ParsePluginCli(args []string) (*PluginCli, error) {
	flagSet := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	var kexReqFileName string
	var kexRespFileName string
	var debug bool
	flagSet.StringVar(&kexReqFileName, "kex-req-file", os.Getenv("GOTIFY_PLUGIN_KEX_REQ_FILE"), "File name for the key exchange for Transport Auth. /proc/self/fd/* can be used to open a file descriptor cross platform.")
	flagSet.StringVar(&kexRespFileName, "kex-resp-file", os.Getenv("GOTIFY_PLUGIN_KEX_RESP_FILE"), "File name for the key exchange for Transport Auth. /proc/self/fd/* can be used to open a file descriptor cross platform.")
	flagSet.BoolVar(&debug, "debug", slices.Contains([]string{"true", "1", "yes", "y"}, strings.ToLower(os.Getenv("GOTIFY_PLUGIN_DEBUG"))), "Enable debug mode.")
	if err := flagSet.Parse(args); err != nil {
		return nil, err
	}

	var kexReqFile *os.File
	var kexRespFile *os.File
	var err error

	if fdNumber, found := strings.CutPrefix(kexReqFileName, "/proc/self/fd/"); found {
		fdNumber, err := strconv.ParseUint(fdNumber, 10, 64)
		kexReqFile = os.NewFile(uintptr(fdNumber), kexReqFileName)
		if err != nil {
			return nil, err
		}
	} else {
		kexReqFile, err = os.OpenFile(kexReqFileName, os.O_WRONLY, 0)
		if err != nil {
			return nil, err
		}
	}
	if fdNumber, found := strings.CutPrefix(kexRespFileName, "/proc/self/fd/"); found {
		fdNumber, err := strconv.ParseUint(fdNumber, 10, 64)
		if err != nil {
			return nil, err
		}
		kexRespFile = os.NewFile(uintptr(fdNumber), kexRespFileName)
	} else {
		kexRespFile, err = os.OpenFile(kexRespFileName, os.O_RDONLY, 0)
		if err != nil {
			return nil, err
		}
	}

	return &PluginCli{
		flagSet:     flagSet,
		KexReqFile:  kexReqFile,
		KexRespFile: kexRespFile,
		Debug:       debug,
	}, nil
}

// Kex performs the key exchange through secure file descriptors provided in the arguments.
func (f *PluginCli) Kex(modulePath string, certPool *x509.CertPool) (certChain []tls.Certificate, err error) {
	// perform key exchange through secure file descriptors
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	csrBytes, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: transport.BuildPluginTLSName("*", modulePath),
		},
	}, priv)

	if err != nil {
		return nil, err
	}
	if _, err := f.KexReqFile.Write(pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE REQUEST",
		Bytes: csrBytes,
	})); err != nil {
		return nil, err
	}

	var certificateChain []tls.Certificate

	if err := transport.IteratePEMFile(f.KexRespFile, func(block *pem.Block) (continueIterate bool, err error) {
		if block.Type == "CERTIFICATE" {
			parsedCert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return false, err
			}
			if certPool != nil {
				certPool.AddCert(parsedCert)
			}
			certificateChain = append(certificateChain, tls.Certificate{
				Certificate: [][]byte{block.Bytes},
				Leaf:        parsedCert,
			})
			return true, nil
		}
		return true, nil
	}); err != nil {
		return nil, err
	}

	if len(certificateChain) == 0 {
		return nil, errors.New("no certificate chain found in kex response file")
	}

	certificateChain[0].PrivateKey = priv

	return certificateChain, nil
}

// Close closes any file descriptors associated with the PluginCli instance.
func (f *PluginCli) Close() error {
	if err := f.KexReqFile.Close(); err != nil {
		return err
	}
	if err := f.KexRespFile.Close(); err != nil {
		return err
	}
	return nil
}
