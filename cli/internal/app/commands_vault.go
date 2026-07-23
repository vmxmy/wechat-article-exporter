package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
	"golang.org/x/term"
)

const maximumVaultPassphraseBytes = 64 << 10

func (a *App) vaultCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "vault",
		Short: "Initialize and verify the encrypted secret-store fallback",
		Long: "Manage the explicitly initialized encrypted vault used when an OS credential store is unavailable. " +
			"Passphrases are read from a protected file or a terminal and are never accepted as command-line values.",
	}
	command.AddCommand(a.vaultStatusCommand(), a.vaultInitCommand(), a.vaultVerifyCommand())
	return command
}

func (a *App) vaultStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use: "status", Short: "Show encrypted vault initialization and active-backend status", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			path := a.vaultPath()
			store := secrets.NewVaultStore(path, secrets.DefaultVaultParameters)
			err := store.ValidateEnvelope()
			initialized := err == nil
			invalid := err != nil && !errors.Is(err, secrets.ErrVaultUninitialized)
			info, statErr := os.Stat(path)
			mode := ""
			if statErr == nil {
				mode = info.Mode().Perm().String()
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return fmt.Errorf("inspect encrypted vault: %w", statErr)
			}
			configured := strings.ToLower(strings.TrimSpace(os.Getenv("WECHAT_ARTICLE_SECRET_BACKEND")))
			if configured == "" {
				configured = "os-keyring"
			}
			if configured == "encrypted-vault" {
				configured = "vault"
			}
			result := map[string]any{
				"initialized": initialized,
				"invalid":     invalid,
				"path":        path,
				"permissions": mode,
				"active":      configured == "vault",
				"backend":     configured,
			}
			if invalid {
				result["error"] = err.Error()
			}
			return a.output(result)
		},
	}
}

func (a *App) vaultInitCommand() *cobra.Command {
	var passphraseFile string
	command := &cobra.Command{
		Use: "init", Short: "Initialize the encrypted vault with a new passphrase", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			passphrase, err := vaultInitializationPassphrase(passphraseFile, a.stdin, a.stderr)
			if err != nil {
				return usage(err.Error())
			}
			defer zeroSecret(passphrase)
			store := secrets.NewVaultStore(a.vaultPath(), secrets.DefaultVaultParameters)
			if err := store.Initialize(passphrase); err != nil {
				return err
			}
			store.Lock()
			return a.output(map[string]any{
				"initialized": true,
				"path":        a.vaultPath(),
				"backend":     store.Backend(),
				"next":        "set WECHAT_ARTICLE_SECRET_BACKEND=vault and provide a passphrase file or interactive terminal",
			})
		},
	}
	command.Flags().StringVar(&passphraseFile, "passphrase-file", "", "protected file containing the vault passphrase")
	return command
}

func (a *App) vaultVerifyCommand() *cobra.Command {
	var passphraseFile string
	command := &cobra.Command{
		Use: "verify", Short: "Verify that the encrypted vault can be unlocked", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if _, err := os.Stat(a.vaultPath()); errors.Is(err, os.ErrNotExist) {
				return secrets.ErrVaultUninitialized
			} else if err != nil {
				return fmt.Errorf("inspect encrypted vault: %w", err)
			}
			passphrase, err := vaultCommandPassphrase(passphraseFile, a.stdin, a.stderr, "Encrypted vault passphrase: ")
			if err != nil {
				return usage(err.Error())
			}
			defer zeroSecret(passphrase)
			store := secrets.NewVaultStore(a.vaultPath(), secrets.DefaultVaultParameters)
			if err := store.Unlock(passphrase); err != nil {
				return err
			}
			store.Lock()
			return a.output(map[string]any{"verified": true, "path": a.vaultPath(), "backend": store.Backend()})
		},
	}
	command.Flags().StringVar(&passphraseFile, "passphrase-file", "", "protected file containing the vault passphrase")
	return command
}

func (a *App) vaultPath() string {
	if a.runtimes != nil {
		return a.runtimes.paths.VaultFile()
	}
	return "secrets.vault.json"
}

