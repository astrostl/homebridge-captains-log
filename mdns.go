// This file is part of homebridge-captains-log
package main

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// MDNSService represents a discovered mDNS service
type MDNSService struct {
	Name       string
	Host       string
	Port       int
	TXTRecords map[string]string
}

// MDNSClient provides mDNS discovery functionality
type MDNSClient struct {
	timeout time.Duration
}

// NewMDNSClient creates a new mDNS client with the specified timeout
func NewMDNSClient(timeout time.Duration) *MDNSClient {
	return &MDNSClient{
		timeout: timeout,
	}
}

// BrowseServicesWithEarlyCompletion discovers services with early completion when expected count is reached
func (c *MDNSClient) BrowseServicesWithEarlyCompletion(
	_ context.Context, serviceType string, expectedCount int,
) ([]string, error) {
	c.logBrowseStart(serviceType, expectedCount)

	conn, mcastAddr, err := c.setupMulticastConnection()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := c.sendBrowseQuery(conn, mcastAddr, serviceType); err != nil {
		return nil, err
	}

	return c.collectBrowseResponses(conn, serviceType, expectedCount)
}

func (*MDNSClient) logBrowseStart(serviceType string, expectedCount int) {
	if expectedCount > 0 {
		debugf("Starting mDNS browse for service type: %s (expecting %d services, will complete early)\n",
			serviceType, expectedCount)
	} else {
		debugf("Starting mDNS browse for service type: %s (unknown count, using full timeout)\n", serviceType)
	}
}

func (*MDNSClient) setupMulticastConnection() (*net.UDPConn, *net.UDPAddr, error) {
	mcastAddr, err := net.ResolveUDPAddr("udp4", "224.0.0.251:5353")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve mDNS address: %w", err)
	}

	conn, err := net.ListenMulticastUDP("udp4", nil, mcastAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create multicast UDP listener: %w", err)
	}

	return conn, mcastAddr, nil
}

func (*MDNSClient) sendBrowseQuery(conn *net.UDPConn, mcastAddr *net.UDPAddr, serviceType string) error {
	var msg dnsmessage.Message
	msg.Header.ID = 0
	msg.Header.OpCode = 0
	msg.Header.RecursionDesired = false
	msg.Questions = []dnsmessage.Question{
		{
			Name:  dnsmessage.MustNewName(serviceType + ".local."),
			Type:  dnsmessage.TypePTR,
			Class: dnsmessage.ClassINET,
		},
	}

	packed, err := msg.Pack()
	if err != nil {
		return fmt.Errorf("failed to pack DNS message: %w", err)
	}

	_, err = conn.WriteTo(packed, mcastAddr)
	if err != nil {
		return fmt.Errorf("failed to send query: %w", err)
	}

	debugf("Sent mDNS query for %s\n", serviceType)
	return nil
}

func (c *MDNSClient) collectBrowseResponses(conn *net.UDPConn, serviceType string, expectedCount int) ([]string, error) {
	var services []string
	serviceMap := make(map[string]bool)
	deadline := time.Now().Add(c.timeout)
	var lastNewServiceTime time.Time
	silenceTimeout := TimeoutConfig.MDNSSilenceTimeout

	for time.Now().Before(deadline) {
		if c.shouldCompleteEarly(services, lastNewServiceTime, silenceTimeout, expectedCount) {
			break
		}

		newServices, err := c.readAndProcessResponse(conn, serviceType, serviceMap)
		if err != nil {
			continue
		}

		if newServices > 0 {
			services = c.updateServicesFromMap(serviceMap)
			lastNewServiceTime = time.Now()
			debugf("Found %d new services, updated silence timer\n", newServices)
		}
	}

	debugf("Browse completed, found %d services\n", len(services))
	return services, nil
}

