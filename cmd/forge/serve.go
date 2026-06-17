package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/wingnut128/forge/pkg/attestation"
	"github.com/wingnut128/forge/pkg/authz"
	"github.com/wingnut128/forge/pkg/orchestration"
)

func runServe() error {
	localTD := os.Getenv("FORGE_LOCAL_TRUST_DOMAIN")
	remoteTD := os.Getenv("FORGE_REMOTE_TRUST_DOMAIN")
	bundleURL := os.Getenv("FORGE_BUNDLE_ENDPOINT_URL")
	listenAddr := os.Getenv("FORGE_LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8080"
	}

	if localTD == "" || remoteTD == "" || bundleURL == "" {
		return fmt.Errorf("FORGE_LOCAL_TRUST_DOMAIN, FORGE_REMOTE_TRUST_DOMAIN, and FORGE_BUNDLE_ENDPOINT_URL are required")
	}

	pair, err := attestation.NewFederationPair(
		attestation.TrustDomain{Name: localTD, Cloud: "gcp"},
		attestation.TrustDomain{Name: remoteTD, Cloud: "aws"},
	)
	if err != nil {
		return fmt.Errorf("federation pair: %w", err)
	}

	refresher, err := attestation.NewBundleRefresher(remoteTD, bundleURL, 0)
	if err != nil {
		return fmt.Errorf("bundle refresher: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := refresher.Start(ctx); err != nil {
		return fmt.Errorf("starting bundle refresher: %w", err)
	}

	var authorizer authz.Authorizer
	policyDir := os.Getenv("FORGE_POLICY_DIR")
	if policyDir != "" {
		a, err := authz.NewCedarAuthorizer(policyDir)
		if err != nil {
			return fmt.Errorf("loading policies from %s: %w", policyDir, err)
		}
		authorizer = a
		fmt.Printf("loaded Cedar policies from %s\n", policyDir)
	}

	srv, err := orchestration.NewServer(pair, refresher, listenAddr, authorizer)
	if err != nil {
		return fmt.Errorf("creating server: %w", err)
	}
	fmt.Printf("forge serve listening on %s\n", listenAddr)
	return srv.Start(ctx)
}
