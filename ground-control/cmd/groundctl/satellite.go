package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// NullTime custom type to handle database sql.NullTime serialization in JSON
type NullTime struct {
	Time  time.Time `json:"Time"`
	Valid bool      `json:"Valid"`
}

// NullString custom type to handle database sql.NullString serialization in JSON
type NullString struct {
	String string `json:"String"`
	Valid  bool   `json:"Valid"`
}

// NullInt64 custom type to handle database sql.NullInt64 serialization in JSON
type NullInt64 struct {
	Int64 int64 `json:"Int64"`
	Valid bool  `json:"Valid"`
}

// NullInt32 custom type to handle database sql.NullInt32 serialization in JSON
type NullInt32 struct {
	Int32 int32 `json:"Int32"`
	Valid bool  `json:"Valid"`
}

// Satellite represents a satellite returned by Ground Control
type Satellite struct {
	ID                int32      `json:"ID"`
	Name              string     `json:"Name"`
	CreatedAt         time.Time  `json:"CreatedAt"`
	UpdatedAt         time.Time  `json:"UpdatedAt"`
	LastSeen          NullTime   `json:"LastSeen"`
	HeartbeatInterval NullString `json:"HeartbeatInterval"`
}

// RegisterSatelliteParams defines the payload for creating a satellite
type RegisterSatelliteParams struct {
	Name       string   `json:"name"`
	Groups     []string `json:"groups,omitempty"`
	ConfigName string   `json:"config_name"`
}

// RegisterSatelliteResponse defines the creation response
type RegisterSatelliteResponse struct {
	Token string `json:"token"`
}

// SatelliteStatus represents operational health status reported by edge satellite
type SatelliteStatus struct {
	ID                 int32      `json:"ID"`
	SatelliteID        int32      `json:"SatelliteID"`
	Activity           string     `json:"Activity"`
	LatestStateDigest  NullString `json:"LatestStateDigest"`
	LatestConfigDigest NullString `json:"LatestConfigDigest"`
	CpuPercent         NullString `json:"CpuPercent"`
	MemoryUsedBytes    NullInt64  `json:"MemoryUsedBytes"`
	StorageUsedBytes   NullInt64  `json:"StorageUsedBytes"`
	LastSyncDurationMs NullInt64  `json:"LastSyncDurationMs"`
	ImageCount         NullInt32  `json:"ImageCount"`
	ReportedAt         time.Time  `json:"ReportedAt"`
	CreatedAt          time.Time  `json:"CreatedAt"`
}

func SatelliteCmd() *cobra.Command {
	var satCmd = &cobra.Command{
		Use:   "satellite",
		Short: "Manage edge satellites registered in Ground Control",
	}

	satCmd.AddCommand(SatelliteListCmd())
	satCmd.AddCommand(SatelliteGetCmd())
	satCmd.AddCommand(SatelliteRegisterCmd())
	satCmd.AddCommand(SatelliteDeleteCmd())
	satCmd.AddCommand(SatelliteStatusCmd())

	return satCmd
}

func getAuthenticatedClient(cfg *Config) (*http.Client, string, error) {
	if cfg.Token == "" || cfg.Server == "" {
		return nil, "", fmt.Errorf("authentication required. Run 'groundctl login' to log in")
	}
	return http.DefaultClient, cfg.Server, nil
}

func SatelliteListCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "list",
		Short: "List all registered edge satellites",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			client, server, err := getAuthenticatedClient(cfg)
			if err != nil {
				return err
			}

			// Get Satellites
			req, err := http.NewRequest(http.MethodGet, server+"/api/satellites", nil)
			if err != nil {
				return err
			}
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("failed to fetch satellites: %w", err)
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)

			if resp.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("session expired or token is invalid. Please run 'groundctl login' again")
			}

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("failed to list satellites (%d): %s", resp.StatusCode, string(body))
			}

			var satellites []Satellite
			if err := json.Unmarshal(body, &satellites); err != nil {
				return fmt.Errorf("failed to parse satellite list: %w", err)
			}

			// Try to associate GroupNames by querying groups
			groupMap, _ := buildSatelliteGroupMap(server, cfg.Token)

			type SatelliteDisplay struct {
				Name     string `json:"name" yaml:"name"`
				Status   string `json:"status" yaml:"status"`
				LastSeen string `json:"last_seen" yaml:"last_seen"`
				Group    string `json:"group" yaml:"group"`
			}

			displayList := make([]SatelliteDisplay, 0, len(satellites))
			for _, sat := range satellites {
				status := "offline"
				lastSeenStr := "never"
				groupName := "default"

				if g, exists := groupMap[sat.Name]; exists {
					groupName = g
				}

				if sat.LastSeen.Valid {
					lastSeenStr = sat.LastSeen.Time.Format("2006-01-02 15:04")
					interval := 1 * time.Minute
					if sat.HeartbeatInterval.Valid {
						if parsed, err := time.ParseDuration(strings.TrimPrefix(sat.HeartbeatInterval.String, "@every ")); err == nil {
							interval = parsed
						} else if parsed, err := time.ParseDuration(sat.HeartbeatInterval.String); err == nil {
							interval = parsed
						}
					}
					if time.Since(sat.LastSeen.Time) > 3*interval {
						status = "stale"
					} else {
						status = "active"
					}
				}

				displayList = append(displayList, SatelliteDisplay{
					Name:     sat.Name,
					Status:   status,
					LastSeen: lastSeenStr,
					Group:    groupName,
				})
			}

			return PrintResult(os.Stdout, outputFlag, displayList, func() {
				if len(displayList) == 0 {
					fmt.Println("No satellites registered yet.")
					return
				}

				table := tablewriter.NewWriter(os.Stdout)
				table.SetHeader([]string{"NAME", "STATUS", "LAST SEEN", "GROUP"})
				table.SetBorder(false)
				table.SetColumnSeparator("")
				table.SetHeaderLine(false)
				table.SetHeaderColor(
					tablewriter.Colors{tablewriter.Bold, tablewriter.FgCyanColor},
					tablewriter.Colors{tablewriter.Bold, tablewriter.FgCyanColor},
					tablewriter.Colors{tablewriter.Bold, tablewriter.FgCyanColor},
					tablewriter.Colors{tablewriter.Bold, tablewriter.FgCyanColor},
				)

				for _, d := range displayList {
					statusColor := "\033[31m" // Red
					if d.Status == "active" {
						statusColor = "\033[32m" // Green
					} else if d.Status == "stale" {
						statusColor = "\033[33m" // Yellow
					}
					resetColor := "\033[0m"

					table.Append([]string{
						d.Name,
						statusColor + d.Status + resetColor,
						d.LastSeen,
						d.Group,
					})
				}
				table.Render()
			})
		},
	}
	return cmd
}

func SatelliteGetCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "get [name]",
		Short: "Show details of a specific satellite",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			client, server, err := getAuthenticatedClient(cfg)
			if err != nil {
				return err
			}

			req, err := http.NewRequest(http.MethodGet, server+"/api/satellites/"+name, nil)
			if err != nil {
				return err
			}
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("failed to fetch satellite: %w", err)
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)

			if resp.StatusCode == http.StatusNotFound {
				return fmt.Errorf("satellite '%s' not found", name)
			}
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("failed to get satellite (%d): %s", resp.StatusCode, string(body))
			}

			var sat Satellite
			if err := json.Unmarshal(body, &sat); err != nil {
				return fmt.Errorf("failed to parse satellite details: %w", err)
			}

			return PrintResult(os.Stdout, outputFlag, sat, func() {
				fmt.Printf("Satellite Details:\n")
				fmt.Printf("  ID:                 %d\n", sat.ID)
				fmt.Printf("  Name:               %s\n", sat.Name)
				fmt.Printf("  Created At:         %s\n", sat.CreatedAt.Format(time.RFC3339))
				fmt.Printf("  Updated At:         %s\n", sat.UpdatedAt.Format(time.RFC3339))
				if sat.LastSeen.Valid {
					fmt.Printf("  Last Seen:          %s\n", sat.LastSeen.Time.Format(time.RFC3339))
				} else {
					fmt.Printf("  Last Seen:          never\n")
				}
				if sat.HeartbeatInterval.Valid {
					fmt.Printf("  Heartbeat Interval: %s\n", sat.HeartbeatInterval.String)
				} else {
					fmt.Printf("  Heartbeat Interval: N/A\n")
				}
			})
		},
	}
	return cmd
}