func (*MDNSClient) shouldCompleteEarly(
	services []string, lastNewServiceTime time.Time, silenceTimeout time.Duration, expectedCount int,
) bool {
	if len(services) > 0 && !lastNewServiceTime.IsZero() &&
		time.Since(lastNewServiceTime) > silenceTimeout {
		debugf("No new services for %v, completing with %d services\n", silenceTimeout, len(services))
		return true
	}

	if expectedCount > 0 && len(services) >= expectedCount && !lastNewServiceTime.IsZero() &&
		time.Since(lastNewServiceTime) > TimeoutConfig.MDNSEarlyExitSilence {
		debugf("Found expected %d services with early exit silence period, completing\n", expectedCount)
		return true
	}

	return false
}

func (c *MDNSClient) readAndProcessResponse(conn *net.UDPConn, serviceType string, serviceMap map[string]bool) (int, error) {
	if err := conn.SetReadDeadline(time.Now().Add(TimeoutConfig.MDNSReadTimeout)); err != nil {
		return 0, err
	}

	buffer := make([]byte, MaxBufSize)
	n, _, err := conn.ReadFrom(buffer)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return 0, err // Expected timeout
		}
		debugf("Read error: %v\n", err)
		return 0, err
	}

	var response dnsmessage.Message
	err = response.Unpack(buffer[:n])
	if err != nil {
		debugf("Failed to unpack response: %v\n", err)
		return 0, err
	}

	return c.extractServicesFromResponse(response, serviceType, serviceMap), nil
}

func (c *MDNSClient) extractServicesFromResponse(
	response dnsmessage.Message, serviceType string, serviceMap map[string]bool,
) int {
	queryName := serviceType + ".local."
	newServicesCount := 0

	for _, answer := range response.Answers {
		if answer.Header.Type == dnsmessage.TypePTR && strings.EqualFold(answer.Header.Name.String(), queryName) {
			if ptr, ok := answer.Body.(*dnsmessage.PTRResource); ok {
				serviceName := c.cleanServiceName(ptr.PTR.String(), serviceType)
				if !serviceMap[serviceName] {
					debugf("Found service: %s\n", serviceName)
					serviceMap[serviceName] = true
					newServicesCount++
				}
			}
		}
	}

	return newServicesCount
}

func (*MDNSClient) cleanServiceName(serviceName, serviceType string) string {
	serviceName = strings.TrimSuffix(serviceName, ".local.")
	serviceName = strings.TrimSuffix(serviceName, ".")
	return strings.TrimSuffix(serviceName, "."+serviceType)
}

func (*MDNSClient) updateServicesFromMap(serviceMap map[string]bool) []string {
	var services []string
	for serviceName := range serviceMap {
		services = append(services, serviceName)
	}
	return services
}

// LookupService gets detailed information about a specific service
// Equivalent to: dns-sd -L "servicename" _hap._tcp local.
func (c *MDNSClient) LookupService(_ context.Context, serviceName, serviceType string) (*MDNSService, error) {
	debugf("Looking up service: %s.%s\n", serviceName, serviceType)

	conn, mcastAddr, err := c.setupLookupConnection()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	fullName := serviceName + "." + serviceType + ".local."
	if err := c.sendLookupQuery(conn, mcastAddr, fullName); err != nil {
		return nil, err
	}

	service := c.collectLookupResponses(conn, serviceName, serviceType)
	c.logLookupResult(serviceName, service)
	return service, nil
}

func (*MDNSClient) setupLookupConnection() (*net.UDPConn, *net.UDPAddr, error) {
	mcastAddr, err := net.ResolveUDPAddr("udp4", "224.0.0.251:5353")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve mDNS address: %w", err)
	}

	conn, err := net.ListenMulticastUDP("udp4", nil, mcastAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create multicast UDP listener: %w", err)
	}

	return conn, mcastAddr, nil
}

func (*MDNSClient) sendLookupQuery(conn *net.UDPConn, mcastAddr *net.UDPAddr, fullName string) error {
	msg := createLookupMessage(fullName)
	packed, err := msg.Pack()
	if err != nil {
		return fmt.Errorf("failed to pack DNS message: %w", err)
	}

	_, err = conn.WriteTo(packed, mcastAddr)
	if err != nil {
		return fmt.Errorf("failed to send query: %w", err)
	}

	debugf("Sent lookup query for %s\n", fullName)
	return nil
}

