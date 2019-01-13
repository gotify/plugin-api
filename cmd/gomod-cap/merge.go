/*gomod-cap

This command line util is used to help plugins keep their go.mod file requirements lower than the gotify-server itself in order to make sure that plugins are built with the same version of dependencies as the main gotify program does.

Since go.mod follow a minimal version selection(https://github.com/golang/proposal/blob/master/design/24301-versioned-go.md). Common import paths described in the plugin go.mod must have a lower or equal version than the main program itself.

Sample usage:

$ go run github.com/gotify/plugin-api/cmd/gomod-cap -from /path/to/gotify/server/go.mod -to /path/to/your/plugin/go.mod
*/
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/exec"
	"path"
)

// Copied from go help mod edit

type Module struct {
	Path    string
	Version string
}

type GoMod struct {
	Module  Module
	Require []Require
	Exclude []Module
	Replace []Replace
}

type Require struct {
	Path     string
	Version  string
	Indirect bool
}

type Replace struct {
	Old Module
	New Module
}

func goCmd(args ...string) ([]byte, error) {
	cmd := exec.Command("go", args...)
	out := bytes.NewBuffer([]byte{})
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	return out.Bytes(), err
}

func getModuleRequireFromGoModFile(path string) []Require {
	var res GoMod
	goModJSON, err := goCmd("mod", "edit", "-json", path)
	if err != nil {
		panic(err)
	}
	if err := json.Unmarshal(goModJSON, &res); err != nil {
		panic(err)
	}
	return res.Require
}

func main() {
	fromFlag := flag.String("from", "./go.mod", "go.mod file or dir to cap go.mod version from")
	toFlag := flag.String("to", "./go.mod", "go.mod file or dir to cap go.mod version to")
	checkFlag := flag.Bool("check", false, "check for incompatibility only")
	flag.Parse()

	from := *fromFlag
	if info, err := os.Stat(from); err == nil && info.IsDir() {
		from = path.Join(from, "./", "go.mod")
	}
	cur := *toFlag
	if info, err := os.Stat(cur); err == nil && info.IsDir() {
		cur = path.Join(cur, "./", "go.mod")
	}
	curMods := getModuleRequireFromGoModFile(cur)
	fromMods := getModuleRequireFromGoModFile(from)

	modConstraint := make(map[string]string)
	for _, req := range fromMods {
		modConstraint[req.Path] = req.Version
	}

	for _, req := range curMods {
		if constraintVersion, ok := modConstraint[req.Path]; ok {
			log.Printf("Found common import path %s", req.Path)
			if req.Version != constraintVersion {
				if *checkFlag {
					log.Printf("Found incompatible go.mod constraint: path %s has version %s, higher than %s", req.Path, req.Version, constraintVersion)
					os.Exit(1)
				}
				log.Printf("Changing require version to %s", constraintVersion)
				goCmd("mod", "edit", "-require="+req.Path+"@"+constraintVersion, cur)
			}
		}
	}
}
