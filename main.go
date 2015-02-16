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
	"os"

	"github.com/appc/docker2aci/lib"
)

const (
	rocketDir = "/var/lib/rkt"
)

var flagNoSquash = flag.Bool("nosquash", false, "Don't Squash layers and output every layer as ACI")

func runDocker2ACI(arg string, flagNoSquash bool) error {
	squash := !flagNoSquash

	aciLayerPaths, err := docker2aci.Convert(arg, squash, ".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Conversion error: %v\n", err)
		return err
	}

	fmt.Printf("\nGenerated ACI(s):\n")
	for _, aciFile := range aciLayerPaths {
		fmt.Println(aciFile)
	}

	return nil
}

func main() {
	flag.Parse()
	args := flag.Args()

	if len(args) != 1 {
		fmt.Println("Usage: docker2aci [--nosquash] [REGISTRYURL/]IMAGE_NAME[:TAG]")
		return
	}

	if err := runDocker2ACI(args[0], *flagNoSquash); err != nil {
		os.Exit(1)
	}
}