func createLookupMessage(fullName string) dnsmessage.Message {
	var msg dnsmessage.Message
	msg.Header.ID = 0
	msg.Header.OpCode = 0
	msg.Header.RecursionDesired = false
	msg.Questions = []dnsmessage.Question{
		{
			Name:  dnsmessage.MustNewName(fullName),
			Type:  dnsmessage.TypeSRV,
			Class: dnsmessage.ClassINET,
		},
		{
			Name:  dnsmessage.MustNewName(fullName),
			Type:  dnsmessage.TypeTXT,
			Class: dnsmessage.ClassINET,
		},
	}
	return msg
}

func (c *MDNSClient) collectLookupResponses(conn *net.UDPConn, serviceName, serviceType string) *MDNSService {
	var service *MDNSService
	deadline := time.Now().Add(TimeoutConfig.MDNSLookupPerService)

	for time.Now().Before(deadline) && service == nil {
		service = c.attemptLookupResponse(conn, serviceName, serviceType)
	}

	return service
}

func (c *MDNSClient) attemptLookupResponse(conn *net.UDPConn, serviceName, serviceType string) *MDNSService {
	if err := conn.SetReadDeadline(time.Now().Add(TimeoutConfig.MDNSReadTimeout)); err != nil {
		return nil
	}

	buffer := make([]byte, MaxBufSize)
	n, _, err := conn.ReadFrom(buffer)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return nil
		}
		debugf("Read error: %v\n", err)
		return nil
	}

	var response dnsmessage.Message
	err = response.Unpack(buffer[:n])
	if err != nil {
		debugf("Failed to unpack response: %v\n", err)
		return nil
	}

	return c.parseServiceResponseForType(&response, serviceName, serviceType)
}

func (*MDNSClient) logLookupResult(serviceName string, service *MDNSService) {
	if service != nil {
		debugf("Lookup completed for %s: %s:%d\n", serviceName, service.Host, service.Port)
	} else {
		debugf("Lookup failed for %s\n", serviceName)
	}
}

// parseServiceResponseForType extracts service details from a DNS response for any service type
func (c *MDNSClient) parseServiceResponseForType(response *dnsmessage.Message, serviceName, serviceType string) *MDNSService {
	if response == nil {
		return nil
	}

	service := &MDNSService{
		Name:       serviceName,
		TXTRecords: make(map[string]string),
	}

	expectedName := serviceName + "." + serviceType + ".local."

	c.parseAnswerRecords(response.Answers, service, expectedName)
	c.resolveHostToIP(response.Additionals, service)

	if service.Host != "" && service.Port > 0 {
		return service
	}

	return nil
}

// parseServiceResponse extracts service details from a DNS response (HAP compatibility)
func (c *MDNSClient) parseServiceResponse(response *dnsmessage.Message, serviceName string) *MDNSService {
	return c.parseServiceResponseForType(response, serviceName, "_hap._tcp")
}

func (c *MDNSClient) parseAnswerRecords(answers []dnsmessage.Resource, service *MDNSService, expectedName string) {
	for _, answer := range answers {
		switch answer.Header.Type {
		case dnsmessage.TypeSRV:
			c.parseSRVRecord(answer, service, expectedName)
		case dnsmessage.TypeTXT:
			c.parseTXTAnswer(answer, service, expectedName)
		}
	}
}

func (*MDNSClient) parseSRVRecord(answer dnsmessage.Resource, service *MDNSService, expectedName string) {
	if srv, ok := answer.Body.(*dnsmessage.SRVResource); ok {
		if strings.EqualFold(answer.Header.Name.String(), expectedName) {
			service.Port = int(srv.Port)
			service.Host = strings.TrimSuffix(srv.Target.String(), ".")
			debugf("Found SRV for %s: %s:%d\n", service.Name, service.Host, service.Port)
		} else {
			debugf("Skipping SRV record for %s (expected %s)\n", answer.Header.Name.String(), expectedName)
		}
	}
}

