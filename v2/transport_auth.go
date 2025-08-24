package plugin

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	purposePluginRPC     = "rpc.plugin"
	purposePluginWebhook = "webhook.plugin"
)

const ServerTLSName = "server.gotify.home.arpa"

func BuildPluginTLSName(purpose string, moduleName string) string {
	moduleNameParts := strings.Split(moduleName, "/")
	for i := range moduleNameParts {
		moduleNameParts[i] = hex.EncodeToString([]byte(moduleNameParts[i]))
	}
	slices.Reverse(moduleNameParts)
	return fmt.Sprintf("%s.%s.plugins.gotify.home.arpa", strings.Join(moduleNameParts, "."), purpose)
}

type EphemeralTLSClient struct {
	caCert    *x509.Certificate
	caPriv    ed25519.PrivateKey
	tlsConfig *tls.Config
}

func (s *EphemeralTLSClient) createCertPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(s.caCert)
	return pool
}

func (s *EphemeralTLSClient) ServerTLSConfig() *tls.Config {
	return &tls.Config{
		ServerName: ServerTLSName,
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  s.createCertPool(),
	}
}

func (s *EphemeralTLSClient) ClientTLSConfig(moduleName string) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{
			{
				Certificate: [][]byte{s.caCert.Raw},
				PrivateKey:  s.caPriv,
			},
		},
		RootCAs:    s.createCertPool(),
		ServerName: BuildPluginTLSName(purposePluginRPC, moduleName),
	}
}

func (s *EphemeralTLSClient) CACert() *x509.Certificate {
	return s.caCert
}

func (s *EphemeralTLSClient) SignCSR(dnsName string, csr *x509.CertificateRequest) ([]byte, error) {
	if err := csr.CheckSignature(); err != nil {
		return nil, err
	}
	certTemplate := &x509.Certificate{
		BasicConstraintsValid: true,
		Subject: pkix.Name{
			CommonName: dnsName,
		},
		DNSNames: []string{
			dnsName,
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(time.Hour * 24 * 365),
		KeyUsage:  x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
		IsCA: false,
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, certTemplate, s.caCert, csr.PublicKey, s.caPriv)
	if err != nil {
		return nil, err
	}
	return certBytes, nil
}

func (s *EphemeralTLSClient) SignPluginCSR(moduleName string, csr *x509.CertificateRequest) ([]byte, error) {
	return s.SignCSR(BuildPluginTLSName(purposePluginRPC, moduleName), csr)
}

func NewEphemeralTLSClient() (*EphemeralTLSClient, error) {
	caPub, caPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	caCertTemplate := &x509.Certificate{
		BasicConstraintsValid: true,
		Subject: pkix.Name{
			CommonName: "gotify Plugin client CA",
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(time.Hour * 24 * 365),
		KeyUsage:  x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageClientAuth,
			x509.ExtKeyUsageServerAuth,
		},
		IsCA: true,
	}
	caCertBytes, err := x509.CreateCertificate(rand.Reader, caCertTemplate, caCertTemplate, caPub, caPriv)
	caCert, err := x509.ParseCertificate(caCertBytes)
	if err != nil {
		return nil, err
	}
	clientPub, clientPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	clientCertTemplate := &x509.Certificate{
		BasicConstraintsValid: true,
		Subject: pkix.Name{
			CommonName: ServerTLSName,
		},
		DNSNames: []string{
			ServerTLSName,
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(time.Hour * 24 * 365),
		KeyUsage:  x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageClientAuth,
		},
		IsCA: false,
	}
	clientCertBytes, err := x509.CreateCertificate(rand.Reader, clientCertTemplate, caCert, clientPub, caPriv)
	if err != nil {
		return nil, err
	}
	certPool := x509.NewCertPool()
	certPool.AddCert(caCert)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{
			{
				Certificate: [][]byte{clientCertBytes},
				PrivateKey:  clientPriv,
			},
			{
				Certificate: [][]byte{caCertBytes},
				PrivateKey:  caPriv,
			},
		},
		RootCAs: certPool,
	}
	return &EphemeralTLSClient{
		caCert:    caCert,
		caPriv:    caPriv,
		tlsConfig: tlsConfig,
	}, nil
}
