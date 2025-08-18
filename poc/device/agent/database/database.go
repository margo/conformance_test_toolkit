package database

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/margo/dev-repo/standard/generatedCode/wfm/sbi"
)

type AppDeployment struct {
	DesiredState *sbi.AppState
	CurrentState *sbi.AppState
	// map[componentname]componentobject
	CurrentComponentsTrack map[string]sbi.ComponentStatus
}

type AgentDatabase interface {
	Start() error
	Stop() error
	Clear() error

	GetAllDeployments() (map[string]AppDeployment, error)
	GetDeployment(deploymentId string) (AppDeployment, error)
	RemoveDeployment(deploymentId string) error
	GetCurrentState(deploymentId string) (*sbi.AppState, error)
	GetDesiredState(deploymentId string) (*sbi.AppState, error)
	UpsertDeploymentDesiredState(state sbi.AppState) error
	UpsertDeploymentCurrentState(state sbi.AppState) error
	UpsertComponentStatus(deploymentId, componentName string, status sbi.ComponentStatus) error
	DeploymentExists(deploymentId string) bool
	GetDeploymentCount() int

	GetDeviceProperties() *sbi.Properties
	GetDeviceAuth() *sbi.OnboardingResponse
	GetDevice() (*DeviceModel, error)
	UpsertDevice(device *DeviceModel) error
	UpsertDeviceCapabilities(capabilities *sbi.DeviceCapabilities) error
	UpsertDeviceAuth(auth *sbi.OnboardingResponse) error
	Subscribe(subscriber DeploymentDatabaseSubscriber) error
	Unsubscribe(subscriberId string) error
}

// DeviceStatus represents device operational status.
type DeviceStatus string

const (
	DeviceStatusOffline DeviceStatus = "offline" // Device is offline
	DeviceStatusOnline  DeviceStatus = "online"  // Device is online
	DeviceStatusError   DeviceStatus = "error"   // Device is in an error state
	DeviceStatusBooting DeviceStatus = "booting" // Device is in booting state
)

type DeviceModel struct {
	Id               string
	DeviceProperties *sbi.Properties
	DeviceAuth       *sbi.OnboardingResponse
	Status           DeviceStatus
	LastSeen         time.Time
}

type DatabaseDump struct {
	Deployments map[string]AppDeployment
	Device      DeviceModel
}

// AgentInMemoryDatabase is an in-memory database that stores application workloads
// and provides event-driven notifications to subscribers when data changes.
// It combines in-memory storage for fast access with disk persistence for durability.
type AgentInMemoryDatabase struct {
	// bkpDataDir is the directory where database dumps are stored for persistence
	bkpDataDir string

	// device object
	device DeviceModel

	// deployments maps workload ID to app state - this is the source of truth
	deployments map[string]AppDeployment

	// workloadChangeSubscribers is a list of components that want to be notified of database changes
	// Using slice instead of map for simplicity, assuming small number of workloadChangeSubscribers
	workloadChangeSubscribers []DeploymentDatabaseSubscriber

	// workloadEventChan is a buffered channel for async event publishing to avoid blocking database operations
	workloadEventChan chan DeploymentDatabaseEvent

	// mu protects concurrent access to subscribers slice and currentWorkloads map
	// Using RWMutex to allow concurrent reads while protecting writes
	mu sync.RWMutex

	// ctx is the context for controlling the lifecycle of background goroutines
	ctx context.Context

	// operationStopper is used to gracefully shutdown background operations
	// Separate from ctx to allow controlled shutdown sequence
	operationStopper context.CancelFunc
}

// NewAgentInMemoryDatabase creates a new in-memory database instance.
// It sets up the database with event publishing capabilities and prepares for persistence.
//
// Parameters:
//   - ctx: Context for controlling database lifecycle
//   - dataDir: Directory for storing database dumps (defaults to "./data" if empty)
func NewAgentInMemoryDatabase(ctx context.Context, dataDir string) *AgentInMemoryDatabase {
	if dataDir == "" {
		dataDir = "./data"
	}

	// Design Note: We create a separate context with cancel function to ensure
	// we can control the shutdown sequence independently of the parent context.
	localCtx, canceller := context.WithCancel(ctx)

	return &AgentInMemoryDatabase{
		device: DeviceModel{
			Id:               "",
			DeviceProperties: nil,
			DeviceAuth:       nil,
			Status:           DeviceStatusOnline,
			LastSeen:         time.Now().UTC(),
		},
		deployments:               make(map[string]AppDeployment),
		bkpDataDir:                dataDir,
		workloadChangeSubscribers: make([]DeploymentDatabaseSubscriber, 0),
		workloadEventChan:         make(chan DeploymentDatabaseEvent, 100), // Buffered channel to prevent blocking
		ctx:                       localCtx,
		operationStopper:          canceller,
	}
}

