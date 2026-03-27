package main

import (
	"fmt"
	"os"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newConfigCloudCmd() *cobra.Command {
	var endpoint, bucket, accessKey string
	var secretKeyFromEnv bool

	cmd := &cobra.Command{
		Use:   "cloud",
		Short: "Configure S3 cloud backup credentials",
		Long: `Set up encrypted cloud backups to S3-compatible storage (Scaleway, AWS,
Backblaze, MinIO, etc.).

Interactive (prompts for each field):
  hazmat config cloud

Non-interactive (all flags, for scripting):
  hazmat config cloud \
    --endpoint s3.fr-par.scw.cloud \
    --bucket hazmat-backups \
    --access-key SCWXXXXXXXXX \
    --secret-key-from-env

  The secret key is read from HAZMAT_CLOUD_SECRET_KEY env var when
  --secret-key-from-env is set, or prompted interactively otherwise.

  The Kopia encryption password is prompted interactively unless
  HAZMAT_CLOUD_PASSWORD env var is set.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runConfigCloud(endpoint, bucket, accessKey, secretKeyFromEnv)
		},
	}

	cmd.Flags().StringVar(&endpoint, "endpoint", "",
		"S3-compatible endpoint (e.g. s3.fr-par.scw.cloud)")
	cmd.Flags().StringVar(&bucket, "bucket", "",
		"S3 bucket name")
	cmd.Flags().StringVar(&accessKey, "access-key", "",
		"S3 access key ID")
	cmd.Flags().BoolVar(&secretKeyFromEnv, "secret-key-from-env", false,
		"Read secret key from HAZMAT_CLOUD_SECRET_KEY env var")

	return cmd
}

func runConfigCloud(endpoint, bucket, accessKey string, secretKeyFromEnv bool) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	ensureCloudConfig(&cfg)

	ui := &UI{}
	interactive := ui.IsInteractive() && endpoint == "" && bucket == "" && accessKey == ""

	if interactive {
		fmt.Println()
		cBold.Println("  Cloud Backup — S3-compatible storage")
		fmt.Println()

		// Pre-fill from existing config if available
		defaultEndpoint := "s3.fr-par.scw.cloud"
		if cfg.Backup.Cloud.Endpoint != "" {
			defaultEndpoint = cfg.Backup.Cloud.Endpoint
		}

		fmt.Printf("  S3 Endpoint [%s]: ", defaultEndpoint)
		fmt.Scanln(&endpoint)
		if endpoint == "" {
			endpoint = defaultEndpoint
		}

		fmt.Print("  S3 Bucket: ")
		if cfg.Backup.Cloud.Bucket != "" {
			fmt.Printf("[%s]: ", cfg.Backup.Cloud.Bucket)
		}
		fmt.Scanln(&bucket)
		if bucket == "" {
			bucket = cfg.Backup.Cloud.Bucket
		}
		if bucket == "" {
			return fmt.Errorf("bucket name is required")
		}

		fmt.Print("  Access Key: ")
		fmt.Scanln(&accessKey)
		if accessKey == "" {
			accessKey = cfg.Backup.Cloud.AccessKey
		}
		if accessKey == "" {
			return fmt.Errorf("access key is required")
		}
	} else {
		// Non-interactive: require all fields
		if endpoint == "" {
			return fmt.Errorf("--endpoint is required in non-interactive mode")
		}
		if bucket == "" {
			return fmt.Errorf("--bucket is required in non-interactive mode")
		}
		if accessKey == "" {
			return fmt.Errorf("--access-key is required in non-interactive mode")
		}
	}

	// Secret key
	var secretKey string
	if secretKeyFromEnv {
		secretKey = os.Getenv("HAZMAT_CLOUD_SECRET_KEY")
		if secretKey == "" {
			return fmt.Errorf("HAZMAT_CLOUD_SECRET_KEY environment variable is not set")
		}
	} else if interactive {
		fmt.Print("  Secret Key: ")
		secret, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			return err
		}
		secretKey = string(secret)
		fmt.Println(" (set)")
		if secretKey == "" {
			return fmt.Errorf("secret key is required")
		}
	} else {
		return fmt.Errorf("use --secret-key-from-env or run interactively for secret key input")
	}

	// Kopia encryption password — auto-generated unless user provides one.
	var kopiaPassword string
	if envPass := os.Getenv("HAZMAT_CLOUD_PASSWORD"); envPass != "" {
		kopiaPassword = envPass
	} else if cfg.Backup.Cloud.Password != "" {
		// Reuse existing password (re-running config cloud).
		kopiaPassword = cfg.Backup.Cloud.Password
	} else {
		// Auto-generate a strong diceware passphrase for encryption.
		// Human-readable so it can be written down for disaster recovery.
		passphrase, err := generatePassphrase(7) // ~90 bits
		if err != nil {
			return fmt.Errorf("generate encryption password: %w", err)
		}
		kopiaPassword = passphrase

		if interactive {
			fmt.Println()
			cBold.Println("  Encryption password (auto-generated):")
			fmt.Println()
			fmt.Printf("    %s\n", kopiaPassword)
			fmt.Println()
			cYellow.Println("  Save this password. You need it to restore from cloud backup.")
			cYellow.Println("  It cannot be recovered if lost.")
			fmt.Println()
		}
	}

	// Save config (endpoint, bucket, access key, kopia password — not secret key)
	cfg.Backup.Cloud.Endpoint = endpoint
	cfg.Backup.Cloud.Bucket = bucket
	cfg.Backup.Cloud.AccessKey = accessKey
	cfg.Backup.Cloud.Password = kopiaPassword

	if err := saveConfig(cfg); err != nil {
		return err
	}

	// Save secret key separately with restricted permissions
	if err := saveCloudSecretKey(secretKey); err != nil {
		return err
	}

	fmt.Println()
	cGreen.Println("  Cloud backup configured.")
	fmt.Printf("    Config:      %s\n", configFilePath)
	fmt.Printf("    Credentials: %s\n", cloudCredentialPath)
	cDim.Println("    Run: hazmat backup --cloud")
	fmt.Println()
	return nil
}
