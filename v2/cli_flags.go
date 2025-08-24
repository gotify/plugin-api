package plugin

import (
	"flag"
	"os"
)

type PluginCliFlags struct {
	flagSet  *flag.FlagSet
	CAData   []byte
	CertData []byte
	KeyData  []byte
}

func ParsePluginCLIFlags(args []string) (*PluginCliFlags, error) {
	flagSet := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	var caFile string
	var certFile string
	var keyFile string
	flagSet.StringVar(&certFile, "cert-file", "", "Path to the certificate file for Transport Auth.")
	flagSet.StringVar(&keyFile, "key-file", "", "Path to the key file for Transport Auth.")
	flagSet.StringVar(&caFile, "ca-file", "", "Path to the CA file for Transport Auth.")
	flagSet.Parse(args)
	certData, err := os.ReadFile(certFile)
	if err != nil {
		return nil, err
	}
	keyData, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}
	caData, err := os.ReadFile(caFile)
	if err != nil {
		return nil, err
	}
	return &PluginCliFlags{
		flagSet:  flagSet,
		CAData:   caData,
		CertData: certData,
		KeyData:  keyData,
	}, nil
}
