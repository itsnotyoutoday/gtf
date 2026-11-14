// Command gtf is the gtf terminal client. All logic lives in internal/client.
package main

import (
	"os"

	"github.com/itsnotyoutoday/gtf/internal/client"
)

func main() { os.Exit(client.Main(os.Args[1:])) }
