package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"

	"github.com/appc/docker2aci/lib"
	"github.com/coreos/rocket/cas"
)

const (
	rocketDir = "/var/lib/rkt"
)

var (
	flagImport   = flag.Bool("import", false, "Import ACI images to the rocket store")
	flagNoSquash = flag.Bool("nosquash", false, "Don't Squash layers and output every layer as ACI")
)

func runDocker2ACI(arg string, flagImport bool, flagNoSquash bool) error {
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

	if flagImport {
		ds := cas.NewStore(rocketDir)

		var rocketAppImageID string
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot open store: %v\n", err)
			return err
		}

		// Import from base image
		for i := len(aciLayerPaths) - 1; i >= 0; i-- {
			aciPath := aciLayerPaths[i]
			rocketLayerID, err := importACI(aciPath, ds)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error importing ACI to the store: %v\n", err)
				return err
			}

			// First layer is the app layer
			if i == 0 {
				rocketAppImageID = rocketLayerID
			}
		}
		fmt.Printf("\nImported images into the store in %s\n", rocketDir)
		fmt.Printf("App image ID: %s\n", rocketAppImageID)
	}
	return nil
}

func importACI(aciPath string, dataStore *cas.Store) (string, error) {
	aciFile, err := os.Open(aciPath)
	if err != nil {
		return "", err
	}
	defer aciFile.Close()

	aciReader := bufio.NewReader(aciFile)
	rocketImageID, err := dataStore.WriteACI(aciReader)
	if err != nil {
		return "", err
	}

	return rocketImageID, nil
}

func main() {
	flag.Parse()
	args := flag.Args()

	if len(args) != 1 {
		fmt.Println("Usage: docker2aci [--import] [--nosquash] [REGISTRYURL/]IMAGE_NAME[:TAG]")
		return
	}

	if err := runDocker2ACI(args[0], *flagImport, *flagNoSquash); err != nil {
		os.Exit(1)
	}
}