func (c *MDNSClient) parseTXTAnswer(answer dnsmessage.Resource, service *MDNSService, expectedName string) {
	if txt, ok := answer.Body.(*dnsmessage.TXTResource); ok {
		if strings.EqualFold(answer.Header.Name.String(), expectedName) {
			for _, record := range txt.TXT {
				c.parseTXTRecord(string(record), service.TXTRecords)
			}
			debugf("Found TXT records for %s: %v\n", service.Name, service.TXTRecords)
		} else {
			debugf("Skipping TXT record for %s (expected %s)\n", answer.Header.Name.String(), expectedName)
		}
	}
}

func (*MDNSClient) resolveHostToIP(additionals []dnsmessage.Resource, service *MDNSService) {
	if service.Host == "" {
		return
	}

	for _, additional := range additionals {
		if additional.Header.Type == dnsmessage.TypeA && strings.Contains(additional.Header.Name.String(), service.Host) {
			if a, ok := additional.Body.(*dnsmessage.AResource); ok {
				ip := net.IP(a.A[:])
				service.Host = ip.String()
				debugf("Resolved hostname to IP: %s\n", service.Host)
				break
			}
		}
	}
}

// parseTXTRecord parses a TXT record string into key-value pairs
func (*MDNSClient) parseTXTRecord(record string, txtRecords map[string]string) {
	if strings.Contains(record, "=") {
		parts := strings.SplitN(record, "=", 2)
		if len(parts) == 2 {
			txtRecords[parts[0]] = parts[1]
		}
	} else {
		// Boolean flag (key without value)
		txtRecords[record] = ""
	}
}

// DiscoverHomebridgeServices discovers all Homebridge HAP services
// This combines browse + lookup and filters for md=homebridge services only
func (c *MDNSClient) DiscoverHomebridgeServices(ctx context.Context) ([]MDNSService, error) {
	return c.DiscoverHomebridgeServicesWithFilter(ctx, nil)
}

// DiscoverHomebridgeServicesWithFilter discovers Homebridge HAP services with optional name filtering
func (c *MDNSClient) DiscoverHomebridgeServicesWithFilter(ctx context.Context, expectedNames []string) ([]MDNSService, error) {
	debugf("Starting Homebridge service discovery with optimized timing\n")

	serviceNames, err := c.browseHAPServices(ctx, expectedNames)
	if err != nil {
		return nil, err
	}

	servicesToLookup := c.filterServicesForLookup(serviceNames, expectedNames)
	homebridgeServices := c.performParallelLookups(servicesToLookup)

	return homebridgeServices, nil
}

func (c *MDNSClient) browseHAPServices(ctx context.Context, expectedNames []string) ([]string, error) {
	browseCtx, browseCancel := context.WithTimeout(ctx, TimeoutConfig.MDNSBrowseMax)
	defer browseCancel()

	expectedServiceCount := len(expectedNames)
	if expectedServiceCount == 0 {
		expectedServiceCount = -1
	}

	serviceNames, err := c.BrowseServicesWithEarlyCompletion(browseCtx, "_hap._tcp", expectedServiceCount)
	if err != nil {
		return nil, fmt.Errorf("failed to browse services: %w", err)
	}

	debugf("Found %d _hap._tcp services to examine\n", len(serviceNames))
	return serviceNames, nil
}

func (*MDNSClient) filterServicesForLookup(serviceNames, expectedNames []string) []string {
	if len(expectedNames) == 0 {
		return serviceNames
	}

	servicesToLookup := filterServicesByExpectedNames(serviceNames, expectedNames)
	debugf("Filtered to %d services for lookup\n", len(servicesToLookup))
	debugf("Services to lookup: %v\n", servicesToLookup)
	return servicesToLookup
}

