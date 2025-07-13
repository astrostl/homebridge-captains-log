// This file is part of homebridge-captains-log
// Package main implements a CLI tool to monitor Homebridge accessory status changes.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

type DiscoveryProgress struct {
	Phase       string       // "browse", "lookup", "complete"
	ServiceName string       // Name of service being processed
	Service     *MDNSService // Completed service (nil if still processing)
	Error       error        // Any error encountered
	Current     int          // Current count
	Total       int          // Total expected (if known)
}

var (
	host     string
	port     int
	interval time.Duration
	count    int
	token    string
	useHAP   bool
	debug    bool
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
	DefaultPollingInterval: 30 * time.Second,
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

	defaultHost := getEnvOrDefault("CLOG_HB_HOST", "localhost")
	defaultPort := getEnvIntOrDefault("CLOG_HB_PORT", DefaultHomebridgePort)
	defaultToken := getEnvOrDefault("CLOG_HB_TOKEN", "")

	rootCmd.Flags().StringVarP(&host, "host", "H", defaultHost, "Homebridge UI host")
	rootCmd.Flags().IntVarP(&port, "port", "p", defaultPort, "Homebridge UI port")
	rootCmd.Flags().DurationVarP(&interval, "interval", "i", TimeoutConfig.DefaultPollingInterval,
		"Polling interval (duration format: 30s, 1m, etc.)")
	rootCmd.Flags().IntVarP(&count, "count", "c", -1,
		"Number of checks to perform (0 = discovery-only, default = infinite)")
	rootCmd.Flags().StringVarP(&token, "token", "t", defaultToken, "Homebridge UI auth token")
	rootCmd.Flags().BoolVarP(&debug, "debug", "d", false, "Enable debug output")
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
	// Check if main bridge mode was explicitly requested
	mainMode, _ := cmd.Flags().GetBool("main")
	if mainMode {
		useHAP = false
	}

	if !useHAP {
		// Main bridge mode
		baseURL := fmt.Sprintf("http://%s:%d", host, port)

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
		// Child bridges mode (default)
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
		if val, ok := newValue.(bool); ok && val {
			return "turned ON"
		}
		if val, ok := newValue.(float64); ok && val == 1.0 {
			return "turned ON"
		}
		return "turned OFF"
	case "ContactSensorState":
		if newValue.(float64) == 0 {
			return "door/window CLOSED"
		}
		return "door/window OPENED"
	case "MotionDetected":
		if val, ok := newValue.(bool); ok && val {
			return "motion DETECTED"
		}
		if val, ok := newValue.(float64); ok && val == 1.0 {
			return "motion DETECTED"
		}
		return "motion CLEARED"
	case "Brightness":
		return fmt.Sprintf("brightness: %v%% → %v%%", oldValue, newValue)
	case "CurrentTemperature":
		return fmt.Sprintf("temperature: %.1f°C → %.1f°C", oldValue, newValue)
	case "CurrentRelativeHumidity":
		return fmt.Sprintf("humidity: %.1f%% → %.1f%%", oldValue, newValue)
	case "BatteryLevel":
		return fmt.Sprintf("battery: %v%% → %v%%", oldValue, newValue)
	case "StatusLowBattery":
		if newValue.(float64) == 1 {
			return "⚠️  LOW BATTERY"
		}
		return "battery OK"
	default:
		return fmt.Sprintf("%s: %v → %v", key, oldValue, newValue)
	}
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
	currentChildBridges, hapServices, usedCache := discoverBridges(cachedChildBridges, cachedHAPServices, forceFullDiscovery)

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
		displayDiscoveryResults(currentChildBridges, hapServices)
		fmt.Printf("Discovery incomplete - exiting. Check that all child bridges are running and " +
			"advertising _hap._tcp services.\n")
		return
	}

	// Show discovery results based on whether we used cache or did full discovery
	if usedCache {
		fmt.Println()
		fmt.Println("Using cached bridge configuration (no changes detected)")
	} else {
		displayDiscoveryResults(currentChildBridges, hapServices)
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
	currentChildBridges, finalServices, _ := discoverBridges(cachedChildBridges, cachedHAPServices, true)
	displayDiscoveryResults(currentChildBridges, finalServices)
}