func vaultInitializationPassphrase(path string, stdin io.Reader, stderr io.Writer) ([]byte, error) {
	if strings.TrimSpace(path) != "" {
		return readPassphraseFile(path)
	}
	first, err := readTerminalPassphrase(stdin, stderr, "New encrypted vault passphrase: ")
	if err != nil {
		return nil, err
	}
	second, err := readTerminalPassphrase(stdin, stderr, "Confirm encrypted vault passphrase: ")
	if err != nil {
		zeroSecret(first)
		return nil, err
	}
	defer zeroSecret(second)
	if !constantTimeBytesEqual(first, second) {
		zeroSecret(first)
		return nil, errors.New("encrypted vault passphrases do not match")
	}
	return first, nil
}

func vaultCommandPassphrase(path string, stdin io.Reader, stderr io.Writer, prompt string) ([]byte, error) {
	if strings.TrimSpace(path) != "" {
		return readPassphraseFile(path)
	}
	return readTerminalPassphrase(stdin, stderr, prompt)
}

func readPassphraseFile(path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("passphrase file path is required")
	}
	file, info, err := openProtectedPassphraseFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if !info.Mode().IsRegular() {
		return nil, errors.New("passphrase file must be a regular file")
	}
	if info.Size() > maximumVaultPassphraseBytes+2 {
		return nil, fmt.Errorf("passphrase file exceeds %d bytes", maximumVaultPassphraseBytes)
	}
	if err := validatePassphraseFilePermissions(path, file, info); err != nil {
		return nil, err
	}
	value, err := io.ReadAll(io.LimitReader(file, maximumVaultPassphraseBytes+3))
	if err != nil {
		zeroSecret(value)
		return nil, fmt.Errorf("read passphrase file: %w", err)
	}
	if len(value) > maximumVaultPassphraseBytes+2 {
		zeroSecret(value)
		return nil, fmt.Errorf("passphrase file exceeds %d bytes", maximumVaultPassphraseBytes)
	}
	value = trimOneLineEnding(value)
	if len(value) == 0 {
		return nil, errors.New("encrypted vault passphrase must not be empty")
	}
	if len(value) > maximumVaultPassphraseBytes {
		zeroSecret(value)
		return nil, fmt.Errorf("encrypted vault passphrase exceeds %d bytes", maximumVaultPassphraseBytes)
	}
	return value, nil
}

func readTerminalPassphrase(stdin io.Reader, stderr io.Writer, prompt string) ([]byte, error) {
	file, ok := stdin.(interface{ Fd() uintptr })
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return nil, errors.New("encrypted vault passphrase requires --passphrase-file or an interactive terminal")
	}
	if stderr != nil {
		_, _ = fmt.Fprint(stderr, prompt)
	}
	value, err := term.ReadPassword(int(file.Fd()))
	if stderr != nil {
		_, _ = fmt.Fprintln(stderr)
	}
	if err != nil {
		zeroSecret(value)
		return nil, fmt.Errorf("read encrypted vault passphrase: %w", err)
	}
	if len(value) == 0 {
		return nil, errors.New("encrypted vault passphrase must not be empty")
	}
	if len(value) > maximumVaultPassphraseBytes {
		zeroSecret(value)
		return nil, fmt.Errorf("encrypted vault passphrase exceeds %d bytes", maximumVaultPassphraseBytes)
	}
	return value, nil
}

func trimOneLineEnding(value []byte) []byte {
	if len(value) > 0 && value[len(value)-1] == '\n' {
		value = value[:len(value)-1]
		if len(value) > 0 && value[len(value)-1] == '\r' {
			value = value[:len(value)-1]
		}
	}
	return value
}

func constantTimeBytesEqual(left, right []byte) bool {
	difference := len(left) ^ len(right)
	maximum := len(left)
	if len(right) > maximum {
		maximum = len(right)
	}
	for index := 0; index < maximum; index++ {
		var leftByte, rightByte byte
		if index < len(left) {
			leftByte = left[index]
		}
		if index < len(right) {
			rightByte = right[index]
		}
		difference |= int(leftByte ^ rightByte)
	}
	return difference == 0
}