func (c *MDNSClient) performParallelLookups(servicesToLookup []string) []MDNSService {
	lookupCtx, lookupCancel := context.WithTimeout(context.Background(), TimeoutConfig.MDNSLookupMax)
	defer lookupCancel()

	var wg sync.WaitGroup
	var mu sync.Mutex
	var homebridgeServices []MDNSService

	debugf("Starting parallel lookups for %d services: %v\n", len(servicesToLookup), servicesToLookup)

	maxConcurrency := c.calculateConcurrency(servicesToLookup)
	semaphore := make(chan struct{}, maxConcurrency)
	done := make(chan struct{})

	go c.executeLookupWorkers(lookupCtx, servicesToLookup, semaphore, &wg, &mu, &homebridgeServices, done)

	c.waitForLookupCompletion(lookupCtx, done, len(homebridgeServices))
	return homebridgeServices
}

func (*MDNSClient) calculateConcurrency(servicesToLookup []string) int {
	maxConcurrency := MaxStatementsPerFunc
	if len(servicesToLookup) < maxConcurrency {
		maxConcurrency = len(servicesToLookup)
	}
	return maxConcurrency
}

func (c *MDNSClient) executeLookupWorkers(
	lookupCtx context.Context,
	servicesToLookup []string,
	semaphore chan struct{},
	wg *sync.WaitGroup,
	mu *sync.Mutex,
	homebridgeServices *[]MDNSService,
	done chan struct{},
) {
	defer close(done)

	for _, serviceName := range servicesToLookup {
		wg.Add(1)
		go c.lookupWorker(lookupCtx, serviceName, semaphore, wg, mu, homebridgeServices)
	}

	wg.Wait()
	debugf("All lookup goroutines completed\n")
}

func (c *MDNSClient) lookupWorker(
	lookupCtx context.Context,
	serviceName string,
	semaphore chan struct{},
	wg *sync.WaitGroup,
	mu *sync.Mutex,
	homebridgeServices *[]MDNSService,
) {
	defer wg.Done()

	if !c.acquireSemaphore(lookupCtx, serviceName, semaphore) {
		return
	}
	defer func() { <-semaphore }()

	if !c.checkLookupContext(lookupCtx, serviceName) {
		return
	}

	service := c.lookupServiceWithRetries(lookupCtx, serviceName, "_hap._tcp")
	if service == nil {
		debugf("Failed to lookup service details for %s\n", serviceName)
		return
	}

	c.processLookupResult(service, mu, homebridgeServices)
}

func (*MDNSClient) acquireSemaphore(lookupCtx context.Context, serviceName string, semaphore chan struct{}) bool {
	select {
	case semaphore <- struct{}{}:
		return true
	case <-lookupCtx.Done():
		debugf("Context cancelled before acquiring semaphore for %s\n", serviceName)
		return false
	}
}

func (*MDNSClient) checkLookupContext(lookupCtx context.Context, serviceName string) bool {
	select {
	case <-lookupCtx.Done():
		debugf("Context cancelled before lookup for %s\n", serviceName)
		return false
	default:
		return true
	}
}

func (*MDNSClient) processLookupResult(service *MDNSService, mu *sync.Mutex, homebridgeServices *[]MDNSService) {
	debugf("Successfully looked up service %s at %s:%d with TXT records: %v\n",
		service.Name, service.Host, service.Port, service.TXTRecords)

	if md, exists := service.TXTRecords["md"]; exists && strings.ToLower(md) == "homebridge" {
		debugf("Found Homebridge service: %s at %s:%d\n", service.Name, service.Host, service.Port)
		mu.Lock()
		*homebridgeServices = append(*homebridgeServices, *service)
		mu.Unlock()
	} else {
		debugf("Skipping non-Homebridge service: %s (md=%s)\n", service.Name, md)
	}
}

func (*MDNSClient) waitForLookupCompletion(lookupCtx context.Context, done chan struct{}, serviceCount int) {
	select {
	case <-done:
		debugf("Discovery completed, found %d Homebridge services\n", serviceCount)
	case <-lookupCtx.Done():
		debugf("Lookup phase timed out after %v, found %d Homebridge services so far\n",
			TimeoutConfig.MDNSLookupMax, serviceCount)
	}
}

