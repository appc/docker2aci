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
