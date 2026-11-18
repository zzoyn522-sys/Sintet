// launcher-icon generates the Windows resource linked into the launcher EXE.
package main

import (
	"log"
	"os"

	"github.com/tc-hib/winres"
)

func main() {
	source, err := os.Open("installer/resources/Sintet.ico")
	if err != nil {
		log.Fatal(err)
	}
	defer source.Close()
	icon, err := winres.LoadICO(source)
	if err != nil {
		log.Fatal(err)
	}

	var resources winres.ResourceSet
	// Wails loads the application's icon from resource ID 3.
	if err := resources.SetIcon(winres.ID(3), icon); err != nil {
		log.Fatal(err)
	}
	output, err := os.Create("launcher_icon_windows_amd64.syso")
	if err != nil {
		log.Fatal(err)
	}
	writeErr := resources.WriteObject(output, winres.ArchAMD64)
	closeErr := output.Close()
	if writeErr != nil {
		log.Fatal(writeErr)
	}
	if closeErr != nil {
		log.Fatal(closeErr)
	}
}
