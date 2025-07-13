// This file is part of homebridge-captains-log
// Package main implements a CLI tool to monitor Homebridge accessory status changes.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

type AccessoryStatus struct {
	UniqueID    string                 `json:"uniqueId"`
	ServiceName string                 `json:"serviceName"`
	Type        string                 `json:"type"`
	Values      map[string]interface{} `json:"values"`
	LastUpdated time.Time              `json:"lastUpdated"`
}

type StatusMonitor struct {
	baseURL    string
	interval   time.Duration
	lastStatus map[string]AccessoryStatus
	client     *http.Client
	token      string
}

// discoveryProgress was used for monitoring discovery but is no longer needed

var (
	host     string
	port     int
	interval time.Duration
	count    int
	token    string
	useHAP   bool
	debug    bool
	
	// AWTRIX3 configuration
	at3Host string
	at3Port int
)

// Timeout Configuration - All timeouts used throughout the application
//
// RELIABILITY STRATEGY:
// - Individual service lookups use short timeouts to avoid blocking parallel operations
// - Overall phases have longer timeouts as safety nets
// - Quick retries with exponential backoff for transient failures
// - HTTP operations use generous timeouts since they're usually reliable
var TimeoutConfig = struct {
	// HTTP API timeouts
	HTTPClient           time.Duration // General HTTP client timeout
	HTTPToken            time.Duration // Auth token request timeout
	HTTPChildBridges     time.Duration // Child bridge API request timeout
	HTTPAccessoryCheck   time.Duration // Individual accessory check timeout
	HTTPReachabilityTest time.Duration // Quick reachability test timeout

	// mDNS Discovery timeouts
	MDNSTotal            time.Duration // Total mDNS discovery timeout (browse + lookup phases)
	MDNSBrowseMax        time.Duration // Maximum time for browse phase
	MDNSLookupMax        time.Duration // Maximum time for lookup phase
	MDNSLookupPerService time.Duration // Maximum time per individual service lookup
	MDNSReadTimeout      time.Duration // Network read timeout for mDNS packets
	MDNSSilenceTimeout   time.Duration // How long to wait for new responses before completing
	MDNSEarlyExitSilence time.Duration // Silence period before early exit when expected count reached

	// Retry and delay timeouts
	RetryDelay       time.Duration // Delay between discovery retry attempts
	LookupRetryDelay time.Duration // Delay between individual service lookup retries

	// Default intervals
	DefaultPollingInterval time.Duration // Default polling interval for accessory checks
}{
	// HTTP timeouts
	HTTPClient:           10 * time.Second,
	HTTPToken:            5 * time.Second,
	HTTPChildBridges:     10 * time.Second,
	HTTPAccessoryCheck:   5 * time.Second,
	HTTPReachabilityTest: 1 * time.Second,

	// mDNS timeouts
	MDNSTotal:            10 * time.Second,
	MDNSBrowseMax:        3 * time.Second,
	MDNSLookupMax:        7 * time.Second,
	MDNSLookupPerService: 2 * time.Second, // Much shorter per-service timeout
	MDNSReadTimeout:      100 * time.Millisecond,
	MDNSSilenceTimeout:   300 * time.Millisecond,
	MDNSEarlyExitSilence: 100 * time.Millisecond,

	// Retry timeouts
	RetryDelay:       2 * time.Second,
	LookupRetryDelay: 500 * time.Millisecond,

	// Default intervals
	DefaultPollingInterval: 3 * time.Second,
}

// Application constants
const (
	DefaultHomebridgePort = 8581
	HTTPStatusOK          = 200
	HTTPStatusMultiple    = 300
	MaxBufSize            = 1500
	MaxStatementsPerFunc  = 10
)

var rootCmd = &cobra.Command{
	Use:   "captains-log",
	Short: "Monitor Homebridge accessory status changes",
	Long:  "A CLI tool to monitor Homebridge bridges and report accessory status changes.",
	Run:   runMonitor,
}

func init() {
	_ = godotenv.Load() // Ignore error - .env file is optional

	// Environment variables are now optional - auto-discovery is used as fallback
	defaultHost := getEnvOrDefault("CLOG_HB_HOST", "")
	defaultPort := getEnvIntOrDefault("CLOG_HB_PORT", DefaultHomebridgePort)
	defaultToken := getEnvOrDefault("CLOG_HB_TOKEN", "")
	
	// AWTRIX3 configuration
	defaultAT3Host := getEnvOrDefault("CLOG_AT3_HOST", "")
	defaultAT3Port := getEnvIntOrDefault("CLOG_AT3_PORT", 80)

	rootCmd.Flags().StringVarP(&host, "host", "H", defaultHost, "Homebridge UI host (auto-discovered if not provided)")
	rootCmd.Flags().IntVarP(&port, "port", "p", defaultPort, "Homebridge UI port (default: 8581)")
	rootCmd.Flags().DurationVarP(&interval, "interval", "i", TimeoutConfig.DefaultPollingInterval,
		"Polling interval (duration format: 3s, 30s, 1m, etc.)")
	rootCmd.Flags().IntVarP(&count, "count", "c", -1,
		"Number of checks to perform (0 = discovery-only, default = infinite)")
	rootCmd.Flags().StringVarP(&token, "token", "t", defaultToken, "Homebridge UI auth token")
	rootCmd.Flags().BoolVarP(&debug, "debug", "d", false, "Enable debug output")
	
	// AWTRIX3 flags
	rootCmd.Flags().StringVar(&at3Host, "at3-host", defaultAT3Host, "AWTRIX3 host IP/hostname (auto-discovered if not provided)")
	rootCmd.Flags().IntVar(&at3Port, "at3-port", defaultAT3Port, "AWTRIX3 port (default: 80)")
	rootCmd.Flags().BoolP("child-bridges", "b", false, "Also monitor child bridges (deprecated)")
	rootCmd.Flags().BoolVarP(&useHAP, "hap", "a", true, "Use HAP (HomeKit) protocol to monitor child bridges (default)")
	rootCmd.Flags().BoolP("main", "m", false, "Monitor main bridge only instead of child bridges")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		if _, writeErr := fmt.Fprintf(os.Stderr, "Error: %v\n", err); writeErr != nil {
			log.Printf("Failed to write error to stderr: %v", writeErr)
		}
		os.Exit(1)
	}
}

