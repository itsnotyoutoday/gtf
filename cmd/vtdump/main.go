// Command vtdump renders a captured terminal byte stream through the vt
// emulator (developer tool): vtdump [-w 80] [-h 24] file
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/itsnotyoutoday/gtf/internal/ui/vt"
)

func main() {
	w := flag.Int("w", 80, "columns")
	h := flag.Int("h", 24, "rows")
	flag.Parse()
	b, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	s := vt.New(*w, *h)
	s.Write(b)
	fmt.Print(s.Dump())
}
