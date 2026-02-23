package cli

import (
	"encoding/json"
	"fmt"

	"github.com/canopy-dev/canopyd/internal/install"
	"github.com/spf13/cobra"
)

var devicesJSON bool

var devicesCmd = &cobra.Command{
	Use:   "devices",
	Short: "List paired devices",
	RunE: func(cmd *cobra.Command, args []string) error {
		devices, err := install.LoadPairedDevices()
		if err != nil {
			return fmt.Errorf("load devices: %w", err)
		}

		if len(devices) == 0 {
			cmd.Println("No paired devices.")
			return nil
		}

		if devicesJSON {
			data, err := json.MarshalIndent(devices, "", "  ")
			if err != nil {
				return err
			}
			cmd.Println(string(data))
			return nil
		}

		for _, d := range devices {
			name := d.Name
			if name == "" {
				name = "(unnamed)"
			}
			cmd.Printf("%-16s  %-20s  paired %s\n", d.DeviceID, name, d.PairedAt)
		}
		return nil
	},
}

var devicesRemoveCmd = &cobra.Command{
	Use:   "remove <device-id>",
	Short: "Remove a paired device",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deviceID := args[0]
		if err := install.RemovePairedDevice(deviceID); err != nil {
			return fmt.Errorf("remove device: %w", err)
		}
		cmd.Printf("Device %s removed.\n", deviceID)
		return nil
	},
}

var devicesRenameCmd = &cobra.Command{
	Use:   "rename <device-id> <new-name>",
	Short: "Rename a paired device",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		deviceID := args[0]
		newName := args[1]

		devices, err := install.LoadPairedDevices()
		if err != nil {
			return fmt.Errorf("load devices: %w", err)
		}

		found := false
		for i, d := range devices {
			if d.DeviceID == deviceID {
				devices[i].Name = newName
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("device %s not found", deviceID)
		}

		if err := install.SavePairedDevices(devices); err != nil {
			return fmt.Errorf("save devices: %w", err)
		}
		cmd.Printf("Device %s renamed to %q.\n", deviceID, newName)
		return nil
	},
}

func init() {
	devicesCmd.Flags().BoolVar(&devicesJSON, "json", false, "Output as JSON")
	devicesCmd.AddCommand(devicesRemoveCmd)
	devicesCmd.AddCommand(devicesRenameCmd)
}
