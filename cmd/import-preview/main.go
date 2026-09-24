package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/navyaraksha/imogi/internal/application/filebatch/preview"
)

func main() {
	config := preview.DefaultConfig()
	flags := flag.NewFlagSet("import-preview", flag.ExitOnError)
	preview.BindFlags(flags, &config)
	flags.Parse(os.Args[1:])

	result, err := preview.Run(context.Background(), config)
	fmt.Printf("preview directory: %s\n", result.RunDirectory)
	if err != nil {
		fmt.Fprintf(os.Stderr, "import preview failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("master: %s (%d valid, %d invalid, %d warnings)\n", result.Master.Status, result.Master.ValidRows, result.Master.InvalidRows, result.Master.WarningRows)
	fmt.Printf("payroll: %s (%d valid, %d invalid, %d warnings)\n", result.Payroll.Status, result.Payroll.ValidRows, result.Payroll.InvalidRows, result.Payroll.WarningRows)
}
