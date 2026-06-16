package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "ayrton",
	Short: "Un CLI moderno y rápido construido con Go + Cobra",
	Long: `ayrton es una herramienta de línea de comandos de ejemplo
que demuestra las mejores prácticas para CLIs open source en Go.

Características:
  - Binario único sin dependencias externas
  - Configuración via flags, env vars y archivo config
  - Releases multi-plataforma automáticos con GoReleaser
  - CI/CD completo con GitHub Actions`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return initConfig()
	},
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "archivo de configuración (default: $HOME/.ayrton.yaml)")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "salida verbosa")
	rootCmd.PersistentFlags().StringP("output", "o", "text", "formato de salida (text|json)")

	_ = viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	_ = viper.BindPFlag("output", rootCmd.PersistentFlags().Lookup("output"))

	rootCmd.AddCommand(versionCmd)
}

func initConfig() error {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}

		viper.AddConfigPath(".")
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".ayrton")
	}

	viper.SetEnvPrefix("AYRTON")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		// Only ignore "config file not found" errors; return all other errors
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("read config: %w", err)
		}
	} else {
		if viper.GetBool("verbose") {
			fmt.Fprintf(os.Stderr, "Usando config: %s\n", viper.ConfigFileUsed())
		}
	}
	return nil
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Muestra la versión del CLI",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Printf("ayrton version %s\n", version)
		cmd.Printf("  commit: %s\n", commit)
		cmd.Printf("  built:  %s\n", date)
	},
}