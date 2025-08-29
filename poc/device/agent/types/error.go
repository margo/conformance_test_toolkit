package types

import (
	"fmt"
)

// AgentError provides structured error handling
type AgentError struct {
	Component string
	Operation string
	Err       error
	Retryable bool
}

func (e AgentError) Error() string {
	return fmt.Sprintf("[%s:%s] %v", e.Component, e.Operation, e.Err)
}

func NewAgentError(component, operation string, err error, retryable bool) AgentError {
	return AgentError{
		Component: component,
		Operation: operation,
		Err:       err,
		Retryable: retryable,
	}
}

// Common error constructors for consistency
func DatabaseError(operation string, err error) AgentError {
	return NewAgentError("database", operation, err, true)
}

func DeploymentError(operation string, err error, retryable bool) AgentError {
	return NewAgentError("deployment", operation, err, retryable)
}

func MonitoringError(operation string, err error) AgentError {
	return NewAgentError("monitoring", operation, err, true)
}

func OnboardingError(operation string, err error) AgentError {
	return NewAgentError("onboarding", operation, err, true)
}

func ConfigError(operation string, err error) AgentError {
	return NewAgentError("config", operation, err, false)
}