// Start initializes the database by restoring from disk and starting background processes.
// This method should be called before using the database.
func (db *AgentInMemoryDatabase) Start() error {
	// Restore data from disk first to ensure we have the latest database state
	if err := db.restore(); err != nil {
		return fmt.Errorf("failed to restore database: %w", err)
	}

	if db.ctx == nil {
		panic("line 114")
	}

	// Start event publisher in background goroutine
	go db.eventPublisher(db.ctx)

	return nil
}

// Stop gracefully shuts down the database by stopping background operations
// and persisting current state to disk.
func (db *AgentInMemoryDatabase) Stop() error {
	// Stop all background operations first
	db.operationStopper()

	// TODO: Wait a moment for goroutines to finish (could be improved with sync.WaitGroup)
	time.Sleep(100 * time.Millisecond)

	// Dump current state to disk for persistence
	if err := db.dump(); err != nil {
		return fmt.Errorf("failed to dump database: %w", err)
	}

	return nil
}

func (db *AgentInMemoryDatabase) UpsertDevice(device *DeviceModel) error {
	db.device = *device
	return nil
}

func (db *AgentInMemoryDatabase) UpsertDeviceCapabilities(capabilities *sbi.DeviceCapabilities) error {
	db.device.DeviceProperties = &capabilities.Properties
	return nil
}

func (db *AgentInMemoryDatabase) UpsertDeviceAuth(auth *sbi.OnboardingResponse) error {
	db.device.DeviceAuth = auth
	return nil
}

func (db *AgentInMemoryDatabase) GetDevice() (*DeviceModel, error) {
	return &db.device, nil
}

// dump persists the current in-memory state to disk as JSON.
// Uses atomic write (write to temp file, then rename) to prevent corruption.
func (db *AgentInMemoryDatabase) dump() error {
	// Ensure data directory exists before writing
	if err := os.MkdirAll(db.bkpDataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}
	filename := filepath.Join(db.bkpDataDir, "agent.dump.json")

	dump := DatabaseDump{
		Deployments: db.deployments,
		Device:      db.device,
	}

	// Marshal with indentation for human readability (could be optimized for size)
	data, err := json.MarshalIndent(dump, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal workloads: %w", err)
	}

	// Atomic write: write to temporary file first, then rename
	// This prevents corruption if the process is killed during write
	tempFile := filename + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write dump file: %w", err)
	}

	// Rename is atomic on most filesystems, ensuring consistency
	if err := os.Rename(tempFile, filename); err != nil {
		return fmt.Errorf("failed to rename dump file: %w", err)
	}

	return nil
}

// restore loads the database state from disk if a dump file exists.
// If no dump exists, initializes with empty state.
func (db *AgentInMemoryDatabase) restore() error {
	filename := filepath.Join(db.bkpDataDir, "agent.dump.json")

	// Check if dump file exists - missing file is not an error
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		// No dump file exists, start with empty database
		db.deployments = make(map[string]AppDeployment)
		return nil
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read dump file: %w", err)
	}

	dump := DatabaseDump{}

	if err := json.Unmarshal(data, &dump); err != nil {
		return fmt.Errorf("failed to unmarshal database dump: %w", err)
	}

	db.deployments = dump.Deployments
	// let us initialize the map if it is nil
	if db.deployments == nil {
		db.deployments = make(map[string]AppDeployment)
	}

	db.device = dump.Device
	return nil
}

// GetAllDeployments returns a copy of all deployments in the database.
// Returns a copy to prevent external modifications to internal state.
func (db *AgentInMemoryDatabase) GetAllDeployments() (map[string]AppDeployment, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	// Return a copy to prevent external modifications
	deployments := make(map[string]AppDeployment, len(db.deployments))
	for k, v := range db.deployments {
		deployments[k] = v
	}
	return deployments, nil
}

