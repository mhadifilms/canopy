package cli

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/canopy-dev/canopyd/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage daemon configuration",
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a config value",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		val, err := getConfigField(cfg, args[0])
		if err != nil {
			return err
		}
		cmd.Println(val)
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a config value",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		if err := setConfigField(cfg, args[0], args[1]); err != nil {
			return err
		}

		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		cmd.Printf("Set %s = %s\n", args[0], args[1])
		return nil
	},
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all config values",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return err
		}
		cmd.Println(string(data))
		return nil
	},
}

var configResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset config to defaults",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Default()
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		cmd.Println("Config reset to defaults.")
		return nil
	},
}

func init() {
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configResetCmd)
}

// getConfigField gets a config value by its JSON key name.
func getConfigField(cfg *config.Config, key string) (string, error) {
	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		jsonKey := strings.Split(tag, ",")[0]
		if jsonKey == key {
			data, err := json.Marshal(v.Field(i).Interface())
			if err != nil {
				return "", err
			}
			return string(data), nil
		}
	}
	return "", fmt.Errorf("unknown config key: %s", key)
}

// setConfigField sets a config value by its JSON key name.
func setConfigField(cfg *config.Config, key, value string) error {
	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		jsonKey := strings.Split(tag, ",")[0]
		if jsonKey == key {
			field := v.Field(i)
			switch field.Kind() {
			case reflect.String:
				field.SetString(value)
			case reflect.Int:
				n, err := strconv.Atoi(value)
				if err != nil {
					return fmt.Errorf("invalid integer: %s", value)
				}
				field.SetInt(int64(n))
			case reflect.Bool:
				b, err := strconv.ParseBool(value)
				if err != nil {
					return fmt.Errorf("invalid boolean: %s", value)
				}
				field.SetBool(b)
			default:
				return fmt.Errorf("cannot set %s (type %s) via CLI, edit config.json directly", key, field.Kind())
			}
			return nil
		}
	}
	return fmt.Errorf("unknown config key: %s", key)
}
