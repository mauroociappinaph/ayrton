package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var greetCmd = &cobra.Command{
	Use:   "greet [name]",
	Short: "Saluda a alguien",
	Long:  `Saluda a la persona indicada con un mensaje amistoso.`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := "mundo"
		if len(args) > 0 {
			name = args[0]
		}

		lang := viper.GetString("lang")
		var greeting string
		switch lang {
		case "en":
			greeting = fmt.Sprintf("Hello, %s! 👋", name)
		default:
			greeting = fmt.Sprintf("¡Hola, %s! 👋", name)
		}

		if viper.GetString("output") == "json" {
			_ = json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{"greeting": greeting})
		} else {
			cmd.Println(greeting)
		}
	},
}

func init() {
	rootCmd.AddCommand(greetCmd)

	greetCmd.Flags().StringP("lang", "l", "es", "idioma del saludo (es|en)")
	_ = viper.BindPFlag("lang", greetCmd.Flags().Lookup("lang"))
}