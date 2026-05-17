package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// LoginRequest defines the request body for the /login endpoint
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse defines the response body for the /login endpoint
type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

func LoginCmd() *cobra.Command {
	var serverURL string
	var username string
	var password string

	var cmd = &cobra.Command{
		Use:   "login",
		Short: "Authenticate and store a Ground Control session",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			scanner := bufio.NewScanner(os.Stdin)

			// Resolve Server URL
			if serverURL == "" {
				fmt.Print("Server: ")
				if scanner.Scan() {
					input := strings.TrimSpace(scanner.Text())
					if input != "" {
						serverURL = input
					} else {
						serverURL = cfg.Server
					}
				}
			}

			if serverURL == "" {
				return fmt.Errorf("server is required")
			}

			// Ensure server has schema prefix
			if !strings.HasPrefix(serverURL, "http://") && !strings.HasPrefix(serverURL, "https://") {
				serverURL = "http://" + serverURL
			}
			serverURL = strings.TrimSuffix(serverURL, "/")

			// Resolve Username
			if username == "" {
				fmt.Print("Username: ")
				if scanner.Scan() {
					input := strings.TrimSpace(scanner.Text())
					if input != "" {
						username = input
					} else {
						username = cfg.Username
					}
				}
			}

			if username == "" {
				return fmt.Errorf("username is required")
			}

			// Resolve Password
			if password == "" {
				fmt.Print("Password: ")
				bytePassword, err := term.ReadPassword(int(syscall.Stdin))
				if err != nil {
					return fmt.Errorf("failed to read password: %w", err)
				}
				fmt.Println()
				password = string(bytePassword)
			}

			reqBody, err := json.Marshal(LoginRequest{
				Username: username,
				Password: password,
			})
			if err != nil {
				return fmt.Errorf("failed to serialize credentials: %w", err)
			}

			resp, err := http.Post(serverURL+"/login", "application/json", bytes.NewBuffer(reqBody))
			if err != nil {
				return fmt.Errorf("failed to connect to server at %s: %w", serverURL, err)
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)

			if resp.StatusCode != http.StatusOK {
				var errMsg struct {
					Error string `json:"error"`
				}
				if json.Unmarshal(body, &errMsg) == nil && errMsg.Error != "" {
					return fmt.Errorf("login failed (%d): %s", resp.StatusCode, errMsg.Error)
				}
				return fmt.Errorf("login failed with status %d: %s", resp.StatusCode, string(body))
			}

			var loginResp LoginResponse
			if err := json.Unmarshal(body, &loginResp); err != nil {
				return fmt.Errorf("failed to parse server response: %w", err)
			}

			cfg.Server = serverURL
			cfg.Token = loginResp.Token
			cfg.ExpiresAt = loginResp.ExpiresAt
			cfg.Username = username

			if err := saveConfig(cfg); err != nil {
				return err
			}

			fmt.Printf("Logged in as %s to %s\n", username, serverURL)
			if loginResp.ExpiresAt != "" {
				fmt.Printf("  Token expires: %s\n", loginResp.ExpiresAt)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&serverURL, "server", "", "Ground Control server URL (e.g. https://gc.example.com)")
	cmd.Flags().StringVar(&username, "username", "", "Username")
	cmd.Flags().StringVar(&password, "password", "", "Password")

	return cmd
}

func LogoutCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "logout",
		Short: "Clear the authenticated session",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			if cfg.Token == "" {
				fmt.Println("Already logged out.")
				return nil
			}

			req, err := http.NewRequest(http.MethodPost, cfg.Server+"/api/logout", nil)
			if err == nil {
				req.Header.Set("Authorization", "Bearer "+cfg.Token)
				// Best effort call to invalidate the session token on the server
				resp, err := http.DefaultClient.Do(req)
				if err == nil {
					resp.Body.Close()
				}
			}

			cfg.Token = ""
			if err := saveConfig(cfg); err != nil {
				return err
			}

			fmt.Println("Successfully logged out and cleared session token.")
			return nil
		},
	}

	return cmd
}

func WhoamiCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "whoami",
		Short: "Show current authenticated user and connection details",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			if cfg.Token == "" || cfg.Server == "" {
				return fmt.Errorf("not logged in. Run 'groundctl login' to authenticate")
			}

			// Validate token by querying /api/whoami
			req, err := http.NewRequest(http.MethodGet, cfg.Server+"/api/whoami", nil)
			if err != nil {
				return err
			}
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("failed to verify session with server: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("session expired or token is invalid. Run 'groundctl login' to re-authenticate")
			}

			var whoamiResp struct {
				Username string `json:"username"`
				Role     string `json:"role"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&whoamiResp); err != nil {
				// Fallback to config values if decoding fails
				whoamiResp.Username = cfg.Username
				whoamiResp.Role = "unknown"
			}

			type WhoAmIResult struct {
				Username  string `json:"username" yaml:"username"`
				Role      string `json:"role" yaml:"role"`
				Server    string `json:"server" yaml:"server"`
				ExpiresAt string `json:"expires_at" yaml:"expires_at"`
				Status    string `json:"status" yaml:"status"`
			}

			res := WhoAmIResult{
				Username:  whoamiResp.Username,
				Role:      whoamiResp.Role,
				Server:    cfg.Server,
				ExpiresAt: cfg.ExpiresAt,
				Status:    "authenticated",
			}

			return PrintResult(os.Stdout, outputFlag, res, func() {
				fmt.Printf("Logged in to %s as %s (role: %s)\n", res.Server, res.Username, res.Role)
				if res.ExpiresAt != "" {
					fmt.Printf("  Token expires: %s\n", res.ExpiresAt)
				}
			})
		},
	}

	return cmd
}