func runMonitor(cmd *cobra.Command, _ []string) {
	// Auto-discover Homebridge if host/port not manually configured
	finalHost, finalPort := resolveHomebridgeLocation(cmd)
	if finalHost == "" {
		fmt.Printf("ERROR: Failed to discover Homebridge instance and no manual configuration provided\n")
		os.Exit(1)
	}

	// Check if main bridge mode was explicitly requested
	mainMode, _ := cmd.Flags().GetBool("main")
	if mainMode {
		useHAP = false
	}

	if !useHAP {
		// Main bridge mode
		baseURL := fmt.Sprintf("http://%s:%d", finalHost, finalPort)

		monitor := &StatusMonitor{
			baseURL:    baseURL,
			interval:   interval,
			lastStatus: make(map[string]AccessoryStatus),
			client:     &http.Client{Timeout: TimeoutConfig.HTTPClient},
			token:      token,
		}

		fmt.Printf("Starting Homebridge main bridge monitor on %s (interval: %v)\n", baseURL, interval)

		monitor.run(count)
	} else {
		// Child bridges mode (default) - update global variables for getChildBridges()
		host = finalHost
		port = finalPort
		runHAPMonitor(count)
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := fmt.Sscanf(value, "%d", &defaultValue); err == nil && intValue == 1 {
			return defaultValue
		}
	}
	return defaultValue
}

// resolveHomebridgeLocation determines the final host/port to use
// Manual configuration (CLI flags or env vars) takes precedence over auto-discovery
func resolveHomebridgeLocation(cmd *cobra.Command) (string, int) {
	// Check if host was manually provided (CLI flag or env var)
	hostProvided := cmd.Flags().Changed("host") || os.Getenv("CLOG_HB_HOST") != ""
	portProvided := cmd.Flags().Changed("port") || os.Getenv("CLOG_HB_PORT") != ""

	if hostProvided || portProvided {
		// Manual configuration provided - use it
		finalHost := host
		finalPort := port
		
		// If only one was provided, auto-discover the other if possible
		if hostProvided && !portProvided {
			// Host provided, port not provided - use default port
			finalPort = DefaultHomebridgePort
		} else if !hostProvided && portProvided {
			// Port provided, host not provided - try auto-discovery for host
			if discoveredHost, _ := discoverServices(); discoveredHost != "" {
				finalHost = discoveredHost
			} else {
				finalHost = "localhost" // fallback
			}
		}
		
		fmt.Printf("Using manual configuration: %s:%d\n", finalHost, finalPort)
		return finalHost, finalPort
	}

	// No manual Homebridge configuration - attempt auto-discovery
	fmt.Printf("No manual configuration provided, attempting auto-discovery...\n")
	discoveredHost, awtrixDevices := discoverServices()
	if discoveredHost != "" {
		fmt.Printf("Auto-discovered Homebridge at: %s:%d\n", discoveredHost, DefaultHomebridgePort)
		
		// Check for manual AWTRIX3 configuration or use discovered devices
		awtrix3Host, awtrix3Port := resolveAWTRIX3Location(cmd)
		if awtrix3Host != "" {
			fmt.Printf("Using manual AWTRIX3 at: %s:%d\n", awtrix3Host, awtrix3Port)
		} else if len(awtrixDevices) > 0 {
			for _, device := range awtrixDevices {
				fmt.Printf("Auto-discovered AWTRIX3 at: %s:%d\n", device.Host, device.Port)
			}
		} else {
			fmt.Printf("No AWTRIX3 devices found\n")
		}
		return discoveredHost, DefaultHomebridgePort
	}

	fmt.Printf("Auto-discovery failed\n")
	return "", 0
}

// resolveAWTRIX3Location determines if manual AWTRIX3 configuration is provided
// Returns empty host if no manual configuration is found
func resolveAWTRIX3Location(cmd *cobra.Command) (string, int) {
	// Check if AWTRIX3 host was manually provided (CLI flag or env var)
	at3HostProvided := cmd.Flags().Changed("at3-host") || os.Getenv("CLOG_AT3_HOST") != ""
	at3PortProvided := cmd.Flags().Changed("at3-port") || os.Getenv("CLOG_AT3_PORT") != ""
	
	if at3HostProvided || at3PortProvided {
		finalHost := at3Host
		finalPort := at3Port
		
		// If only host provided, use default port
		if at3HostProvided && !at3PortProvided {
			finalPort = 80
		} else if !at3HostProvided && at3PortProvided {
			// Port provided but no host - cannot use manual config
			return "", 0
		}
		
		return finalHost, finalPort
	}
	
	return "", 0
}