// lookupServiceWithRetries performs service lookup with retry logic
func (c *MDNSClient) lookupServiceWithRetries(ctx context.Context, serviceName, serviceType string) *MDNSService {
	var service *MDNSService
	var err error

	debugf("Starting lookup with retries for %s.%s\n", serviceName, serviceType)

	// Try lookup up to 3 times for reliability, but respect context timeout
	for attempt := 1; attempt <= 3; attempt++ {
		// Check if context is cancelled before each attempt
		select {
		case <-ctx.Done():
			debugf("Context cancelled during lookup of %s (attempt %d)\n", serviceName, attempt)
			return nil
		default:
		}

		debugf("Lookup attempt %d/3 for %s\n", attempt, serviceName)
		service, err = c.LookupService(ctx, serviceName, serviceType)
		if err != nil {
			debugf("Failed to lookup service %s (attempt %d): %v\n", serviceName, attempt, err)
		} else if service != nil {
			debugf("Successfully found service %s on attempt %d: %s:%d\n", serviceName, attempt, service.Host, service.Port)
			return service // Success
		} else {
			debugf("No details found for service %s (attempt %d) - service returned nil\n", serviceName, attempt)
		}

		// Short delay before retry, but respect context timeout
		if attempt < 3 {
			debugf("Waiting %v before retry %d for %s\n", TimeoutConfig.LookupRetryDelay, attempt+1, serviceName)
			select {
			case <-time.After(TimeoutConfig.LookupRetryDelay):
				// Continue to next attempt
			case <-ctx.Done():
				debugf("Context cancelled during retry delay for %s\n", serviceName)
				return nil
			}
		}
	}

	debugf("Failed to lookup service %s after 3 attempts - giving up\n", serviceName)
	return nil
}

// DiscoverAWTRIXServices discovers all AWTRIX LED matrix devices via mDNS
func (c *MDNSClient) DiscoverAWTRIXServices(ctx context.Context) ([]MDNSService, error) {
	debugf("Starting AWTRIX service discovery\n")

	serviceNames, err := c.BrowseServicesWithEarlyCompletion(ctx, "_awtrix._tcp", 0)
	if err != nil {
		return nil, fmt.Errorf("failed to browse AWTRIX services: %w", err)
	}

	debugf("Found %d _awtrix._tcp services to examine\n", len(serviceNames))

	var awtrixServices []MDNSService
	for _, serviceName := range serviceNames {
		service, err := c.LookupService(ctx, serviceName, "_awtrix._tcp")
		if err != nil {
			debugf("Failed to lookup AWTRIX service %s: %v\n", serviceName, err)
			continue
		}
		if service != nil {
			debugf("Found AWTRIX device: %s at %s:%d\n", service.Name, service.Host, service.Port)
			awtrixServices = append(awtrixServices, *service)
		}
	}

	debugf("Discovery completed, found %d AWTRIX devices\n", len(awtrixServices))
	return awtrixServices, nil
}

// filterServicesByExpectedNames filters mDNS service names to only include those that match expected child bridge names
// mDNS service names have trailing identifiers (e.g., "TplinkSmarthome 4160") that we trim for comparison
func filterServicesByExpectedNames(serviceNames []string, expectedNames []string) []string {
	var filtered []string

	// Create a map of expected names for O(1) lookup
	expectedMap := make(map[string]bool)
	for _, name := range expectedNames {
		expectedMap[strings.ToLower(name)] = true
	}

	for _, serviceName := range serviceNames {
		// Trim the last word (identifier) from the mDNS service name for comparison
		// e.g., "TplinkSmarthome 4160" becomes "TplinkSmarthome"
		fields := strings.Fields(serviceName)
		if len(fields) == 0 {
			continue
		}

		var trimmedName string
		if len(fields) > 1 {
			trimmedName = strings.Join(fields[:len(fields)-1], " ")
		} else {
			trimmedName = fields[0]
		}

		// Check for exact match with trimmed name
		if expectedMap[strings.ToLower(trimmedName)] {
			filtered = append(filtered, serviceName) // Keep original full name for HAP operations
		}
	}

	return filtered
}