func SatelliteRegisterCmd() *cobra.Command {
	var configName string
	var groups []string

	var cmd = &cobra.Command{
		Use:   "register [name]",
		Short: "Register a new satellite in Ground Control",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			client, server, err := getAuthenticatedClient(cfg)
			if err != nil {
				return err
			}

			params := RegisterSatelliteParams{
				Name:       name,
				ConfigName: configName,
				Groups:     groups,
			}

			reqBody, err := json.Marshal(params)
			if err != nil {
				return err
			}

			req, err := http.NewRequest(http.MethodPost, server+"/api/satellites", bytes.NewBuffer(reqBody))
			if err != nil {
				return err
			}
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("failed to register satellite: %w", err)
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)

			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
				return fmt.Errorf("failed to register satellite (%d): %s", resp.StatusCode, string(body))
			}

			var createResp RegisterSatelliteResponse
			if err := json.Unmarshal(body, &createResp); err != nil {
				return fmt.Errorf("failed to parse registration response: %w", err)
			}

			return PrintResult(os.Stdout, outputFlag, createResp, func() {
				fmt.Printf("Successfully registered satellite '%s'!\n", name)
				fmt.Printf("Access Token: %s\n", createResp.Token)
			})
		},
	}

	cmd.Flags().StringVar(&configName, "config-name", "default-config", "Default configuration name for this satellite")
	cmd.Flags().StringSliceVar(&groups, "groups", []string{}, "Comma-separated list of groups to attach this satellite to")

	return cmd
}

func SatelliteDeleteCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "delete [name]",
		Short: "Delete a satellite registration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			client, server, err := getAuthenticatedClient(cfg)
			if err != nil {
				return err
			}

			req, err := http.NewRequest(http.MethodDelete, server+"/api/satellites/"+name, nil)
			if err != nil {
				return err
			}
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("failed to delete satellite: %w", err)
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)

			if resp.StatusCode == http.StatusNotFound {
				return fmt.Errorf("satellite '%s' not found", name)
			}
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("failed to delete satellite (%d): %s", resp.StatusCode, string(body))
			}

			fmt.Printf("Successfully deleted satellite '%s'.\n", name)
			return nil
		},
	}
	return cmd
}

func SatelliteStatusCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "status [name]",
		Short: "Show detailed health and operational status of a satellite",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			client, server, err := getAuthenticatedClient(cfg)
			if err != nil {
				return err
			}

			req, err := http.NewRequest(http.MethodGet, server+"/api/satellites/"+name+"/status", nil)
			if err != nil {
				return err
			}
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("failed to fetch status: %w", err)
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)

			if resp.StatusCode == http.StatusNotFound {
				return fmt.Errorf("status for satellite '%s' not found", name)
			}
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("failed to get status (%d): %s", resp.StatusCode, string(body))
			}

			var status SatelliteStatus
			if err := json.Unmarshal(body, &status); err != nil {
				return fmt.Errorf("failed to parse satellite status: %w", err)
			}

			return PrintResult(os.Stdout, outputFlag, status, func() {
				fmt.Printf("Satellite Health & Status Details for '%s':\n", name)
				fmt.Printf("  Operational Activity:  %s\n", status.Activity)
				if status.LatestStateDigest.Valid {
					fmt.Printf("  Latest State Digest:   %s\n", status.LatestStateDigest.String)
				}
				if status.LatestConfigDigest.Valid {
					fmt.Printf("  Latest Config Digest:  %s\n", status.LatestConfigDigest.String)
				}
				if status.CpuPercent.Valid {
					fmt.Printf("  CPU Usage:             %s%%\n", status.CpuPercent.String)
				}
				if status.MemoryUsedBytes.Valid {
					fmt.Printf("  Memory Usage:          %.2f MB\n", float64(status.MemoryUsedBytes.Int64)/(1024*1024))
				}
				if status.StorageUsedBytes.Valid {
					fmt.Printf("  Storage Usage:         %.2f MB\n", float64(status.StorageUsedBytes.Int64)/(1024*1024))
				}
				if status.ImageCount.Valid {
					fmt.Printf("  Cached Image Count:    %d\n", status.ImageCount.Int32)
				}
				fmt.Printf("  Last Reported At:      %s\n", status.ReportedAt.Format(time.RFC3339))
			})
		},
	}
	return cmd
}

// buildSatelliteGroupMap constructs a map of satelliteName to groupName by listing groups and querying their satellites.
func buildSatelliteGroupMap(server, token string) (map[string]string, error) {
	req, err := http.NewRequest(http.MethodGet, server+"/api/groups", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return make(map[string]string), nil
	}

	var groups []struct {
		GroupName string `json:"group_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&groups); err != nil {
		return make(map[string]string), nil
	}

	groupMap := make(map[string]string)
	for _, g := range groups {
		sReq, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/groups/%s/satellites", server, g.GroupName), nil)
		if err != nil {
			continue
		}
		sReq.Header.Set("Authorization", "Bearer "+token)
		sResp, err := http.DefaultClient.Do(sReq)
		if err != nil {
			continue
		}
		var sList []Satellite
		if json.NewDecoder(sResp.Body).Decode(&sList) == nil {
			for _, sat := range sList {
				groupMap[sat.Name] = g.GroupName
			}
		}
		sResp.Body.Close()
	}

	return groupMap, nil
}
