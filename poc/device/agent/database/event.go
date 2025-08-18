package database

import (
	"time"
)

// Event system for database changes
type DeploymentDatabaseEvent struct {
	Type       EventType
	Deployment AppDeployment
	Timestamp  time.Time
}

type EventType string

const (
	EventDeploymentAdded               EventType = "DEPLOYMENT_ADDED"
	EventDeploymentUpdated             EventType = "DEPLOYMENT_UPDATED"
	EventDeploymentDeployed            EventType = "DEPLOYMENT_DEPLOYED"
	EventDeploymentDeleted             EventType = "DEPLOYMENT_DELETED"
	EventDeploymentDesiredStateChanged EventType = "DEPLOYMENT_DESIRED_STATE_CHANGED"
	EventDeploymentStatusUpdate        EventType = "DEPLOYMENT_STATUS_UPDATE"
)

type DeploymentDatabaseSubscriber interface {
	OnDatabaseEvent(event DeploymentDatabaseEvent) error
	GetSubscriberID() string
}