// GetDeployment retrieves a specific deployment by ID.
// Returns an error if the deployment doesn't exist.
func (db *AgentInMemoryDatabase) GetDeployment(deploymentId string) (AppDeployment, error) {
	if deploymentId == "" {
		return AppDeployment{}, fmt.Errorf("deployment ID cannot be empty")
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	deployment, exists := db.deployments[deploymentId]
	if !exists {
		return AppDeployment{}, fmt.Errorf("deployment with ID %s not found", deploymentId)
	}

	return deployment, nil
}

// GetDesiredState retrieves the desired state for a specific deployment by ID.
// Returns an error if the deployment doesn't exist.
func (db *AgentInMemoryDatabase) GetDesiredState(deploymentId string) (*sbi.AppState, error) {
	if deploymentId == "" {
		return nil, fmt.Errorf("deployment ID cannot be empty")
	}

	db.mu.RLock() // Use read lock instead of write lock
	defer db.mu.RUnlock()

	// Check if deployment exists
	existingDeployment, exists := db.deployments[deploymentId]
	if !exists {
		return nil, fmt.Errorf("deployment with ID %s not found", deploymentId)
	}

	return existingDeployment.DesiredState, nil // Use correct field name 'Desired'
}

// GetCurrentState retrieves the current state for a specific deployment by ID.
// Returns an error if the deployment doesn't exist.
func (db *AgentInMemoryDatabase) GetCurrentState(deploymentId string) (*sbi.AppState, error) {
	if deploymentId == "" {
		return nil, fmt.Errorf("deployment ID cannot be empty")
	}

	db.mu.RLock() // Use read lock instead of write lock
	defer db.mu.RUnlock()

	// Check if deployment exists
	existingDeployment, exists := db.deployments[deploymentId]
	if !exists {
		return nil, fmt.Errorf("deployment with ID %s not found", deploymentId)
	}

	return existingDeployment.CurrentState, nil
}

// UpsertDeploymentDesiredState upserts the desired state for a deployment.
// Creates a new deployment if it doesn't exist, updates if it does.
func (db *AgentInMemoryDatabase) UpsertDeploymentDesiredState(newstate sbi.AppState) error {
	if newstate.AppId == "" {
		return fmt.Errorf("deployment AppId cannot be empty")
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	// Check if deployment already exists
	existingDeployment, exists := db.deployments[newstate.AppId]
	var eventType EventType

	if exists {
		// Deployment exists - this is an update
		eventType = EventDeploymentUpdated
		// Check if desired state actually changed
		if existingDeployment.DesiredState.AppDeploymentYAMLHash != newstate.AppDeploymentYAMLHash {
			eventType = EventDeploymentDesiredStateChanged
		}
	} else {
		// Deployment doesn't exist - this is an add
		eventType = EventDeploymentAdded
		existingDeployment = AppDeployment{}
	}

	// Update the desired state
	existingDeployment.DesiredState = &newstate
	db.deployments[newstate.AppId] = existingDeployment

	// Publish event to notify subscribers of the change
	db.publishEvent(DeploymentDatabaseEvent{
		Type:       eventType,
		Deployment: existingDeployment,
		Timestamp:  time.Now(),
	})

	return nil
}

// UpsertDeploymentCurrentState updates the current state for a deployment.
// Creates a new deployment if it doesn't exist, updates if it does.
func (db *AgentInMemoryDatabase) UpsertDeploymentCurrentState(currentstate sbi.AppState) error {
	if currentstate.AppId == "" {
		return fmt.Errorf("deployment AppId cannot be empty")
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	// Check if deployment already exists
	existingDeployment, exists := db.deployments[currentstate.AppId]
	var eventType EventType

	if exists {
		// Deployment exists - this is an update
		eventType = EventDeploymentStatusUpdate
	} else {
		// Deployment doesn't exist - create new one
		eventType = EventDeploymentAdded
		existingDeployment = AppDeployment{}
	}

	// Update the current state
	existingDeployment.CurrentState = &currentstate
	db.deployments[currentstate.AppId] = existingDeployment

	// Publish event to notify subscribers of the change
	db.publishEvent(DeploymentDatabaseEvent{
		Type:       eventType,
		Deployment: existingDeployment,
		Timestamp:  time.Now(),
	})
	return nil
}

func (db *AgentInMemoryDatabase) UpsertComponentStatus(deploymentId, componentName string, status sbi.ComponentStatus) error {
	if deploymentId == "" {
		return fmt.Errorf("deployment ID cannot be empty")
	}
	if componentName == "" {
		return fmt.Errorf("component name cannot be empty")
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	existingDeployment, exists := db.deployments[deploymentId]
	if !exists {
		return fmt.Errorf("deployment with ID %s not found", deploymentId)
	}

	// Update the component status
	existingDeployment.CurrentComponentsTrack[componentName] = status
	db.deployments[deploymentId] = existingDeployment

	// Publish event to notify subscribers of the component status change
	db.publishEvent(DeploymentDatabaseEvent{
		Type:       EventDeploymentStatusUpdate,
		Deployment: existingDeployment,
		Timestamp:  time.Now(),
	})

	return nil
}

// Remove deletes a deployment from the database.
// Returns an error if the deployment doesn't exist.
func (db *AgentInMemoryDatabase) RemoveDeployment(deploymentId string) error {
	if deploymentId == "" {
		return fmt.Errorf("deployment ID cannot be empty")
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	existingDeployment, exists := db.deployments[deploymentId]
	if !exists {
		return fmt.Errorf("deployment with ID %s not found", deploymentId)
	}

	delete(db.deployments, deploymentId)

	db.publishEvent(DeploymentDatabaseEvent{
		Type:       EventDeploymentDeleted,
		Deployment: existingDeployment,
		Timestamp:  time.Now(),
	})

	return nil
}

// DeploymentExists checks if a deployment with the given ID exists.
func (db *AgentInMemoryDatabase) DeploymentExists(deploymentId string) bool {
	db.mu.RLock()
	defer db.mu.RUnlock()

	_, exists := db.deployments[deploymentId]
	return exists
}

// GetDeploymentCount returns the total number of deployments in the database.
func (db *AgentInMemoryDatabase) GetDeploymentCount() int {
	db.mu.RLock()
	defer db.mu.RUnlock()

	return len(db.deployments)
}

// GetDeviceProperties returns device properties.
func (db *AgentInMemoryDatabase) GetDeviceProperties() *sbi.Properties {
	return db.device.DeviceProperties
}

// GetDeviceAuth returns device auth.
func (db *AgentInMemoryDatabase) GetDeviceAuth() *sbi.OnboardingResponse {
	return db.device.DeviceAuth
}

// Clear removes all deployments from the database.
// This should primarily be used for testing or reset scenarios
func (db *AgentInMemoryDatabase) Clear() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	for _, deployment := range db.deployments {
		db.publishEvent(DeploymentDatabaseEvent{
			Type:       EventDeploymentDeleted,
			Deployment: deployment,
			Timestamp:  time.Now(),
		})
	}

	db.deployments = make(map[string]AppDeployment)
	return nil
}

// eventPublisher runs in a background goroutine and distributes database events
// to all registered subscribers.
//
// Note: This runs in a separate goroutine to avoid blocking database operations.
// Each subscriber is notified in its own goroutine to prevent slow subscribers
// from blocking others.
func (db *AgentInMemoryDatabase) eventPublisher(ctx context.Context) {
	if ctx == nil {
		panic("context cannot be nil")
	}

	for {
		select {
		case event := <-db.workloadEventChan:
			// Get a snapshot of current subscribers under read lock
			db.mu.RLock()
			subscribers := make([]DeploymentDatabaseSubscriber, len(db.workloadChangeSubscribers))
			copy(subscribers, db.workloadChangeSubscribers)
			db.mu.RUnlock()

			// Notify all subscribers in parallel to avoid blocking
			// Each subscriber gets its own goroutine to prevent slow subscribers
			// from affecting others
			for _, subscriber := range subscribers {
				go func(sub DeploymentDatabaseSubscriber) {
					if err := sub.OnDatabaseEvent(event); err != nil {
						// Log error but don't block other subscribers
						log.Printf("Subscriber %s failed to handle event: %v",
							sub.GetSubscriberID(), err)
					}
				}(subscriber)
			}

		case <-ctx.Done():
			// shutdown
			log.Print("shutting down the database...")
			return
		}
	}
}

// Subscribe registers a subscriber to receive database events.
// Subscribers will be notified of all workload changes via the OnDatabaseEvent method.
func (db *AgentInMemoryDatabase) Subscribe(subscriber DeploymentDatabaseSubscriber) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// TODO: Consider checking for duplicate subscribers based on ID
	db.workloadChangeSubscribers = append(db.workloadChangeSubscribers, subscriber)
	return nil
}

func (db *AgentInMemoryDatabase) Unsubscribe(subscriberId string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	for i, subscriber := range db.workloadChangeSubscribers {
		if subscriber.GetSubscriberID() == subscriberId {
			db.workloadChangeSubscribers = append(db.workloadChangeSubscribers[:i], db.workloadChangeSubscribers[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("subscriber with id %s not found", subscriberId)
}

// publishEvent sends an event to the event channel for distribution to subscribers.
// This method is non-blocking - if the channel is full, the event is dropped.
//
// Note: Non-blocking publish prevents database operations from hanging
// if the event system is overwhelmed.
func (db *AgentInMemoryDatabase) publishEvent(event DeploymentDatabaseEvent) {
	select {
	case db.workloadEventChan <- event:
		// Event successfully queued
	default:
		// TODO: Event channel full, log this
		log.Printf("Warning: Event channel full, dropping event for app %s", event.Deployment.CurrentState.AppId)
	}
}