// discoverBridges performs the actual discovery logic, returns bridges and services
func discoverBridges(
	cachedChildBridges *[]ChildBridge, cachedHAPServices *[]HAPAccessory, forceFullDiscovery bool,
) ([]ChildBridge, []HAPAccessory, bool) {
	// Get known child bridges from API first
	currentChildBridges := getChildBridges()
	if len(currentChildBridges) == 0 {
		debugf("No child bridges found in API\n")
		return currentChildBridges, []HAPAccessory{}, false
	}
	debugf("Found %d child bridges from API\n", len(currentChildBridges))

	var finalServices []HAPAccessory
	needsFullDiscovery := forceFullDiscovery
	usedCache := false

	// Check if this is initial discovery or if child bridge list has changed
	if !forceFullDiscovery && len(*cachedChildBridges) > 0 {
		if !childBridgeListsEqual(*cachedChildBridges, currentChildBridges) {
			debugf("Child bridge list changed, forcing full discovery\n")
			needsFullDiscovery = true
		} else {
			debugf("Child bridge list unchanged, checking cached services\n")
			// Test cached services for reachability
			unreachableServices := testCachedServicesReachability(*cachedHAPServices)
			if len(unreachableServices) > 0 {
				debugf("Found %d unreachable services, forcing full discovery\n", len(unreachableServices))
				for _, service := range unreachableServices {
					debugf("  - Unreachable: %s at %s:%d\n", service.Name, service.Host, service.Port)
				}
				needsFullDiscovery = true
			} else {
				debugf("All cached services reachable, using cached discovery\n")
				finalServices = *cachedHAPServices
				usedCache = true
			}
		}
	}

	// Perform full mDNS discovery if needed
	if needsFullDiscovery {
		debugf("Performing full mDNS discovery\n")

		// Discover HAP services via mDNS
		var expectedNames []string
		for _, bridge := range currentChildBridges {
			expectedNames = append(expectedNames, bridge.Name)
		}

		allHAPServices := discoverHAPServicesWithTimeoutAndFilter(TimeoutConfig.MDNSTotal, expectedNames)

		// Filter HAP services to only include known child bridges
		for _, hapService := range allHAPServices {
			if isKnownChildBridge(hapService, currentChildBridges) {
				finalServices = append(finalServices, hapService)
			} else {
				debugf("Skipping non-child-bridge HAP service: %s\n", hapService.Name)
			}
		}
	}

	// Update cache
	*cachedChildBridges = currentChildBridges
	*cachedHAPServices = finalServices

	return currentChildBridges, finalServices, usedCache
}

// displayDiscoveryResults shows the consistent discovery output
func displayDiscoveryResults(bridges []ChildBridge, services []HAPAccessory) {
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
	for _, change := range result.changes {
		output = append(output, change)
	}

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

	summaryLine := generateSummaryLine(initialDiscovery, accessoryCount, accessoryNames, changesDetected)
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

func generateSummaryLine(
	initialDiscovery bool, accessoryCount int, accessoryNames []string, changesDetected int,
) string {
	if initialDiscovery && accessoryCount > 0 {
		if accessoryCount == 1 {
			return fmt.Sprintf("Found %d accessory: %s.", accessoryCount, accessoryNames[0])
		}
		return fmt.Sprintf("Found %d accessories: %s.", accessoryCount, strings.Join(accessoryNames, ", "))
	}

	if changesDetected == 0 {
		return fmt.Sprintf("No changes detected in %d accessories.", accessoryCount)
	}

	return fmt.Sprintf("Summary: %d changes detected", changesDetected)
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
