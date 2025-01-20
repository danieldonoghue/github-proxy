package main

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/hashicorp/vault/api"
)

var (
	privateKey *rsa.PrivateKey
)

// validateBindAddr validates the bind address to ensure it's a valid TCP address.
func validateBindAddr(addr string) error {
	if addr == "" {
		return fmt.Errorf("bind address is empty")
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid bind address format: %w", err)
	}

	if port == "" {
		return fmt.Errorf("port is missing in bind address")
	}

	ip := net.ParseIP(host)
	if ip == nil && host != "" {
		return fmt.Errorf("invalid IP address: %s", host)
	}

	return nil
}

// parseFlags() parse startup flags and returns an error if any required flags are missing
func parseFlags(ctx context.Context) error {
	flag.Parse()

	if *verCheck {
		fmt.Printf("Version: %s\n", Version)
		return versionCheckErr
	}

	if err := validateBindAddr(*bindAddr); err != nil {
		return fmt.Errorf("invalid bind address: %s", *bindAddr)
	}

	if *clientID == "" {
		return fmt.Errorf("client ID is required")
	}

	if *installationID == "" {
		return fmt.Errorf("installation ID is required")
	}

	key, err := RetrieveGithubPrivateKey(ctx)
	if err != nil {
		return err
	}

	privateKey = key
	return nil
}

// RetrieveGithubPrivateKey() returns the private key for the GitHub App.
func RetrieveGithubPrivateKey(ctx context.Context) (*rsa.PrivateKey, error) {
	switch {
	case *useVault:
		path, key, _ := strings.Cut(*privateKeyPath, ":")
		return retrievePrivateKeyFromVault(ctx, path, key)

	case *privateKeyPath != "":
		return loadPrivateKeyFromFile(*privateKeyPath)

	case os.Getenv("GH_PRIVATE_KEY") != "":
		return getPrivateKeyFromEnv("GH_PRIVATE_KEY")
	}

	return nil, fmt.Errorf("no private key source found")
}

// ParsePrivateKey parses and returns a PEM encoded RSA private key.
func parsePrivateKey(keyBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(keyBytes)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		return nil, fmt.Errorf("failed to decode PEM block containing private key")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return key, nil
}

// LoadPrivateKeyFromFile reads the private key from a .pem file.
func loadPrivateKeyFromFile(path string) (*rsa.PrivateKey, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}

	return parsePrivateKey(keyBytes)
}

// RetrievePrivateKeyFromVault retrieves an RSA private key from hashicorp Vault.
func retrievePrivateKeyFromVault(ctx context.Context, vaultPath, key string) (*rsa.PrivateKey, error) {
	if vaultPath == "" {
		return nil, fmt.Errorf("vault path is empty")
	}

	if key == "" {
		// default to private_key for the vault field name
		key = "private_key"
	}

	client, err := api.NewClient(api.DefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create Vault client: %w", err)
	}

	if vaultPath[0] == '/' {
		// remove leading / from path
		vaultPath = vaultPath[1:]
	}

	mount, path, _ := strings.Cut(vaultPath, "/")
	secret, err := client.KVv2(mount).Get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed to read secret from Vault: %w", err)
	}

	if secret == nil || secret.Data[key] == nil {
		return nil, fmt.Errorf("no private key found at %s, using key name %s", vaultPath, key)
	}

	keyBytes, ok := secret.Data[key].(string)
	if !ok {
		return nil, fmt.Errorf("private key is not a string")
	}

	return parsePrivateKey([]byte(keyBytes))
}

// GetPrivateKeyFromEnv retrieves an RSA private key from an environment variable.
func getPrivateKeyFromEnv(varName string) (*rsa.PrivateKey, error) {
	if varName == "" {
		// default to GH_PRIVATE_KEY
		varName = "GH_PRIVATE_KEY"
	}

	keyBytes := os.Getenv(varName)
	if keyBytes == "" {
		return nil, fmt.Errorf("%s environment variable is empty", varName)
	}

	return parsePrivateKey([]byte(keyBytes))
}