// discoverServices discovers the main Homebridge instance and AWTRIX devices via mDNS concurrently
func discoverServices() (string, []MDNSService) {
	debugf("Starting concurrent auto-discovery of services\n")
	
	client := NewMDNSClient(TimeoutConfig.MDNSTotal)
	ctx := context.Background()
	
	// Channels for goroutine communication
	type homebridgeResult struct {
		services []MDNSService
		err      error
	}
	type awtrixResult struct {
		services []MDNSService
		err      error
	}
	
	homebridgeChan := make(chan homebridgeResult, 1)
	awtrixChan := make(chan awtrixResult, 1)
	
	// Launch Homebridge discovery goroutine (CRITICAL)
	go func() {
		debugf("Starting Homebridge discovery goroutine\n")
		services, err := client.DiscoverHomebridgeServices(ctx)
		homebridgeChan <- homebridgeResult{services: services, err: err}
	}()
	
	// Launch AWTRIX discovery goroutine (OPTIONAL)
	go func() {
		debugf("Starting AWTRIX discovery goroutine\n")
		services, err := client.DiscoverAWTRIXServices(ctx)
		awtrixChan <- awtrixResult{services: services, err: err}
	}()
	
	// Wait for both discoveries to complete
	var mdnsServices []MDNSService
	var awtrixServices []MDNSService
	
	// Get Homebridge results (CRITICAL - die if this fails)
	homebridgeRes := <-homebridgeChan
	if homebridgeRes.err != nil {
		debugf("CRITICAL: Homebridge discovery failed: %v\n", homebridgeRes.err)
		log.Fatalf("Failed to discover Homebridge services: %v", homebridgeRes.err)
	}
	mdnsServices = homebridgeRes.services
	debugf("Found %d Homebridge services via mDNS\n", len(mdnsServices))
	
	// Get AWTRIX results (OPTIONAL - continue if this fails)
	awtrixRes := <-awtrixChan
	if awtrixRes.err != nil {
		debugf("AWTRIX discovery failed (non-critical): %v\n", awtrixRes.err)
		awtrixServices = nil
	} else {
		awtrixServices = awtrixRes.services
		debugf("Found %d AWTRIX devices via mDNS\n", len(awtrixServices))
		for _, service := range awtrixServices {
			debugf("AWTRIX device: %s at %s:%d\n", service.Name, service.Host, service.Port)
		}
	}
	
	// Test each Homebridge service to find the main instance
	for _, service := range mdnsServices {
		debugf("Testing service: %s at %s:%d\n", service.Name, service.Host, service.Port)
		
		if isMainHomebridgeInstance(service.Host, service.Port) {
			debugf("Confirmed main Homebridge instance: %s\n", service.Host)
			// Resolve hostname to IP if needed
			resolvedHost := resolveHostnameToIP(service.Host)
			return resolvedHost, awtrixServices
		}
	}
	
	// CRITICAL: If no main Homebridge instance found, die
	log.Fatalf("No main Homebridge instance found among %d discovered services", len(mdnsServices))
	return "", awtrixServices // This line won't be reached due to log.Fatalf
}

// resolveHostnameToIP resolves a hostname to IP address using standard DNS lookup
func resolveHostnameToIP(hostname string) string {
	// If it's already an IP address, return as-is
	if net.ParseIP(hostname) != nil {
		return hostname
	}
	
	// Try to resolve hostname to IP
	ips, err := net.LookupIP(hostname)
	if err != nil {
		debugf("Failed to resolve hostname %s: %v\n", hostname, err)
		return hostname // Return original hostname if resolution fails
	}
	
	// Return the first IPv4 address found
	for _, ip := range ips {
		if ipv4 := ip.To4(); ipv4 != nil {
			debugf("Resolved hostname %s to IP: %s\n", hostname, ipv4.String())
			return ipv4.String()
		}
	}
	
	debugf("No IPv4 address found for hostname %s\n", hostname)
	return hostname // Return original hostname if no IPv4 found
}

