package main

import (
	"fmt"
	"os"
	"sintet/internal/identity"
)

func main() {
	if len(os.Args) != 2 || os.Getenv("SINTET_CURSEFORGE_API_KEY") == "" {
		fmt.Fprintln(os.Stderr, "Usage: set SINTET_CURSEFORGE_API_KEY and pass the credential file path")
		os.Exit(1)
	}
	if err := identity.SaveServiceSecret(os.Args[1], os.Getenv("SINTET_CURSEFORGE_API_KEY")); err != nil {
		fmt.Fprintln(os.Stderr, "Could not protect the service key")
		os.Exit(1)
	}
	fmt.Println("CurseForge key saved with Windows account-bound encryption")
}
