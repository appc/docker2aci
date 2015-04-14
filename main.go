// Copyright 2015 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/appc/docker2aci/lib"
	"github.com/appc/docker2aci/lib/util"
)

var (
	flagNoSquash = flag.Bool("nosquash", false, "Don't squash layers and output every layer as ACI")
	flagImage    = flag.String("image", "", "When converting a local file, it selects a particular image to convert. Format: IMAGE_NAME[:TAG]")
	flagDebug    = flag.Bool("debug", false, "Enables debug messages")
)

func runDocker2ACI(arg string, flagNoSquash bool, flagImage string, flagDebug bool) error {
	if flagDebug {
		util.InitDebug()
	}
	squash := !flagNoSquash

	var aciLayerPaths []string
	// try to convert a local file
	u, err := url.Parse(arg)
	if err != nil {
		return fmt.Errorf("error parsing argument: %v", err)
	}
	if u.Scheme == "docker" {
		if flagImage != "" {
			return fmt.Errorf("flag --image works only with files.")
		}
		dockerURL := strings.TrimPrefix(arg, "docker://")

		indexServer := docker2aci.GetIndexName(dockerURL)

		var username, password string
		username, password, err = docker2aci.GetDockercfgAuth(indexServer)
		if err != nil {
			return fmt.Errorf("error reading .dockercfg file: %v", err)
		}

		aciLayerPaths, err = docker2aci.Convert(dockerURL, squash, ".", username, password)
	} else {
		aciLayerPaths, err = docker2aci.ConvertFile(flagImage, arg, squash, ".")
	}
	if err != nil {
		return fmt.Errorf("conversion error: %v", err)
	}

	fmt.Printf("\nGenerated ACI(s):\n")
	for _, aciFile := range aciLayerPaths {
		fmt.Println(aciFile)
	}

	return nil
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "docker2aci [--debug] [--nosquash] IMAGE\n")
	fmt.Fprintf(os.Stderr, "  Where IMAGE is\n")
	fmt.Fprintf(os.Stderr, "    [--image=IMAGE_NAME[:TAG]] FILEPATH\n")
	fmt.Fprintf(os.Stderr, "  or\n")
	fmt.Fprintf(os.Stderr, "    docker://[REGISTRYURL/]IMAGE_NAME[:TAG]\n")
	fmt.Fprintf(os.Stderr, "Flags:\n")
	flag.PrintDefaults()
}

func main() {
	flag.Usage = usage
	flag.Parse()
	args := flag.Args()

	if len(args) < 1 {
		usage()
		return
	}

	if err := runDocker2ACI(args[0], *flagNoSquash, *flagImage, *flagDebug); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