// isMainHomebridgeInstance checks if the given HAP service is the main Homebridge instance
func isMainHomebridgeInstance(host string, port int) bool {
	client := &http.Client{Timeout: TimeoutConfig.HTTPReachabilityTest}
	
	url := fmt.Sprintf("http://%s:%d/accessories", host, port)
	resp, err := client.Get(url)
	if err != nil {
		debugf("Failed to connect to %s:%d - %v\n", host, port, err)
		return false
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != HTTPStatusOK {
		debugf("Non-200 response from %s:%d - status %d\n", host, port, resp.StatusCode)
		return false
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		debugf("Failed to read response from %s:%d - %v\n", host, port, err)
		return false
	}
	
	var hapResp HAPResponse
	if err := json.Unmarshal(body, &hapResp); err != nil {
		debugf("Failed to parse HAP response from %s:%d - %v\n", host, port, err)
		return false
	}
	
	// Check if this is the main Homebridge instance
	if len(hapResp.Accessories) > 0 {
		firstAccessory := hapResp.Accessories[0]
		for _, service := range firstAccessory.Services {
			for _, char := range service.Characteristics {
				// Look for manufacturer characteristic
				if char.Description == "Manufacturer" {
					if manufacturer, ok := char.Value.(string); ok && manufacturer == "homebridge.io" {
						debugf("Confirmed main Homebridge via manufacturer: %s\n", manufacturer)
						return true
					}
				}
				// Look for model characteristic as additional confirmation
				if char.Description == "Model" {
					if model, ok := char.Value.(string); ok && model == "homebridge" {
						debugf("Confirmed main Homebridge via model: %s\n", model)
						return true
					}
				}
			}
		}
	}
	
	debugf("Service at %s:%d is not main Homebridge\n", host, port)
	return false
}

func (m *StatusMonitor) run(maxChecks int) {
	// Handle discovery-only mode (count = 0) for main bridge
	if maxChecks == 0 {
		fmt.Println("Discovery-only mode: performing main bridge discovery and exiting...")
		accessories, err := m.fetchAccessories()
		if err != nil {
			fmt.Printf("Error during discovery: %v\n", err)
			return
		}
		fmt.Printf("Main bridge discovery complete. Found %d accessories from main bridge.\n", len(accessories))
		for _, accessory := range accessories {
			fmt.Printf("  - %s (%s)\n", accessory.ServiceName, accessory.Type)
		}
		return
	}

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	// Initial check
	m.checkStatus()

	if maxChecks == 1 {
		return
	}

	checkCount := 1
	for range ticker.C {
		m.checkStatus()
		checkCount++

		if maxChecks > 0 && checkCount >= maxChecks {
			fmt.Printf("Completed %d checks, exiting.\n", checkCount)
			return
		}
	}
}

func (m *StatusMonitor) checkStatus() {
	fmt.Printf("\n[%s] Checking %d accessories...\n", time.Now().Format("15:04:05"), len(m.lastStatus))

	accessories, err := m.fetchAccessories()
	if err != nil {
		log.Printf("Error fetching accessories: %v", err)
		return
	}

	changesDetected := 0
	newAccessories := 0

	for _, accessory := range accessories {
		if lastAccessory, exists := m.lastStatus[accessory.UniqueID]; exists {
			if m.hasChanged(lastAccessory, accessory) {
				m.reportChange(lastAccessory, accessory)
				changesDetected++
			}
		} else {
			fmt.Printf("New accessory detected: %s\n", accessory.ServiceName)
			newAccessories++
		}

		m.lastStatus[accessory.UniqueID] = accessory
	}

	if changesDetected == 0 && newAccessories == 0 {
		fmt.Printf("No changes detected.\n")
	} else {
		fmt.Printf("Summary: %d changes, %d new accessories\n", changesDetected, newAccessories)
	}
}

func (m *StatusMonitor) fetchAccessories() ([]AccessoryStatus, error) {
	req, err := http.NewRequest("GET", m.baseURL+"/api/accessories", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if m.token != "" {
		req.Header.Set("Authorization", "Bearer "+m.token)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch accessories: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		// Try to get token via noauth and retry
		if token, err := m.getNoAuthToken(); err == nil {
			m.token = token
			fmt.Println("Obtained auth token via /api/auth/noauth, retrying...")
			return m.fetchAccessories()
		}
		return nil, fmt.Errorf("API returned 401 and failed to get noauth token: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var accessories []AccessoryStatus
	if err := json.Unmarshal(body, &accessories); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Update timestamps
	now := time.Now()
	for i := range accessories {
		accessories[i].LastUpdated = now
	}

	return accessories, nil
}

func (m *StatusMonitor) getNoAuthToken() (string, error) {
	req, err := http.NewRequest("POST", m.baseURL+"/api/auth/noauth", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create noauth request: %w", err)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call noauth endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("noauth endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read noauth response: %w", err)
	}

	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		Token       string `json:"token"`
	}

	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return "", fmt.Errorf("failed to parse noauth response: %w", err)
	}

	// Try both possible field names
	if tokenResponse.AccessToken != "" {
		return tokenResponse.AccessToken, nil
	}
	if tokenResponse.Token != "" {
		return tokenResponse.Token, nil
	}

	return "", fmt.Errorf("no token found in noauth response")
}

func (*StatusMonitor) hasChanged(old, current AccessoryStatus) bool {
	return !reflect.DeepEqual(old.Values, current.Values)
}

func (m *StatusMonitor) reportChange(old, current AccessoryStatus) {
	fmt.Printf("\n[%s] %s:\n",
		current.LastUpdated.Format("15:04:05"),
		current.ServiceName)

	for key, newValue := range current.Values {
		if oldValue, exists := old.Values[key]; exists {
			if !reflect.DeepEqual(oldValue, newValue) {
				message := m.formatChangeMessage(key, oldValue, newValue, current.ServiceName)
				fmt.Printf("  %s\n", message)
			}
		} else {
			fmt.Printf("  %s: (new) %v\n", key, newValue)
		}
	}

	for key, oldValue := range old.Values {
		if _, exists := current.Values[key]; !exists {
			fmt.Printf("  %s: %v → (removed)\n", key, oldValue)
		}
	}
}

func (*StatusMonitor) formatChangeMessage(key string, oldValue, newValue interface{}, _ string) string {
	switch key {
	case "On":
		return formatOnOffMessage(newValue)
	case "ContactSensorState":
		return formatContactSensorMessage(newValue)
	case "MotionDetected":
		return formatMotionMessage(newValue)
	case "Brightness":
		return fmt.Sprintf("brightness: %v%% → %v%%", oldValue, newValue)
	case "CurrentTemperature":
		return fmt.Sprintf("temperature: %.1f°C → %.1f°C", oldValue, newValue)
	case "CurrentRelativeHumidity":
		return fmt.Sprintf("humidity: %.1f%% → %.1f%%", oldValue, newValue)
	case "BatteryLevel":
		return fmt.Sprintf("battery: %v%% → %v%%", oldValue, newValue)
	case "StatusLowBattery":
		return formatBatteryStatusMessage(newValue)
	default:
		return fmt.Sprintf("%s: %v → %v", key, oldValue, newValue)
	}
}

func formatOnOffMessage(newValue interface{}) string {
	if val, ok := newValue.(bool); ok && val {
		return "turned ON"
	}
	if val, ok := newValue.(float64); ok && val == 1.0 {
		return "turned ON"
	}
	return "turned OFF"
}

func formatContactSensorMessage(newValue interface{}) string {
	if newValue.(float64) == 0 {
		return "door/window CLOSED"
	}
	return "door/window OPENED"
}

func formatMotionMessage(newValue interface{}) string {
	if val, ok := newValue.(bool); ok && val {
		return "motion DETECTED"
	}
	if val, ok := newValue.(float64); ok && val == 1.0 {
		return "motion DETECTED"
	}
	return "motion CLEARED"
}

func formatBatteryStatusMessage(newValue interface{}) string {
	if newValue.(float64) == 1 {
		return "⚠️  LOW BATTERY"
	}
	return "battery OK"
}

func runHAPMonitor(maxChecks int) {
	checksInfo := "infinite"
	if maxChecks > 0 {
		checksInfo = fmt.Sprintf("%d", maxChecks)
	} else if maxChecks == 0 {
		checksInfo = "discovery-only"
	}
	fmt.Printf("Starting Homebridge Captain's Log (checks: %s, interval: %v)\n", checksInfo, interval)

	// Track status for each discovered bridge
	bridgeStatusMap := make(map[string]map[string]interface{})

	// Cache for optimized discovery
	var cachedChildBridges []ChildBridge
	var cachedHAPServices []HAPAccessory
	isInitialDiscovery := true

	// Handle discovery-only mode (count = 0)
	if maxChecks == 0 {
		performDiscovery(&cachedChildBridges, &cachedHAPServices)
		return
	}

	// Convert -1 (default) to 0 for infinite checks in the monitoring logic
	infiniteMode := (maxChecks == -1)
	if infiniteMode {
		maxChecks = 0 // 0 means infinite in the existing logic
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial check
	checkAllBridgesOptimized(bridgeStatusMap, &cachedChildBridges, &cachedHAPServices, isInitialDiscovery)

	if maxChecks == 1 {
		return
	}

	checkCount := 1

	// Set up interrupt handling
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	for {
		select {
		case <-ticker.C:
			checkAllBridgesOptimized(bridgeStatusMap, &cachedChildBridges, &cachedHAPServices, false)
			checkCount++

			if maxChecks > 0 && checkCount >= maxChecks {
				fmt.Printf("Completed %d checks, stopping...\n", checkCount)
				return
			}

		case <-c:
			fmt.Println("\nStopping monitor...")
			return
		}
	}
}

func checkAllBridgesOptimized(
	bridgeStatusMap map[string]map[string]interface{}, cachedChildBridges *[]ChildBridge,
	cachedHAPServices *[]HAPAccessory, forceFullDiscovery bool,
) {
	checkAllBridgesOptimizedWithRetry(
		bridgeStatusMap, cachedChildBridges, cachedHAPServices, forceFullDiscovery, 1)
}

func checkAllBridgesOptimizedWithRetry(
	bridgeStatusMap map[string]map[string]interface{}, cachedChildBridges *[]ChildBridge,
	cachedHAPServices *[]HAPAccessory, forceFullDiscovery bool, attempt int,
) {
	discoveryConfig := discoveryConfig{forceFullDiscovery: forceFullDiscovery}
	currentChildBridges, hapServices, usedCache := discoverBridges(cachedChildBridges, cachedHAPServices, discoveryConfig)

	if len(currentChildBridges) == 0 {
		fmt.Println("No child bridges found in API.")
		return
	}

	// Check if we found fewer services than expected and retry if needed
	if len(hapServices) < len(currentChildBridges) {
		if attempt < 3 {
			debugf("mDNS found only %d/%d expected bridges on attempt %d, retrying...\n",
				len(hapServices), len(currentChildBridges), attempt)
			time.Sleep(TimeoutConfig.RetryDelay)
			checkAllBridgesOptimizedWithRetry(
				bridgeStatusMap, cachedChildBridges, cachedHAPServices, true, attempt+1)
			return
		}
		fmt.Printf("ERROR: Failed to find all bridges after 3 attempts\n")
		displaydiscoveryResults(currentChildBridges, hapServices)
		fmt.Printf("Discovery incomplete - exiting. Check that all child bridges are running and " +
			"advertising _hap._tcp services.\n")
		return
	}

	// Show discovery results based on whether we used cache or did full discovery
	if usedCache {
		fmt.Println()
		fmt.Println("Using cached bridge configuration (no changes detected)")
	} else {
		displaydiscoveryResults(currentChildBridges, hapServices)
	}

	debugf("Total discovered child bridge services: %d\n", len(hapServices))

	// Parallelize accessory checks using goroutines
	var wg sync.WaitGroup
	var mu sync.Mutex
	bridgesWithAccessories := 0
	client := &http.Client{Timeout: TimeoutConfig.HTTPAccessoryCheck}

	// Process each service in parallel
	for _, service := range hapServices {
		wg.Add(1)
		go func(svc HAPAccessory) {
			defer wg.Done()

			// Check if this service actually has accessories
			if !hasHAPAccessories(svc.Host, svc.Port) {
				debugf("Service %s at %s:%d has no accessories\n", svc.Name, svc.Host, svc.Port)
				return
			}

			// Thread-safe increment
			mu.Lock()
			bridgesWithAccessories++
			// Initialize status map for this bridge if needed
			if bridgeStatusMap[svc.ID] == nil {
				bridgeStatusMap[svc.ID] = make(map[string]interface{})
			}
			bridgeStatus := bridgeStatusMap[svc.ID]
			mu.Unlock()

			// Check accessories on this bridge with synchronized output
			checkHAPAccessoryWithSync(client, svc, bridgeStatus, &mu)
		}(service)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	if bridgesWithAccessories == 0 {
		fmt.Println("No HAP services with accessories found.")
		fmt.Println("Note: Services may only have read-only sensors or no controllable accessories.")
	} else {
		debugf("Found %d HAP services with accessories\n", bridgesWithAccessories)
	}
}

// performDiscovery performs bridge discovery and outputs results consistently
func performDiscovery(cachedChildBridges *[]ChildBridge, cachedHAPServices *[]HAPAccessory) {
	discoveryConfig := discoveryConfig{forceFullDiscovery: true}
	currentChildBridges, finalServices, _ := discoverBridges(cachedChildBridges, cachedHAPServices, discoveryConfig)
	displaydiscoveryResults(currentChildBridges, finalServices)
}

// discoverBridges performs the actual discovery logic, returns bridges and services
func discoverBridges(
	cachedChildBridges *[]ChildBridge, cachedHAPServices *[]HAPAccessory, discoveryConfig discoveryConfig,
) ([]ChildBridge, []HAPAccessory, bool) {
	currentChildBridges := getChildBridges()
	if len(currentChildBridges) == 0 {
		debugf("No child bridges found in API\n")
		return currentChildBridges, []HAPAccessory{}, false
	}
	debugf("Found %d child bridges from API\n", len(currentChildBridges))

	discoveryResult := evaluateDiscoveryStrategy(cachedChildBridges, cachedHAPServices, currentChildBridges, discoveryConfig)
	finalServices := executeLookupStrategy(discoveryResult, currentChildBridges)

	updateDiscoveryCache(cachedChildBridges, cachedHAPServices, currentChildBridges, finalServices)
	return currentChildBridges, finalServices, discoveryResult.usedCache
}

type discoveryConfig struct {
	forceFullDiscovery bool
}

type discoveryResult struct {
	needsFullDiscovery bool
	usedCache          bool
	cachedServices     []HAPAccessory
}

func evaluateDiscoveryStrategy(
	cachedChildBridges *[]ChildBridge,
	cachedHAPServices *[]HAPAccessory,
	currentChildBridges []ChildBridge,
	discoveryConfig discoveryConfig,
) discoveryResult {
	if discoveryConfig.forceFullDiscovery || len(*cachedChildBridges) == 0 {
		return discoveryResult{needsFullDiscovery: true, usedCache: false}
	}

	if !childBridgeListsEqual(*cachedChildBridges, currentChildBridges) {
		debugf("Child bridge list changed, forcing full discovery\n")
		return discoveryResult{needsFullDiscovery: true, usedCache: false}
	}

	return evaluateCachedServices(cachedHAPServices)
}

func evaluateCachedServices(cachedHAPServices *[]HAPAccessory) discoveryResult {
	debugf("Child bridge list unchanged, checking cached services\n")
	unreachableServices := testCachedServicesReachability(*cachedHAPServices)
	if len(unreachableServices) > 0 {
		logUnreachableServices(unreachableServices)
		return discoveryResult{needsFullDiscovery: true, usedCache: false}
	}

	debugf("All cached services reachable, using cached discovery\n")
	return discoveryResult{needsFullDiscovery: false, usedCache: true, cachedServices: *cachedHAPServices}
}

func logUnreachableServices(unreachableServices []HAPAccessory) {
	debugf("Found %d unreachable services, forcing full discovery\n", len(unreachableServices))
	for _, service := range unreachableServices {
		debugf("  - Unreachable: %s at %s:%d\n", service.Name, service.Host, service.Port)
	}
}

func executeLookupStrategy(discoveryResult discoveryResult, currentChildBridges []ChildBridge) []HAPAccessory {
	if discoveryResult.usedCache {
		return discoveryResult.cachedServices
	}

	if discoveryResult.needsFullDiscovery {
		return performFullMDNSDiscovery(currentChildBridges)
	}

	return []HAPAccessory{}
}

func performFullMDNSDiscovery(currentChildBridges []ChildBridge) []HAPAccessory {
	debugf("Performing full mDNS discovery\n")

	var expectedNames []string
	for _, bridge := range currentChildBridges {
		expectedNames = append(expectedNames, bridge.Name)
	}

	allHAPServices := discoverHAPServicesWithTimeoutAndFilter(TimeoutConfig.MDNSTotal, expectedNames)
	return filterKnownChildBridges(allHAPServices, currentChildBridges)
}

func filterKnownChildBridges(allHAPServices []HAPAccessory, currentChildBridges []ChildBridge) []HAPAccessory {
	var finalServices []HAPAccessory
	for _, hapService := range allHAPServices {
		if isKnownChildBridge(hapService, currentChildBridges) {
			finalServices = append(finalServices, hapService)
		} else {
			debugf("Skipping non-child-bridge HAP service: %s\n", hapService.Name)
		}
	}
	return finalServices
}

func updateDiscoveryCache(
	cachedChildBridges *[]ChildBridge,
	cachedHAPServices *[]HAPAccessory,
	currentChildBridges []ChildBridge,
	finalServices []HAPAccessory,
) {
	*cachedChildBridges = currentChildBridges
	*cachedHAPServices = finalServices
}

// displaydiscoveryResults shows the consistent discovery output
func displaydiscoveryResults(bridges []ChildBridge, services []HAPAccessory) {
	fmt.Println("\nDiscovered bridges:")
	for _, bridge := range bridges {
		// Find matching service
		found := false
		for _, service := range services {
			if trimServiceName(service.Name) == bridge.Name {
				fmt.Printf("  - %s at %s:%d\n", bridge.Name, service.Host, service.Port)
				found = true
				break
			}
		}
		if !found {
			fmt.Printf("  - %s (not found)\n", bridge.Name)
		}
	}
}

func isKnownChildBridge(hapService HAPAccessory, _ []ChildBridge) bool {
	// Exclude main bridge - it follows pattern "Homebridge XXXX YYYY"
	if strings.HasPrefix(hapService.Name, "Homebridge ") && len(strings.Fields(hapService.Name)) == 3 {
		debugf("Excluding main bridge: %s\n", hapService.Name)
		return false
	}

	// Include all other HAP services as child bridges
	debugf("Including child bridge HAP service: %s\n", hapService.Name)
	return true
}

func discoverHAPServicesWithTimeoutAndFilter(timeout time.Duration, expectedNames []string) []HAPAccessory {
	debugf("Starting custom mDNS discovery for _hap._tcp services (total timeout: %v)...\n", timeout)

	// Create mDNS client - timeout is now handled internally with two phases
	client := NewMDNSClient(timeout)

	// Use background context since timeout is handled internally
	ctx := context.Background()

	// Discover Homebridge services with optional name filtering
	// This will use configured browse + lookup timeouts internally
	var mdnsServices []MDNSService
	var err error
	if expectedNames != nil {
		mdnsServices, err = client.DiscoverHomebridgeServicesWithFilter(ctx, expectedNames)
	} else {
		mdnsServices, err = client.DiscoverHomebridgeServices(ctx)
	}
	if err != nil {
		debugf("mDNS discovery failed: %v\n", err)
		return nil
	}

	// Convert to HAPAccessory format
	var services []HAPAccessory
	for _, mdnsService := range mdnsServices {
		service := HAPAccessory{
			Name: mdnsService.Name,
			Host: mdnsService.Host,
			Port: mdnsService.Port,
			ID:   mdnsService.Name + "_" + fmt.Sprintf("%d", mdnsService.Port),
		}

		debugf("Discovered Homebridge HAP service: %s at %s:%d\n", service.Name, service.Host, service.Port)
		services = append(services, service)
	}

	debugf("Custom mDNS discovery completed, found %d Homebridge services\n", len(services))
	return services
}

func hasHAPAccessories(host string, port int) bool {
	client := &http.Client{Timeout: TimeoutConfig.HTTPReachabilityTest}

	url := fmt.Sprintf("http://%s:%d/accessories", host, port)
	resp, err := client.Get(url)
	if err != nil {
		debugf("Failed to check accessories at %s:%d - %v\n", host, port, err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != HTTPStatusOK {
		debugf("Non-%d response from %s:%d - status %d\n", HTTPStatusOK, host, port, resp.StatusCode)
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		debugf("Failed to read response from %s:%d - %v\n", host, port, err)
		return false
	}

	var hapResp HAPResponse
	if err := json.Unmarshal(body, &hapResp); err != nil {
		debugf("Failed to parse HAP response from %s:%d - %v\n", host, port, err)
		return false
	}

	accessoryCount := len(hapResp.Accessories)
	debugf("Found %d accessories at %s:%d\n", accessoryCount, host, port)
	return accessoryCount > 0
}

type HAPAccessory struct {
	Name string
	Host string
	Port int
	ID   string
}

type ChildBridge struct {
	Status     string `json:"status"`
	Name       string `json:"name"`
	Plugin     string `json:"plugin"`
	Username   string `json:"username"`
	Identifier string `json:"identifier"`
	PID        int    `json:"pid"`
}

func getChildBridges() []ChildBridge {
	client := &http.Client{Timeout: TimeoutConfig.HTTPChildBridges}

	// Get auth token for main API
	tokenReq, _ := http.NewRequest("POST", fmt.Sprintf("http://%s:%d/api/auth/noauth", host, port), strings.NewReader("{}"))
	tokenReq.Header.Set("Content-Type", "application/json")
	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		fmt.Printf("Failed to get auth token: %v\n", err)
		return nil
	}
	defer tokenResp.Body.Close()

	tokenBody, _ := io.ReadAll(tokenResp.Body)
	debugf("Token response: %s\n", string(tokenBody))

	var tokenData map[string]interface{}
	if err := json.Unmarshal(tokenBody, &tokenData); err != nil {
		fmt.Printf("Failed to parse token response: %v\n", err)
		return nil
	}

	var authToken string
	if token, ok := tokenData["access_token"].(string); ok {
		authToken = token
	} else if token, ok := tokenData["token"].(string); ok {
		authToken = token
	}

	if authToken == "" {
		fmt.Printf("Failed to get auth token from response: %+v\n", tokenData)
		return nil
	}

	// Get child bridges
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://%s:%d/api/status/homebridge/child-bridges", host, port), nil)
	req.Header.Set("Authorization", "Bearer "+authToken)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Failed to get child bridges: %v\n", err)
		return nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != HTTPStatusOK {
		fmt.Printf("Child bridges API returned status %d: %s\n", resp.StatusCode, string(body))
		return nil
	}

	var bridges []ChildBridge
	if err := json.Unmarshal(body, &bridges); err != nil {
		fmt.Printf("Failed to parse child bridges JSON: %v\n", err)
		return nil
	}

	debugf("Found %d child bridges from API\n", len(bridges))
	for _, bridge := range bridges {
		debugf("  - %s (%s)\n", bridge.Name, bridge.Plugin)
	}

	return bridges
}

type HAPResponse struct {
	Accessories []HAPAccessoryData `json:"accessories"`
}

type HAPAccessoryData struct {
	AID      int          `json:"aid"`
	Services []HAPService `json:"services"`
}

type HAPService struct {
	Type            string              `json:"type"`
	IID             int                 `json:"iid"`
	Characteristics []HAPCharacteristic `json:"characteristics"`
}

type HAPCharacteristic struct {
	Type        string      `json:"type"`
	IID         int         `json:"iid"`
	Value       interface{} `json:"value"`
	Description string      `json:"description"`
	Perms       []string    `json:"perms"`
	Format      string      `json:"format"`
}

func checkHAPAccessoryWithSync(client *http.Client, acc HAPAccessory, lastStatus map[string]interface{}, mu *sync.Mutex) {
	var output []string
	output = append(output, fmt.Sprintf("\n[%s] Checking %s...", time.Now().Format("15:04:05"), acc.Name))

	hapResp, err := fetchHAPResponse(client, acc, &output)
	if err != nil {
		printOutputSync(output, mu)
		return
	}

	debugf("Parsed %d accessories from HAP response for %s\n", len(hapResp.Accessories), acc.Name)
	if len(hapResp.Accessories) <= 3 {
		debugf("Full HAP response for %s: %s\n", acc.Name, hapResp)
	}

	result := processAccessories(hapResp.Accessories, acc.Name, lastStatus)
	output = append(output, result.summaryLine)

	// Add individual change details if any changes were detected
	output = append(output, result.changes...)

	printOutputSync(output, mu)
}

type accessoryProcessResult struct {
	summaryLine string
	changes     []string
}

func fetchHAPResponse(client *http.Client, acc HAPAccessory, output *[]string) (*HAPResponse, error) {
	url := fmt.Sprintf("http://%s:%d/accessories", acc.Host, acc.Port)
	resp, err := client.Get(url)
	if err != nil {
		*output = append(*output, fmt.Sprintf("Connection failed: %v", err))
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != HTTPStatusOK {
		*output = append(*output, fmt.Sprintf("HTTP %d - Child bridges require HAP authentication", resp.StatusCode))
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		*output = append(*output, fmt.Sprintf("Failed to read response: %v", err))
		return nil, err
	}

	var hapResp HAPResponse
	if err := json.Unmarshal(body, &hapResp); err != nil {
		*output = append(*output, fmt.Sprintf("Failed to parse JSON: %v", err))
		return nil, err
	}

	return &hapResp, nil
}

func processAccessories(
	accessories []HAPAccessoryData, bridgeName string, lastStatus map[string]interface{},
) accessoryProcessResult {
	bridgeMarkerKey := fmt.Sprintf("_bridge_seen_%s", bridgeName)
	initialDiscovery := lastStatus[bridgeMarkerKey] == nil
	lastStatus[bridgeMarkerKey] = true

	changesDetected := 0
	accessoryCount := 0
	var accessoryNames []string
	var allChanges []string

	for _, accessory := range accessories {
		accessoryName := getAccessoryName(accessory)
		if accessoryName == "" {
			continue
		}
		accessoryCount++
		accessoryNames = append(accessoryNames, accessoryName)

		changes, changeDetails := processAccessoryCharacteristics(accessory, bridgeName, accessoryName, lastStatus)
		changesDetected += changes
		allChanges = append(allChanges, changeDetails...)
	}

	summaryLine := generateSummaryLine(summaryConfig{
		isInitialDiscovery: initialDiscovery,
		accessoryCount:     accessoryCount,
		accessoryNames:     accessoryNames,
		changesDetected:    changesDetected,
	})
	return accessoryProcessResult{summaryLine: summaryLine, changes: allChanges}
}

func processAccessoryCharacteristics(
	accessory HAPAccessoryData, bridgeName, accessoryName string, lastStatus map[string]interface{},
) (int, []string) {
	changesDetected := 0
	var changes []string

	for _, service := range accessory.Services {
		for _, char := range service.Characteristics {
			key := fmt.Sprintf("%s_%d_%d_%d_%s", bridgeName, accessory.AID, service.IID, char.IID, char.Type)

			if lastValue, exists := lastStatus[key]; exists {
				if !reflect.DeepEqual(lastValue, char.Value) {
					changeMsg := fmt.Sprintf("%s: %s changed from %v to %v",
						accessoryName, char.Description, lastValue, char.Value)
					changes = append(changes, changeMsg)
					debugf("%s characteristic %s changed from %v (%T) to %v (%T)\n",
						accessoryName, char.Description, lastValue, lastValue, char.Value, char.Value)
					changesDetected++
				}
			} else {
				debugf("First time seeing %s characteristic %s (%s) with value %v (%T)\n",
					accessoryName, char.Description, char.Type, char.Value, char.Value)
			}

			lastStatus[key] = char.Value
		}
	}

	return changesDetected, changes
}

type summaryConfig struct {
	isInitialDiscovery bool
	accessoryCount     int
	accessoryNames     []string
	changesDetected    int
}

func generateSummaryLine(config summaryConfig) string {
	if config.isInitialDiscovery && config.accessoryCount > 0 {
		return formatInitialDiscoveryMessage(config.accessoryCount, config.accessoryNames)
	}

	if config.changesDetected == 0 {
		return fmt.Sprintf("No changes detected in %d accessories.", config.accessoryCount)
	}

	return fmt.Sprintf("Summary: %d changes detected", config.changesDetected)
}

func formatInitialDiscoveryMessage(accessoryCount int, accessoryNames []string) string {
	if accessoryCount == 1 {
		return fmt.Sprintf("Found %d accessory: %s.", accessoryCount, accessoryNames[0])
	}
	return fmt.Sprintf("Found %d accessories: %s.", accessoryCount, strings.Join(accessoryNames, ", "))
}

func printOutputSync(output []string, mu *sync.Mutex) {
	mu.Lock()
	for _, line := range output {
		fmt.Println(line)
	}
	mu.Unlock()
}

func getAccessoryName(accessory HAPAccessoryData) string {
	// Look for Name characteristic (type "23") in services
	for _, service := range accessory.Services {
		for _, char := range service.Characteristics {
			if char.Type == "23" && char.Description == "Name" {
				if name, ok := char.Value.(string); ok {
					return name
				}
			}
		}
	}
	return ""
}

func debugf(format string, args ...interface{}) {
	if debug {
		fmt.Printf("[DEBUG] "+format, args...)
	}
}

// childBridgeListsEqual compares two child bridge lists to detect changes
func childBridgeListsEqual(cached, current []ChildBridge) bool {
	if len(cached) != len(current) {
		return false
	}

	// Create maps for efficient comparison
	cachedMap := make(map[string]ChildBridge)
	for _, bridge := range cached {
		cachedMap[bridge.Name] = bridge
	}

	for _, bridge := range current {
		if cachedBridge, exists := cachedMap[bridge.Name]; !exists {
			return false // New bridge
		} else if cachedBridge.Status != bridge.Status || cachedBridge.Plugin != bridge.Plugin || cachedBridge.PID != bridge.PID {
			return false // Bridge changed
		}
	}

	return true
}

// testCachedServicesReachability tests if cached services are still reachable
func testCachedServicesReachability(cachedServices []HAPAccessory) []HAPAccessory {
	var unreachableServices []HAPAccessory
	client := &http.Client{Timeout: TimeoutConfig.HTTPReachabilityTest} // Quick timeout for reachability test

	for _, service := range cachedServices {
		url := fmt.Sprintf("http://%s:%d/accessories", service.Host, service.Port)
		resp, err := client.Get(url)
		if err != nil {
			debugf("Service unreachable: %s at %s:%d - %v\n", service.Name, service.Host, service.Port, err)
			unreachableServices = append(unreachableServices, service)
			continue
		}
		if err := resp.Body.Close(); err != nil {
			debugf("Failed to close response body: %v\n", err)
		}

		if resp.StatusCode < HTTPStatusOK || resp.StatusCode >= HTTPStatusMultiple {
			debugf("Service returned error: %s at %s:%d - status %d\n", service.Name, service.Host, service.Port, resp.StatusCode)
			unreachableServices = append(unreachableServices, service)
		} else {
			debugf("Service reachable: %s at %s:%d\n", service.Name, service.Host, service.Port)
		}
	}

	return unreachableServices
}

// trimServiceName removes trailing identifier from mDNS service name
// e.g., "TplinkSmarthome 4160" becomes "TplinkSmarthome"
func trimServiceName(serviceName string) string {
	fields := strings.Fields(serviceName)
	if len(fields) <= 1 {
		return serviceName
	}
	return strings.Join(fields[:len(fields)-1], " ")
}

// updateLineInPlace updates a specific line using cursor positioning
