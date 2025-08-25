package plugin

import (
	"flag"
	"os"
	"strconv"
	"strings"
)

type PluginCliFlags struct {
	flagSet     *flag.FlagSet
	KexReqFile  *os.File
	KexRespFile *os.File
	Debug       bool
}

func ParsePluginCLIFlags(args []string) (*PluginCliFlags, error) {
	flagSet := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	var kexReqFileName string
	var kexRespFileName string
	var debug bool
	flagSet.StringVar(&kexReqFileName, "kex-req-file", "", "File name for the key exchange for Transport Auth. /proc/self/fd/* can be used to open a file descriptor cross platform.")
	flagSet.StringVar(&kexRespFileName, "kex-resp-file", "", "File name for the key exchange for Transport Auth. /proc/self/fd/* can be used to open a file descriptor cross platform.")
	flagSet.BoolVar(&debug, "debug", false, "Enable debug mode.")
	flagSet.Parse(args)

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
		kexRespFile = os.NewFile(uintptr(fdNumber), kexRespFileName)
		if err != nil {
			return nil, err
		}
	} else {
		kexRespFile, err = os.OpenFile(kexRespFileName, os.O_RDONLY, 0)
		if err != nil {
			return nil, err
		}
	}

	if err != nil {
		return nil, err
	}
	return &PluginCliFlags{
		flagSet:     flagSet,
		KexReqFile:  kexReqFile,
		KexRespFile: kexRespFile,
		Debug:       debug,
	}, nil
}

func (f *PluginCliFlags) Close() error {
	if err := f.KexReqFile.Close(); err != nil {
		return err
	}
	if err := f.KexRespFile.Close(); err != nil {
		return err
	}
	return nil
}
