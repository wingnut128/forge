package main

import (
	"context"
	"fmt"
	"os"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optdestroy"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optup"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: forge <preview|up|destroy|serve>")
		os.Exit(1)
	}

	if os.Args[1] == "serve" {
		if err := runServe(); err != nil {
			fmt.Fprintf(os.Stderr, "serve failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ctx := context.Background()

	stackName := os.Getenv("FORGE_STACK")
	if stackName == "" {
		stackName = "dev"
	}

	s, err := auto.UpsertStackInlineSource(ctx, stackName, "forge", deployFunc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create/select stack: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("operating on stack %q\n", stackName)

	w := os.Stdout

	switch os.Args[1] {
	case "preview":
		_, err = s.Preview(ctx, optpreview.ProgressStreams(w))
	case "up":
		_, err = s.Up(ctx, optup.ProgressStreams(w))
	case "destroy":
		_, err = s.Destroy(ctx, optdestroy.ProgressStreams(w))
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "command failed: %v\n", err)
		os.Exit(1)
	}
}
