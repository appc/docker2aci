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
	"github.com/appc/docker2aci/lib/common"
	"github.com/appc/docker2aci/lib/util"

	"github.com/appc/spec/aci"
	"github.com/appc/spec/schema"
)

var (
	flagNoSquash    = flag.Bool("nosquash", false, "Don't squash layers and output every layer as ACI")
	flagImage       = flag.String("image", "", "When converting a local file, it selects a particular image to convert. Format: IMAGE_NAME[:TAG]")
	flagDebug       = flag.Bool("debug", false, "Enables debug messages")
	flagInsecure    = flag.Bool("insecure", false, "Uses unencrypted connections when fetching images")
	flagCompression = flag.String("compression", "gzip", "Type of compression to use; allowed values: gzip, none")
)

func runDocker2ACI(arg string, flagNoSquash bool, flagImage string, flagDebug bool, flagInsecure bool, flagCompression string) error {
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

	var compression common.Compression

	switch flagCompression {
	case "none":
		compression = common.NoCompression
	case "gzip":
		compression = common.GzipCompression
	default:
		return fmt.Errorf("unknown compression method: %s", flagCompression)
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

		aciLayerPaths, err = docker2aci.Convert(dockerURL, squash, ".", os.TempDir(), compression, username, password, flagInsecure)
	} else {
		aciLayerPaths, err = docker2aci.ConvertFile(flagImage, arg, squash, ".", os.TempDir(), compression)
	}
	if err != nil {
		return fmt.Errorf("conversion error: %v", err)
	}

	// we get last layer's manifest, this will include all the elements in the
	// previous layers. If we're squashing, the last element of aciLayerPaths
	// will be the squashed image.
	manifest, err := getManifest(aciLayerPaths[len(aciLayerPaths)-1])
	if err != nil {
		return err
	}

	if err := printConvertedVolumes(*manifest); err != nil {
		return err
	}
	if err := printConvertedPorts(*manifest); err != nil {
		return err
	}

	fmt.Printf("\nGenerated ACI(s):\n")
	for _, aciFile := range aciLayerPaths {
		fmt.Println(aciFile)
	}

	return nil
}

func printConvertedVolumes(manifest schema.ImageManifest) error {
	if manifest.App != nil && manifest.App.MountPoints != nil {
		mps := manifest.App.MountPoints
		if len(mps) > 0 {
			fmt.Printf("\nConverted volumes:\n")
			for _, mp := range mps {
				fmt.Printf("\tname: %q, path: %q, readOnly: %v\n", mp.Name, mp.Path, mp.ReadOnly)
			}
		}
	}

	return nil
}

func printConvertedPorts(manifest schema.ImageManifest) error {
	if manifest.App != nil && manifest.App.Ports != nil {
		ports := manifest.App.Ports
		if len(ports) > 0 {
			fmt.Printf("\nConverted ports:\n")
			for _, port := range ports {
				fmt.Printf("\tname: %q, protocol: %q, port: %v, count: %v, socketActivated: %v\n",
					port.Name, port.Protocol, port.Port, port.Count, port.SocketActivated)
			}
		}
	}

	return nil
}

func getManifest(aciPath string) (*schema.ImageManifest, error) {
	f, err := os.Open(aciPath)
	if err != nil {
		return nil, fmt.Errorf("error opening converted image: %v", err)
	}
	defer f.Close()

	manifest, err := aci.ManifestFromImage(f)
	if err != nil {
		return nil, fmt.Errorf("error reading manifest from converted image: %v", err)
	}

	return manifest, nil
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "docker2aci [--debug] [--nosquash] [--compression=(gzip|none)] IMAGE\n")
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

	if err := runDocker2ACI(args[0], *flagNoSquash, *flagImage, *flagDebug, *flagInsecure, *flagCompression); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
